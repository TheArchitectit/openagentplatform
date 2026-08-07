package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/thearchitectit/guardrail-mcp/internal/database"
	"github.com/thearchitectit/guardrail-mcp/internal/models"
)

func (s *MCPServer) handleVerifyFixesIntact(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionToken, _ := args["session_token"].(string)
	filePath, _ := args["file_path"].(string)
	modifiedContent, _ := args["modified_content"].(string)
	originalContent, _ := args["original_content"].(string)

	// Validate required parameters
	if sessionToken == "" {
		result := models.FixVerificationResult{
			AllFixesIntact: false,
			VerifySummary:  "Session token is required",
			Fixes:          []models.IndividualFixResult{},
			Recommendation: "Invalid input - session_token required",
		}
		return buildToolResult(result, true)
	}

	if filePath == "" {
		result := models.FixVerificationResult{
			AllFixesIntact: false,
			VerifySummary:  "File path is required",
			Fixes:          []models.IndividualFixResult{},
			Recommendation: "Invalid input - file_path required",
		}
		return buildToolResult(result, true)
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		result := models.FixVerificationResult{
			AllFixesIntact: false,
			VerifySummary:  "Session not found or expired",
			Fixes:          []models.IndividualFixResult{},
			Recommendation: "Create a new session",
		}
		return buildToolResult(result, true)
	}

	if s.db == nil {
		result := models.FixVerificationResult{
			AllFixesIntact: false,
			VerifySummary:  "Database connection is not configured",
			Fixes:          []models.IndividualFixResult{},
			Recommendation: "Configure database and retry",
		}
		return buildToolResult(result, true)
	}

	if s.fixVerificationStore == nil {
		result := models.FixVerificationResult{
			AllFixesIntact: false,
			VerifySummary:  "Fix verification store is not configured",
			Fixes:          []models.IndividualFixResult{},
			Recommendation: "Configure fix verification store and retry",
		}
		return buildToolResult(result, true)
	}

	// Get failure registry store to query active failures for the file
	failStore := database.NewFailureStore(s.db)
	failures, err := failStore.GetActiveByFiles(ctx, []string{filePath})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf("Failed to check failures: %v", err)}},
			IsError: true,
		}, nil
	}

	// Get current content for verification (use modified_content if provided, otherwise read from file)
	currentContent := modifiedContent
	if currentContent == "" {
		if currentContent, err = s.readFileContent(filePath); err != nil {
			currentContent = originalContent
		}
	}

	// Process each failure as a potential fix to verify
	fixVerificationStore := s.fixVerificationStore
	results := []models.IndividualFixResult{}
	intactCount := 0

	if len(failures) == 0 {
		// No failures found for this file, check if we have any fix tracking records
		if verifications, err := fixVerificationStore.GetBySessionAndFile(ctx, sessionToken, filePath); err == nil && len(verifications) > 0 {
			for _, v := range verifications {
				// Verify against current content
				status, message := fixVerificationStore.VerifyFixContent(ctx, currentContent, &v)
				results = append(results, models.IndividualFixResult{
					FailureID:           v.FailureID,
					Status:              status,
					FixType:             v.FixType,
					AffectedFile:        v.FilePath,
					VerificationMessage: message,
				})
				if status == models.StatusConfirmed {
					intactCount++
				}
			}
		}
	} else {
		// Verify each failure/fix against current content
		for _, failure := range failures {
			// Try to get or create a fix verification record
			fixContent := failure.RootCause
			var fixType models.FixType

			// Determine fix type based on failure data
			if failure.RegressionPattern != "" {
				fixType = models.FixTypeRegex
				// Use regression pattern as fix content for regex fixes
				fixContent = failure.RegressionPattern
			} else if failure.RootCause != "" {
				// This is a bit of a guess - using error message as fix content for code changes
				fixContent = failure.ErrorMessage
				fixType = models.FixTypeCodeChange
			} else {
				fixType = models.FixTypeConfig
				fixContent = failure.ErrorMessage
			}

			verification, err := fixVerificationStore.GetOrCreate(ctx, sessionToken, failure.FailureID, filePath, fixContent, fixType)
			if err != nil {
				slog.Warn("Failed to get or create fix verification", "error", err, "failure_id", failure.FailureID)
				continue
			}

			// Verify if fix is intact
			status, message := fixVerificationStore.VerifyFixContent(ctx, currentContent, verification)

			// Update verification status
			if err := fixVerificationStore.UpdateVerificationStatus(ctx, sessionToken, failure.FailureID, status); err != nil {
				slog.Warn("Failed to update verification status", "error", err, "failure_id", failure.FailureID)
			}

			results = append(results, models.IndividualFixResult{
				FailureID:           failure.FailureID,
				Status:              status,
				FixType:             fixType,
				AffectedFile:        filePath,
				VerificationMessage: message,
			})

			if status == models.StatusConfirmed {
				intactCount++
			}
		}
	}

	// Build summary and recommendation
	totalFixes := len(results)
	var summary, recommendation string
	allIntact := true

	if totalFixes == 0 {
		summary = "No fixes to verify for this file"
		recommendation = "Proceed - no fixes found"
		allIntact = true
	} else {
		summary = fmt.Sprintf("%d/%d fixes verified intact", intactCount, totalFixes)
		if intactCount == totalFixes {
			recommendation = "Continue - all fixes intact"
			allIntact = true
		} else if intactCount > 0 {
			recommendation = "Review - some fixes modified"
			allIntact = false
		} else {
			recommendation = "Halt - fix verification failed"
			allIntact = false
		}
	}

	result := models.FixVerificationResult{
		AllFixesIntact: allIntact,
		VerifySummary:  summary,
		Fixes:          results,
		Recommendation: recommendation,
	}

	return buildToolResult(result, !allIntact)
}

