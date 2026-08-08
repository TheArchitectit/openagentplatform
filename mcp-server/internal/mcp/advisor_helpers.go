package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/thearchitectit/guardrail-mcp/internal/models"
)

func (s *MCPServer) handleAdvisorResolve(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	advisorID, _ := args["advisor_id"].(string)
	resolutionStatus, _ := args["resolution_status"].(string)
	justification, _ := args["justification"].(string)

	if advisorID == "" {
		return buildToolResult(map[string]string{
			"error": "advisor_id is required",
		}, true)
	}

	if resolutionStatus == "" {
		return buildToolResult(map[string]string{
			"error": "resolution_status is required (applied, bypassed_with_risk, false_positive)",
		}, true)
	}

	validStatuses := map[string]bool{
		"applied":            true,
		"bypassed_with_risk": true,
		"false_positive":     true,
	}

	if !validStatuses[resolutionStatus] {
		return buildToolResult(map[string]string{
			"error": fmt.Sprintf("Invalid resolution_status: %s", resolutionStatus),
		}, true)
	}

	if justification == "" {
		return buildToolResult(map[string]string{
			"error": "justification is required",
		}, true)
	}

	advisors := models.StandardAdvisors()
	advisor, ok := advisors[advisorID]
	if !ok {
		return buildToolResult(map[string]string{
			"error": fmt.Sprintf("Advisor not found: %s", advisorID),
		}, true)
	}

	unblocked := resolutionStatus == "applied" || resolutionStatus == "false_positive"

	result := models.AdvisorResolveResult{
		Success:   true,
		AdvisorID: advisorID,
		Status:    resolutionStatus,
		Message:   fmt.Sprintf("%s advice resolved: %s", advisor.Name, resolutionStatus),
		Unblocked: unblocked,
	}

	return buildToolResult(result, false)
}

// readFileIfExists reads a file if it exists, returns empty string otherwise
func readFileIfExists(path string) string {
	// This is a simplified version - in production, this would check if file
	// is within authorized scope and handle errors properly
	return ""
}

// contains checks if a string slice contains a value
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// isPatternMatch checks if text matches a glob pattern
func isPatternMatch(text, pattern string) bool {
	// Simple glob matching - convert glob to regex
	// * -> .*
	// ? -> .
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false
	}

	return re.MatchString(text)
}
