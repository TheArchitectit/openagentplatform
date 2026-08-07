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

func (s *MCPServer) handlePreventRegression(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	// Extract file paths
	filesArg, _ := args["file_paths"].([]interface{})
	files := make([]string, 0, len(filesArg))
	for _, f := range filesArg {
		if str, ok := f.(string); ok {
			files = append(files, str)
		}
	}

	// Extract code content for pattern matching
	codeContent, _ := args["code_content"].(string)

	if len(files) == 0 && codeContent == "" {
		result := models.RegressionCheckResult{
			Matches: []models.RegressionMatch{},
			Checked: 0,
		}
		return buildToolResult(result, false)
	}

	// Query database for active failures affecting these files
	failStore := database.NewFailureStore(s.db)
	failures, err := failStore.GetActiveByFiles(ctx, files)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf("Failed to check failures: %v", err)}},
			IsError: true,
		}, nil
	}

	// Match failures against code content if provided
	matches := []models.RegressionMatch{}
	for _, failure := range failures {
		// Check if regression pattern matches code content
		if codeContent != "" && failure.RegressionPattern != "" {
			pattern, err := regexp.Compile(failure.RegressionPattern)
			if err == nil && pattern.MatchString(codeContent) {
				matches = append(matches, models.RegressionMatch{
					FailureID:         failure.FailureID,
					Category:          failure.Category,
					Severity:          failure.Severity,
					Message:           failure.ErrorMessage,
					RootCause:         failure.RootCause,
					RegressionPattern: failure.RegressionPattern,
					AffectedFiles:     models.ToStringSlice(failure.AffectedFiles),
				})
			}
		} else {
			// Include failure if it affects any of the files
			matches = append(matches, models.RegressionMatch{
				FailureID:         failure.FailureID,
				Category:          failure.Category,
				Severity:          failure.Severity,
				Message:           failure.ErrorMessage,
				RootCause:         failure.RootCause,
				RegressionPattern: failure.RegressionPattern,
				AffectedFiles:     models.ToStringSlice(failure.AffectedFiles),
			})
		}
	}

	result := models.RegressionCheckResult{
		Matches: matches,
		Checked: len(files),
	}

	return buildToolResult(result, len(matches) > 0)
}

// handleCheckTestProdSeparation verifies test/production environment isolation
func (s *MCPServer) handleCheckTestProdSeparation(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	filePath, _ := args["file_path"].(string)
	environment, _ := args["environment"].(string)

	if filePath == "" {
		result := models.TestProdSeparationResult{
			Valid:       false,
			Violations:  []string{"file_path is required"},
			Environment: environment,
		}
		return buildToolResult(result, true)
	}

	violations := []string{}

	// Read file content if it exists
	content := ""
	if data, err := os.ReadFile(filePath); err == nil {
		content = string(data)
	}

	switch environment {
	case "prod":
		// In prod code, check for test database usage
		if strings.Contains(content, "test_db") || strings.Contains(content, "test_database") {
			violations = append(violations, "Production code references test database")
		}
		if strings.Contains(content, "localhost:5433") || strings.Contains(content, "localhost:5434") {
			violations = append(violations, "Production code uses test database port")
		}
		// Check for test-only patterns
		if regexp.MustCompile(`testMode\s*=\s*true`).MatchString(content) {
			violations = append(violations, "Production code has test mode enabled")
		}

	case "test":
		// In test code, check for production credentials/patterns
		if strings.Contains(content, "prod_db") || strings.Contains(content, "production_database") {
			violations = append(violations, "Test code references production database")
		}
		// Check for hardcoded production URLs
		if regexp.MustCompile(`https?://api\.production\.`).MatchString(content) {
			violations = append(violations, "Test code contains production API URL")
		}
		// Check for real credentials (basic patterns)
		if regexp.MustCompile(`(?i)(aws_access_key_id|aws_secret_access_key)\s*=\s*["'][A-Z0-9]{20}["']`).MatchString(content) {
			violations = append(violations, "Test code may contain hardcoded AWS credentials")
		}
		// Check for production secrets
		if regexp.MustCompile(`(?i)production.*secret`).MatchString(content) {
			violations = append(violations, "Test code references production secrets")
		}

	default:
		violations = append(violations, fmt.Sprintf("Unknown environment: %s (expected 'test' or 'prod')", environment))
	}

	valid := len(violations) == 0

	result := models.TestProdSeparationResult{
		Valid:       valid,
		Violations:  violations,
		FilePath:    filePath,
		Environment: environment,
	}

	return buildToolResult(result, !valid)
}