// Helper function to read file content
func (s *MCPServer) readFileContent(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
}

// handleValidateExactReplacement validates that code replacement matches exact specification
func (s *MCPServer) handleValidateExactReplacement(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionToken, _ := args["session_token"].(string)
	filePath, _ := args["file_path"].(string)
	originalContent, _ := args["original_content"].(string)
	modifiedContent, _ := args["modified_content"].(string)
	replacementType, _ := args["replacement_type"].(string)

	// Validate required parameters
	if sessionToken == "" {
		result := models.ExactReplacementValidationResult{
			ExactMatch:     false,
			Violations:     []models.ExactReplacementViolation{{Type: "validation_error", Severity: "error", Message: "session_token is required"}},
			Recommendation: "Invalid input - session_token required",
		}
		return buildToolResult(result, true)
	}

	if filePath == "" {
		result := models.ExactReplacementValidationResult{
			ExactMatch:     false,
			Violations:     []models.ExactReplacementViolation{{Type: "validation_error", Severity: "error", Message: "file_path is required"}},
			Recommendation: "Invalid input - file_path required",
		}
		return buildToolResult(result, true)
	}

	// If original_content is empty and modified_content is empty or not provided,
	// it's not an error but should be flagged as no validation needed
	if originalContent == "" {
		if modifiedContent == "" {
			result := models.ExactReplacementValidationResult{
				ExactMatch:     true,
				Violations:     []models.ExactReplacementViolation{},
				DiffStats:      models.DiffStats{Additions: 0, Deletions: 0},
				Recommendation: "No content to validate - acceptable for file creation",
			}
			return buildToolResult(result, false)
		}
		// Original content is empty but modified content exists - this is file creation
		result := models.ExactReplacementValidationResult{
			ExactMatch:     true,
			Violations:     []models.ExactReplacementViolation{},
			DiffStats:      models.DiffStats{Additions: len(strings.Split(modifiedContent, "\n")), Deletions: 0},
			Recommendation: "File creation - no exact match validation needed",
		}
		return buildToolResult(result, false)
	}

	// Use provided modified_content or read from file
	actualContent := modifiedContent
	if actualContent == "" {
		if readContent, err := s.readFileContent(filePath); err == nil {
			actualContent = readContent
		}
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		result := models.ExactReplacementValidationResult{
			ExactMatch:     false,
			Violations:     []models.ExactReplacementViolation{{Type: "session_error", Severity: "error", Message: "Session not found or expired"}},
			Recommendation: "Create a new session",
		}
		return buildToolResult(result, true)
	}

	// Analyze the diff between original and modified content
	result := detectExactReplacementViolations(originalContent, actualContent, replacementType)
	return buildToolResult(result, !result.ExactMatch)
}

