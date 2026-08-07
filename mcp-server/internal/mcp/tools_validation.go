package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/thearchitectit/guardrail-mcp/internal/models"
)

func (s *MCPServer) handleValidateScope(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	filePath, _ := args["file_path"].(string)
	scope, _ := args["authorized_scope"].(string)

	if filePath == "" {
		result := models.ScopeValidationResult{
			Valid:   false,
			Message: "file_path is required",
		}
		return buildToolResult(result, true)
	}

	if scope == "" {
		result := models.ScopeValidationResult{
			Valid:    true,
			Message:  "No scope restriction specified - file allowed",
			FilePath: filePath,
			Scope:    scope,
		}
		return buildToolResult(result, false)
	}

	// Clean paths for comparison
	cleanPath := filepath.Clean(filePath)
	cleanScope := filepath.Clean(scope)

	// Check if file is within scope
	isValid := strings.HasPrefix(cleanPath, cleanScope)

	var result models.ScopeValidationResult
	if isValid {
		result = models.ScopeValidationResult{
			Valid:    true,
			Message:  fmt.Sprintf("File %s is within authorized scope", filePath),
			FilePath: filePath,
			Scope:    scope,
		}
	} else {
		result = models.ScopeValidationResult{
			Valid:        false,
			Message:      fmt.Sprintf("File %s is OUTSIDE authorized scope %s", filePath, scope),
			FilePath:     filePath,
			Scope:        scope,
			OutsideScope: true,
		}
	}

	return buildToolResult(result, !isValid)
}

// handleValidateCommit validates a commit message against conventional commit format
func (s *MCPServer) handleValidateCommit(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	message, _ := args["message"].(string)

	if message == "" {
		result := models.CommitValidationResult{
			Valid:   false,
			Message: "Commit message is required",
			Issues:  []string{"Empty commit message"},
		}
		return buildToolResult(result, true)
	}

	result := validateConventionalCommit(message)
	return buildToolResult(result, !result.Valid)
}

// validateConventionalCommit validates against conventional commit format
// Format: type(scope): description
func validateConventionalCommit(message string) models.CommitValidationResult {
	issues := []string{}

	// Valid conventional commit types
	validTypes := []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "chore", "build", "ci", "revert"}
	validTypesMap := make(map[string]bool)
	for _, t := range validTypes {
		validTypesMap[t] = true
	}

	// Pattern for conventional commit: type(scope): description
	// Scope is optional
	conventionalPattern := regexp.MustCompile(`^(\w+)(?:\(([^)]+)\))?!?: (.+)$`)

	// Check message length
	if len(message) > 72 {
		issues = append(issues, "Message exceeds 72 characters (consider using body for details)")
	}

	// Check for common issues
	if strings.HasSuffix(message, ".") {
		issues = append(issues, "Message should not end with a period")
	}

	// Check first word capitalization (should be lowercase for conventional commits)
	if len(message) > 0 && message[0] >= 'A' && message[0] <= 'Z' {
		issues = append(issues, "First word should be lowercase (type)")
	}

	// Match against conventional commit pattern
	matches := conventionalPattern.FindStringSubmatch(message)

	if matches == nil {
		// Not in conventional commit format
		return models.CommitValidationResult{
			Valid:           false,
			FormatCompliant: false,
			Issues:          append(issues, "Message does not follow conventional commit format: type(scope): description"),
			Message:         message,
		}
	}

	commitType := matches[1]
	scope := matches[2]
	description := matches[3]

	// Validate type
	if !validTypesMap[commitType] {
		issues = append(issues, fmt.Sprintf("Invalid type '%s' - must be one of: %s", commitType, strings.Join(validTypes, ", ")))
	}

	// Validate description
	if description == "" {
		issues = append(issues, "Description cannot be empty")
	}

	// Check description starts with lowercase (for non-proper nouns)
	if len(description) > 0 && description[0] >= 'A' && description[0] <= 'Z' {
		// This is a warning, not an error - might be a proper noun
		if !isProperNounStart(description) {
			issues = append(issues, "Description should start with lowercase (unless it's a proper noun)")
		}
	}

	valid := len(issues) == 0

	return models.CommitValidationResult{
		Valid:            valid,
		FormatCompliant:  true,
		Issues:           issues,
		Message:          message,
		ConventionalType: commitType,
		Scope:            scope,
	}
}

// isProperNounStart checks if description starts with what might be a proper noun
func isProperNounStart(description string) bool {
	// Common proper nouns in commit messages
	properNouns := []string{"API", "URL", "HTTP", "JSON", "XML", "SQL", "CSS", "HTML", "AWS", "GCP", "UI", "UX"}
	for _, noun := range properNouns {
		if strings.HasPrefix(description, noun) {
			return true
		}
	}
	return false
}

// handleValidatePush validates git push safety conditions
func (s *MCPServer) handleValidatePush(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	branch, _ := args["branch"].(string)
	isForce, _ := args["is_force"].(bool)
	hasUnpushedCommits, _ := args["has_unpushed_commits"].(bool)

	warnings := []string{}
	canPush := true
	valid := true

	// Check for force push
	if isForce {
		valid = false
		canPush = false
		warnings = append(warnings, "Force push detected - this can cause data loss for other team members")
		warnings = append(warnings, "Consider using 'git push --force-with-lease' instead")
	}

	// Check for main/master branch protection
	protectedBranches := []string{"main", "master", "production", "release"}
	for _, protected := range protectedBranches {
		if branch == protected || strings.HasPrefix(branch, protected+"/") {
			if !isForce {
				warnings = append(warnings, fmt.Sprintf("Pushing directly to '%s' branch - consider using a pull request", branch))
			} else {
				valid = false
				canPush = false
				warnings = append(warnings, fmt.Sprintf("FORCE PUSH to '%s' is highly discouraged and potentially dangerous", branch))
			}
		}
	}

	// Check for unpushed commits
	if !hasUnpushedCommits && !isForce {
		warnings = append(warnings, "No unpushed commits detected - push may be unnecessary")
	}

	// Check branch naming convention
	if branch == "" {
		valid = false
		canPush = false
		warnings = append(warnings, "Branch name is required")
	} else if strings.Contains(branch, " ") {
		valid = false
		warnings = append(warnings, "Branch name contains spaces - this is unconventional")
	}

	result := models.PushValidationResult{
		Valid:    valid,
		CanPush:  canPush,
		Warnings: warnings,
		Branch:   branch,
		IsForce:  isForce,
	}

	return buildToolResult(result, !valid)
}


