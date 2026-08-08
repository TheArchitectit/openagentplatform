package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/thearchitectit/guardrail-mcp/internal/models"
	"github.com/thearchitectit/guardrail-mcp/internal/updates"
)

func (s *Server) listOrphans(c echo.Context) error {
	_ = c.Request().Context() // Reserved for future use

	limit, err := strconv.Atoi(c.QueryParam("limit"))
	if err != nil || limit <= 0 || limit > maxPageLimit {
		limit = defaultPageLimit
	}
	offset, err := strconv.Atoi(c.QueryParam("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	// Query orphaned documents using the existing List method with a filter
	// In a full implementation, this would query with orphaned=true filter
	// For now, return an empty list
	docs := []models.Document{}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": docs,
		"pagination": map[string]interface{}{
			"total":  0,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// deleteOrphan deletes a single orphaned document
func (s *Server) deleteOrphan(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	ctx := c.Request().Context()

	// Get document to verify it exists and is orphaned
	doc, err := s.docStore.GetByID(ctx, parsedUUID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "document not found"})
	}

	// Verify the document is orphaned
	if !doc.Orphaned {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "document is not orphaned"})
	}

	// Delete the document
	if err := s.docStore.Delete(ctx, parsedUUID); err != nil {
		slog.Error("Failed to delete orphaned document", "doc_id", id, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete document"})
	}

	// Invalidate cache
	if err := s.cache.InvalidateOnDocumentChange(ctx, doc.Slug); err != nil {
		slog.Warn("Failed to invalidate document cache", "slug", doc.Slug, "error", err)
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogDocChange(ctx, keyHash, doc.Slug, "delete_orphan")

	return c.NoContent(http.StatusNoContent)
}

// Update handlers

// getUpdateStatus returns the current update status
func (s *Server) getUpdateStatus(c echo.Context) error {
	ctx := c.Request().Context()

	// Get the latest update check from database
	latestCheck, err := s.updateChecker.GetLatestCheck(ctx)
	if err != nil {
		// No check has been performed yet
		return c.JSON(http.StatusOK, map[string]interface{}{
			"last_checked":      nil,
			"updates_available": false,
			"message":           "No update check has been performed yet. Use POST /api/updates/check to trigger a check.",
		})
	}

	// Convert to response format
	response := updates.ToStatusResponse(latestCheck)

	return c.JSON(http.StatusOK, response)
}

// checkForUpdates triggers a manual update check
func (s *Server) checkForUpdates(c echo.Context) error {
	ctx := c.Request().Context()

	// Perform the update check
	check, err := s.updateChecker.Check(ctx)
	if err != nil {
		slog.Error("Update check failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to check for updates",
		})
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogDocChange(ctx, keyHash, "update-check", "check")

	// Return the result
	response := updates.ToStatusResponse(check)

	return c.JSON(http.StatusOK, response)
}
func (s *Server) ideHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) validateFile(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse request body
	var req struct {
		FilePath    string `json:"file_path"`
		Content     string `json:"content"`
		ProjectSlug string `json:"project_slug,omitempty"`
		Language    string `json:"language,omitempty"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.FilePath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file_path is required"})
	}

	// Get active rules for the project
	var rules []models.PreventionRule
	var err error

	if req.ProjectSlug != "" {
		proj, err := s.projStore.GetBySlug(ctx, req.ProjectSlug)
		if err == nil && len(proj.ActiveRules) > 0 {
			rules, err = s.ruleStore.GetByRuleIDs(ctx, proj.ActiveRules)
			if err != nil {
				slog.Warn("Failed to get project rules, falling back to all active", "error", err)
			}
		}
	}

	// If no project-specific rules, get all active rules
	if len(rules) == 0 {
		rules, err = s.ruleStore.GetActiveRules(ctx)
		if err != nil {
			slog.Error("Failed to get active rules", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to retrieve rules"})
		}
	}

	// Validate content against rules
	violations := validateContentAgainstRules(req.FilePath, req.Content, req.Language, rules)

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogValidation(ctx, keyHash, "validate_file", len(violations) == 0, len(violations))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"valid":         len(violations) == 0,
		"violations":    violations,
		"file_path":     req.FilePath,
		"rules_checked": len(rules),
	})
}

func (s *Server) validateSelection(c echo.Context) error {
	ctx := c.Request().Context()

	var req struct {
		Selection   string `json:"selection"`
		FilePath    string `json:"file_path"`
		ProjectSlug string `json:"project_slug,omitempty"`
		Language    string `json:"language,omitempty"`
		StartLine   int    `json:"start_line"`
		EndLine     int    `json:"end_line"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Selection == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "selection is required"})
	}

	// Get active rules for the project
	var rules []models.PreventionRule
	var err error

	if req.ProjectSlug != "" {
		proj, err := s.projStore.GetBySlug(ctx, req.ProjectSlug)
		if err == nil && len(proj.ActiveRules) > 0 {
			rules, err = s.ruleStore.GetByRuleIDs(ctx, proj.ActiveRules)
			if err != nil {
				slog.Warn("Failed to get project rules, falling back to all active", "error", err)
			}
		}
	}

	// If no project-specific rules, get all active rules
	if len(rules) == 0 {
		rules, err = s.ruleStore.GetActiveRules(ctx)
		if err != nil {
			slog.Error("Failed to get active rules", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to retrieve rules"})
		}
	}

	// Validate selection against rules
	violations := validateContentAgainstRules(req.FilePath, req.Selection, req.Language, rules)

	// Adjust line numbers to be relative to the file, not the selection
	for i := range violations {
		violations[i].Line = req.StartLine + violations[i].Line - 1
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogValidation(ctx, keyHash, "validate_selection", len(violations) == 0, len(violations))

	return c.JSON(http.StatusOK, map[string]interface{}{
		"valid":         len(violations) == 0,
		"violations":    violations,
		"file_path":     req.FilePath,
		"start_line":    req.StartLine,
		"end_line":      req.EndLine,
		"rules_checked": len(rules),
	})
}

func (s *Server) getIDERules(c echo.Context) error {
	ctx := c.Request().Context()
	projectSlug := c.QueryParam("project")

	// Try cache first for better performance
	cacheKey := projectSlug
	if cacheKey == "" {
		cacheKey = "default"
	}

	if cached, err := s.cache.GetIDERules(ctx, cacheKey); err == nil && len(cached) > 0 {
		// Return cached JSON directly to avoid re-marshaling
		return c.JSONBlob(http.StatusOK, cached)
	}

	var rules []models.PreventionRule
	var err error

	if projectSlug != "" {
		// Get project to find active rules
		proj, err := s.projStore.GetBySlug(ctx, projectSlug)
		if err == nil && len(proj.ActiveRules) > 0 {
			// Batch fetch all project rules in a single query (prevents N+1)
			rules, err = s.ruleStore.GetByRuleIDs(ctx, proj.ActiveRules)
			if err != nil {
				slog.Warn("Failed to get project rules, falling back to all active", "error", err)
			}
		}
	}

	// If no project-specific rules, get all active rules
	if len(rules) == 0 {
		rules, err = s.ruleStore.GetActiveRules(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}

	// Marshal once for both caching and response
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to marshal rules"})
	}

	// Cache the result asynchronously to not block the response
	go func(ctx context.Context, key string, data []byte) {
		// Use a new context with timeout for cache operation
		cacheCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if cacheErr := s.cache.SetIDERules(cacheCtx, key, data); cacheErr != nil {
			slog.Warn("Failed to cache IDE rules", "error", cacheErr)
		}
	}(ctx, cacheKey, rulesJSON)

	return c.JSONBlob(http.StatusOK, rulesJSON)
}

func (s *Server) getQuickReference(c echo.Context) error {
	ctx := c.Request().Context()

	// Try to find quick-reference document
	doc, err := s.docStore.GetBySlug(ctx, "quick-reference")
	if err != nil {
		// Try alternative slugs
		doc, err = s.docStore.GetBySlug(ctx, "quick-reference-card")
		if err != nil {
			// Search for any document with "quick reference" in title
			docs, searchErr := s.docStore.Search(ctx, "quick reference", 5)
			if searchErr != nil || len(docs) == 0 {
				return c.JSON(http.StatusOK, map[string]string{
					"reference": "Quick reference documentation not found. Please ensure documents are ingested.",
				})
			}
			doc = &docs[0]
		}
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogDocChange(ctx, keyHash, doc.Slug, "quick-reference-access")

	return c.JSON(http.StatusOK, map[string]interface{}{
		"reference": doc.Content,
		"title":     doc.Title,
		"slug":      doc.Slug,
		"category":  doc.Category,
	})
}

// validateContentAgainstRules checks content against prevention rules and returns violations
type ValidationViolation struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Match    string `json:"match"`
}

func validateContentAgainstRules(filePath, content, language string, rules []models.PreventionRule) []ValidationViolation {
	var violations []ValidationViolation
	lines := strings.Split(content, "\n")

	for _, rule := range rules {
		if !rule.Enabled || rule.Pattern == "" {
			continue
		}

		// Skip language-specific rules if language doesn't match
		if rule.Category != "" && language != "" && rule.Category != language {
			continue
		}

		// Compile regex pattern
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			slog.Warn("Invalid rule pattern", "rule_id", rule.RuleID, "error", err)
			continue
		}

		// Check each line
		for lineNum, line := range lines {
			matches := re.FindAllStringIndex(line, -1)
			for _, match := range matches {
				violations = append(violations, ValidationViolation{
					RuleID:   rule.RuleID,
					RuleName: rule.Name,
					Severity: string(rule.Severity),
					Message:  rule.Message,
					Line:     lineNum + 1,
					Column:   match[0] + 1,
					Match:    truncateMatch(line[match[0]:match[1]]),
				})
			}
		}
	}

	return violations
}

// truncateMatch limits the match length for display
func truncateMatch(match string) string {
	if len(match) > 50 {
		return match[:50] + "..."
	}
	return match
}

// getAPIKeyHash safely extracts the API key hash from the context
func getAPIKeyHash(c echo.Context) string {
	keyHash, ok := c.Get("api_key_hash").(string)
	if !ok || keyHash == "" {
		return "unknown"
	}
	return keyHash
}

// isValidSlug validates a project slug to prevent path traversal attacks
// Valid slugs contain only alphanumeric characters, hyphens, and underscores
func isValidSlug(slug string) bool {
	if slug == "" {
		return false
	}
	if len(slug) > 100 {
		return false
	}
	// Check for path traversal attempts
	if strings.Contains(slug, "..") || strings.Contains(slug, "/") || strings.Contains(slug, "\\") {
		return false
	}
	// Only allow alphanumeric, hyphens, and underscores
	for _, r := range slug {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