// detectExactReplacementViolations analyzes content differences for violations
func detectExactReplacementViolations(originalContent, actualContent, replacementType string) models.ExactReplacementValidationResult {
	violations := []models.ExactReplacementViolation{}
	additions := 0
	deletions := 0

	// If content matches exactly, return early
	if originalContent == actualContent {
		return models.ExactReplacementValidationResult{
			ExactMatch:     true,
			Violations:     []models.ExactReplacementViolation{},
			DiffStats:      models.DiffStats{Additions: 0, Deletions: 0},
			Recommendation: "Accept changes - exact match confirmed",
		}
	}

	// Split content into lines
	originalLines := strings.Split(originalContent, "\n")
	actualLines := strings.Split(actualContent, "\n")

	// Precompile regex patterns
	importPattern := regexp.MustCompile(`^\+*\s*import\s+`)
	typeHintPattern := regexp.MustCompile(`^\+*\s*.*:\s*\w+`) // e.g., name: Type
	debugPattern := regexp.MustCompile(`^\+*\s*.*(fmt\.Print|console\.log|println|echo)`)
	funcPattern := regexp.MustCompile(`^\+*\s*\b(func|def|function)\s+\w+`)
	commentPattern := regexp.MustCompile(`^[-+]\s*//.*`)
	variableRenamePattern := regexp.MustCompile(`^\+*\s*.*=.*//\s*renamed`)
	formattingPattern := regexp.MustCompile(`^\+*\s*\s*$`) // Lines with only whitespace changes
	reorderPattern := regexp.MustCompile(`^[-+]\s*package\s+|^\+*\s*package\s+`)

	// Calculate line-by-line diff summary using a simple line comparison
	// This is a simplified diff - in production you might use a proper diff library
	minLines := len(originalLines)
	if len(actualLines) < minLines {
		minLines = len(actualLines)
	}

	// Track changes line by line
	contentChanged := false
	for i := 0; i < minLines; i++ {
		if originalLines[i] != actualLines[i] {
			contentChanged = true
			// Count as modification (both add and delete)
			additions++
			deletions++
		}
	}

	// Add extra lines
	if len(actualLines) > minLines {
		additions += len(actualLines) - minLines
		contentChanged = true
	}

	// Removed lines
	if len(originalLines) > minLines {
		deletions += len(originalLines) - minLines
		contentChanged = true
	}

	// If no content changes detected, return exact match
	if !contentChanged && additions == 0 && deletions == 0 {
		return models.ExactReplacementValidationResult{
			ExactMatch:     true,
			Violations:     []models.ExactReplacementViolation{},
			DiffStats:      models.DiffStats{Additions: 0, Deletions: 0},
			Recommendation: "Accept changes - exact match confirmed",
		}
	}

	// Analyze actual content for violations
	// Split into added and removed lines
	addedLines := []string{}
	removedLines := []string{}

	// Simple diff calculation - find adds and removes
	// In production, consider using a proper diff algorithm
	originalSet := make(map[string]bool)
	for _, line := range originalLines {
		if strings.TrimSpace(line) != "" {
			originalSet[line] = true
		}
	}

	actualSet := make(map[string]bool)
	for _, line := range actualLines {
		if strings.TrimSpace(line) != "" {
			actualSet[line] = true
			if !originalSet[line] {
				addedLines = append(addedLines, line)
			}
		}
	}

	for _, line := range originalLines {
		if strings.TrimSpace(line) != "" && !actualSet[line] {
			removedLines = append(removedLines, line)
		}
	}

	// Check for violations in added lines
	var criticalCount, warningCount int

	for _, line := range addedLines {
		// Check for new imports
		if importPattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "new_import",
				Severity: "warning",
				Message:  "Additional import added: " + strings.TrimSpace(line),
			})
			warningCount++
		}

		// Check for type hint changes (particularly relevant for Python/JavaScript)
		if typeHintPattern.MatchString(line) && !strings.Contains(line, "//") {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "type_change",
				Severity: "warning",
				Message:  "Type hint added or changed: " + strings.TrimSpace(line),
			})
			warningCount++
		}

		// Check for debug statements
		if debugPattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "debug_added",
				Severity: "error",
				Message:  "Debug statement added: " + strings.TrimSpace(line),
			})
			criticalCount++
		}

		// Check for new functions
		if funcPattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "extra_function",
				Severity: "error",
				Message:  "New function added: " + strings.TrimSpace(line),
			})
			criticalCount++
		}

		// Check for variable renames (pattern: old = new // renamed)
		if variableRenamePattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "variable_rename",
				Severity: "warning",
				Message:  "Variable renamed: " + strings.TrimSpace(line),
			})
			warningCount++
		}

		// Check for pure formatting changes (only whitespace)
		if formattingPattern.MatchString(line) && len(strings.TrimSpace(line)) == 0 {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "formatting",
				Severity: "info",
				Message:  "Formatting change (whitespace only)",
			})
		}

		// Check for package reorganization (Go files)
		if reorderPattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "code_reorganized",
				Severity: "warning",
				Message:  "Code structure changed (package reordering)",
			})
			warningCount++
		}
	}

	// Check for comment changes
	for _, line := range append(addedLines, removedLines...) {
		if commentPattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "comment_change",
				Severity: "info",
				Message:  "Comment modified: " + strings.TrimSpace(line),
			})
		}
	}

	// Check removed lines for significant deletions
	for _, line := range removedLines {
		// Check if a function was removed
		if funcPattern.MatchString(line) {
			violations = append(violations, models.ExactReplacementViolation{
				Type:     "function_removed",
				Severity: "error",
				Message:  "Original function removed: " + strings.TrimSpace(line),
			})
			criticalCount++
		}
	}

	// Determine recommendation based on violations
	recommendation := "Accept changes - exact match confirmed"
	exactMatch := len(violations) == 0

	if !exactMatch {
		if criticalCount > 0 {
			recommendation = "Reject and use exact code - critical violations detected"
		} else if warningCount > 2 {
			recommendation = "Review modifications - multiple warnings"
		} else if warningCount > 0 {
			recommendation = "Proceed with caution - minor changes acceptable"
		} else {
			// Only info-level violations
			recommendation = "Accept changes - improvements only"
			exactMatch = true // Consider info-only as acceptable
		}
	}

	return models.ExactReplacementValidationResult{
		ExactMatch:     exactMatch,
		Violations:     violations,
		DiffStats:      models.DiffStats{Additions: additions, Deletions: deletions},
		Recommendation: recommendation,
	}
}
