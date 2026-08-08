package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/thearchitectit/guardrail-mcp/internal/ingest"
	"github.com/thearchitectit/guardrail-mcp/internal/models"
)

func (s *Server) listRules(c echo.Context) error {
	ctx := c.Request().Context()
	var enabled *bool
	if enabledParam := c.QueryParam("enabled"); enabledParam != "" {
		e := enabledParam == "true"
		enabled = &e
	}
	category := c.QueryParam("category")
	limit, err := strconv.Atoi(c.QueryParam("limit"))
	if err != nil || limit <= 0 || limit > maxPageLimit {
		limit = defaultPageLimit
	}
	offset, err := strconv.Atoi(c.QueryParam("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	rules, err := s.ruleStore.List(ctx, enabled, category, limit, offset)
	if err != nil {
		slog.Error("Failed to list rules", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to retrieve rules"})
	}

	total, err := s.ruleStore.Count(ctx, enabled, category)
	if err != nil {
		slog.Warn("Failed to count rules", "error", err)
		total = len(rules) // Fallback to current page size
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": rules,
		"pagination": map[string]interface{}{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

func (s *Server) getRule(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	rule, err := s.ruleStore.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		if err.Error() == fmt.Sprintf("rule not found: %s", parsedUUID) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "rule not found"})
		}
		slog.Error("Failed to get rule", "rule_id", id, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to retrieve rule"})
	}

	return c.JSON(http.StatusOK, rule)
}

func (s *Server) createRule(c echo.Context) error {
	var rule models.PreventionRule
	if err := c.Bind(&rule); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if err := s.ruleStore.Create(c.Request().Context(), &rule); err != nil {
		slog.Error("Failed to create rule", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create rule"})
	}

	// Invalidate cache - log error but don't fail the request
	if err := s.cache.InvalidateOnRuleChange(c.Request().Context(), rule.RuleID); err != nil {
		slog.Warn("Failed to invalidate rule cache", "rule_id", rule.RuleID, "error", err)
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogRuleChange(c.Request().Context(), keyHash, rule.RuleID, "create")

	return c.JSON(http.StatusCreated, rule)
}

func (s *Server) updateRule(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	var rule models.PreventionRule
	if err := c.Bind(&rule); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	rule.ID = parsedUUID
	if err := s.ruleStore.Update(c.Request().Context(), &rule); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Invalidate cache
	s.cache.InvalidateOnRuleChange(c.Request().Context(), rule.RuleID)

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogRuleChange(c.Request().Context(), keyHash, rule.RuleID, "update")

	return c.JSON(http.StatusOK, rule)
}

func (s *Server) deleteRule(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	// Get rule for cache invalidation before deleting
	rule, err := s.ruleStore.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		// Rule doesn't exist - return 404
		return c.JSON(http.StatusNotFound, map[string]string{"error": "rule not found"})
	}

	if err := s.ruleStore.Delete(c.Request().Context(), parsedUUID); err != nil {
		slog.Error("Failed to delete rule", "rule_id", id, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete rule"})
	}

	// Invalidate cache - log error but don't fail the request
	if err := s.cache.InvalidateOnRuleChange(c.Request().Context(), rule.RuleID); err != nil {
		slog.Warn("Failed to invalidate cache after rule deletion", "rule_id", rule.RuleID, "error", err)
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogRuleChange(c.Request().Context(), keyHash, rule.RuleID, "delete")

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) patchRule(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	var req struct {
		Enabled  *bool   `json:"enabled,omitempty"`
		Name     *string `json:"name,omitempty"`
		Message  *string `json:"message,omitempty"`
		Pattern  *string `json:"pattern,omitempty"`
		Severity *string `json:"severity,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// Get existing rule
	rule, err := s.ruleStore.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		if err.Error() == fmt.Sprintf("rule not found: %s", parsedUUID) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "rule not found"})
		}
		slog.Error("Failed to get rule for patch", "rule_id", id, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to retrieve rule"})
	}

	// Apply patches
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Name != nil {
		rule.Name = *req.Name
	}
	if req.Message != nil {
		rule.Message = *req.Message
	}
	if req.Pattern != nil {
		rule.Pattern = *req.Pattern
	}
	if req.Severity != nil {
		rule.Severity = models.Severity(*req.Severity)
	}

	if err := s.ruleStore.Update(c.Request().Context(), rule); err != nil {
		slog.Error("Failed to patch rule", "rule_id", id, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update rule"})
	}

	// Invalidate cache - log error but don't fail the request
	if err := s.cache.InvalidateOnRuleChange(c.Request().Context(), rule.RuleID); err != nil {
		slog.Warn("Failed to invalidate rule cache after patch", "rule_id", rule.RuleID, "error", err)
	}

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogRuleChange(c.Request().Context(), keyHash, rule.RuleID, "patch")

	return c.JSON(http.StatusOK, rule)
}

// syncRules triggers a rule sync from repository directories
func (s *Server) syncRules(c echo.Context) error {
	slog.Info("Received rule sync request")
	ctx := c.Request().Context()

	// Parse optional request body for sync options
	var req struct {
		Force bool `json:"force,omitempty"`
	}
	_ = c.Bind(&req) // Optional, ignore error

	// Create a new sync job
	jobID := uuid.New()
	slog.Info("Starting rule sync job", "job_id", jobID)

	// Trigger sync in background to not block the response
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// Update status to running
		lastRuleSyncStatusLock.Lock()
		lastRuleSyncStatus = RuleSyncStatus{
			Status:   "running",
			LastSync: time.Now().UTC(),
			Errors:   []string{},
		}
		lastRuleSyncStatusLock.Unlock()

		// Perform the sync
		result, err := s.ingestSvc.SyncRulesFromRepo(bgCtx)

		// Update final status
		lastRuleSyncStatusLock.Lock()
		if err != nil {
			lastRuleSyncStatus.Status = "failed"
			lastRuleSyncStatus.Errors = append(lastRuleSyncStatus.Errors, err.Error())
		} else {
			lastRuleSyncStatus.Status = "completed"
			lastRuleSyncStatus.RulesAdded = result.Added
			lastRuleSyncStatus.RulesUpdated = result.Updated
			lastRuleSyncStatus.RulesDeleted = result.Disabled
			if len(result.Errors) > 0 {
				lastRuleSyncStatus.Errors = result.Errors
			}
		}
		lastRuleSyncStatusLock.Unlock()

		if err != nil {
			slog.Error("Rule sync background job failed", "job_id", jobID, "error", err)
		} else {
			slog.Info("Rule sync background job completed", "job_id", jobID, "added", result.Added, "updated", result.Updated, "disabled", result.Disabled)
		}
	}()

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogRuleChange(ctx, keyHash, fmt.Sprintf("sync:%s", jobID), "sync")

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"job_id":  jobID,
		"status":  "running",
		"message": "Rule sync started",
	})
}

// getRuleSyncStatus returns the status of the last rule sync operation
func (s *Server) getRuleSyncStatus(c echo.Context) error {
	lastRuleSyncStatusLock.RLock()
	status := lastRuleSyncStatus
	lastRuleSyncStatusLock.RUnlock()

	// If never synced, return appropriate message
	if status.Status == "" {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":        "never_run",
			"last_sync":     nil,
			"message":       "No rule sync has been performed yet. Use POST /api/rules/sync to trigger a sync.",
			"rules_added":   0,
			"rules_updated": 0,
			"rules_deleted": 0,
			"errors":        []string{},
		})
	}

	return c.JSON(http.StatusOK, status)
}

// triggerRuleSyncFromUpload handles uploaded markdown files to create/update rules
func (s *Server) triggerRuleSyncFromUpload(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse multipart form with 50MB max memory
	form, err := c.MultipartForm()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
	}
	defer form.RemoveAll()

	files := form.File["files"]
	if len(files) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no files provided"})
	}

	// Create a new ingest job
	jobID := uuid.New()
	slog.Info("Starting rule sync job", "job_id", jobID)
	fileContents := make(map[string][]byte)

	// Process each uploaded file
	var processedFiles []string
	var skippedFiles []string

	for _, fileHeader := range files {
		// Validate file type
		if !ingest.IsMarkdownFile(fileHeader.Filename) {
			skippedFiles = append(skippedFiles, fileHeader.Filename)
			continue
		}

		// Open the uploaded file
		file, err := fileHeader.Open()
		if err != nil {
			slog.Error("Failed to open uploaded file", "filename", fileHeader.Filename, "error", err)
			continue
		}

		// Read file content
		content, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			slog.Error("Failed to read uploaded file", "filename", fileHeader.Filename, "error", err)
			continue
		}

		fileContents[fileHeader.Filename] = content
		processedFiles = append(processedFiles, fileHeader.Filename)
	}

	// Process the files through the ingest service for rules
	totalResult := &ingest.RuleSyncResult{}
	for filename, content := range fileContents {
		result, err := s.ingestSvc.SyncRulesFromUpload(ctx, content, filename)
		if err != nil {
			slog.Error("Failed to process uploaded rule file", "filename", filename, "error", err)
			continue
		}
		totalResult.Added += result.Added
		totalResult.Updated += result.Updated
		totalResult.Disabled += result.Disabled
		totalResult.Errors = append(totalResult.Errors, result.Errors...)
	}

	// Update sync status
	lastRuleSyncStatusLock.Lock()
	lastRuleSyncStatus = RuleSyncStatus{
		Status:       "completed",
		LastSync:     time.Now().UTC(),
		RulesAdded:   totalResult.Added,
		RulesUpdated: totalResult.Updated,
		RulesDeleted: totalResult.Disabled,
		Errors:       totalResult.Errors,
	}
	lastRuleSyncStatusLock.Unlock()

	// Audit log
	keyHash := getAPIKeyHash(c)
	s.auditLogger.LogRuleChange(ctx, keyHash, fmt.Sprintf("upload:%s", jobID), "upload")

	return c.JSON(http.StatusOK, map[string]interface{}{
		"job_id":    jobID,
		"processed": len(processedFiles),
		"skipped":   skippedFiles,
		"files":     processedFiles,
		"status":    "completed",
	})
}

// Project handlers
