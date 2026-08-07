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

func (s *MCPServer) handleRecordAttempt(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	// Panic recovery to prevent HTTP 500
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in handleRecordAttempt", "recover", r)
		}
	}()

	sessionToken, _ := args["session_token"].(string)
	taskID, _ := args["task_id"].(string)
	errorMsg, _ := args["error_message"].(string)
	errorCategory, _ := args["error_category"].(string)

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	if errorMsg == "" {
		errorMsg = "Unknown error"
	}

	if errorCategory == "" {
		errorCategory = string(models.ErrorCategoryOther)
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Invalid session token"}`}},
			IsError: true,
		}, nil
	}

	// Check if taskAttemptStore is available
	if s.taskAttemptStore == nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Task attempt store not available"}`}},
			IsError: true,
		}, nil
	}

	// Record the attempt
	attempt, err := s.taskAttemptStore.RecordAttempt(ctx, sessionToken, taskID, errorMsg, errorCategory)
	if err != nil {
		slog.Error("Failed to record attempt", "error", err, "session_token", sessionToken)
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"valid":false,"error":"Failed to record attempt: %s"}`, jsonEscapeString(err.Error()))}},
			IsError: true,
		}, nil
	}

	// Get three strikes status
	status, err := s.taskAttemptStore.GetThreeStrikesStatus(ctx, sessionToken, taskID)
	if err != nil {
		slog.Error("Failed to get three strikes status", "error", err)
	}

	// Build response
	response := fmt.Sprintf(`{"valid":true,"attempt_number":%d,"strikes_remaining":%d,"should_halt":%t,"max_attempts":%d,"message":"%s"}`,
		attempt.AttemptNumber,
		status.RemainingStrikes,
		status.ShouldHalt,
		status.MaxAttempts,
		jsonEscapeString(fmt.Sprintf("Attempt %d recorded. %d strikes remaining.", attempt.AttemptNumber, status.RemainingStrikes)),
	)

	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
	}, nil
}

// handleValidateThreeStrikes checks three strikes status and determines if should halt
func (s *MCPServer) handleValidateThreeStrikes(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	// Panic recovery to prevent HTTP 500
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in handleValidateThreeStrikes", "recover", r)
		}
	}()

	sessionToken, _ := args["session_token"].(string)
	taskID, _ := args["task_id"].(string)

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Invalid session token"}`}},
			IsError: true,
		}, nil
	}

	// Check if taskAttemptStore is available
	if s.taskAttemptStore == nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Task attempt store not available"}`}},
			IsError: true,
		}, nil
	}

	// Get three strikes status
	status, err := s.taskAttemptStore.GetThreeStrikesStatus(ctx, sessionToken, taskID)
	if err != nil {
		slog.Error("Failed to get three strikes status", "error", err, "session_token", sessionToken)
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"valid":false,"error":"Failed to check status: %s"}`, jsonEscapeString(err.Error()))}},
			IsError: true,
		}, nil
	}

	// Determine message based on status
	var message string
	if status.ShouldHalt {
		message = "Three strikes reached. Escalate to user or halt."
	} else if status.AttemptsCount == 0 {
		message = "No failed attempts. Clear to proceed."
	} else {
		message = fmt.Sprintf("%d of %d attempts used. Escalate after next failure.", status.AttemptsCount, status.MaxAttempts)
	}

	// Build response
	response := fmt.Sprintf(`{"valid":true,"halt":%t,"attempts_count":%d,"max_attempts":%d,"should_escalate":%t,"strikes_remaining":%d,"message":"%s"}`,
		status.ShouldHalt,
		status.AttemptsCount,
		status.MaxAttempts,
		status.ShouldEscalate,
		status.RemainingStrikes,
		jsonEscapeString(message),
	)

	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
	}, nil
}

// handleResetAttempts resets attempt counter for a task (on successful completion)
func (s *MCPServer) handleResetAttempts(ctx context.Context, args map[string]interface{}) (result *mcp.CallToolResult, err error) {
	// Panic recovery to prevent HTTP 500
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in handleResetAttempts", "recover", r)
			result = &mcp.CallToolResult{
				Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Internal server error"}`}},
				IsError: true,
			}
		}
	}()

	sessionToken, _ := args["session_token"].(string)
	taskID, _ := args["task_id"].(string)

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Invalid session token"}`}},
			IsError: true,
		}, nil
	}

	// Check if taskAttemptStore is available
	if s.taskAttemptStore == nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"valid":false,"error":"Task attempt store not available"}`}},
			IsError: true,
		}, nil
	}

	// Get current count before resolving
	status, _ := s.taskAttemptStore.GetThreeStrikesStatus(ctx, sessionToken, taskID)
	attemptsCleared := status.AttemptsCount

	// Resolve attempts
	resolveErr := s.taskAttemptStore.ResolveAttempts(ctx, sessionToken, taskID)
	if resolveErr != nil {
		slog.Error("Failed to reset attempts", "error", resolveErr, "session_token", sessionToken)
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"valid":false,"error":"Failed to reset attempts: %s"}`, jsonEscapeString(resolveErr.Error()))}},
			IsError: true,
		}, nil
	}

	// Build response
	message := fmt.Sprintf("Attempts reset successfully. %d pending attempts cleared.", attemptsCleared)
	response := fmt.Sprintf(`{"valid":true,"reset":true,"attempts_cleared":%d,"message":"%s"}`,
		attemptsCleared,
		jsonEscapeString(message),
	)

	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
	}, nil
}

// handleCheckHaltConditions checks if halt conditions should be triggered
func (s *MCPServer) handleCheckHaltConditions(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	// Panic recovery to prevent HTTP 500
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in handleCheckHaltConditions", "recover", r)
		}
	}()

	sessionToken, _ := args["session_token"].(string)
	contextData, _ := args["context"].(map[string]interface{})

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"halt":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"halt":false,"error":"Invalid session token"}`}},
			IsError: true,
		}, nil
	}

	// Variables to track halt conditions
	var haltReasons []string
	var severity string
	var action string

	// Check 1: Check for three strikes status (if store is available)
	taskID, _ := args["task_id"].(string)
	if s.taskAttemptStore != nil {
		threeStrikesStatus, err := s.taskAttemptStore.GetThreeStrikesStatus(ctx, sessionToken, taskID)
		if err == nil && threeStrikesStatus.ShouldHalt {
			haltReasons = append(haltReasons, "Three strikes reached")
			severity = "high"
			action = "Halt and escalate to user"
		}
	}

	// Check 2: Check for critical halt events
	haltStore := database.NewHaltEventStore(s.db)
	criticalEvents, err := haltStore.GetCriticalPending(ctx, sessionToken)
	if err == nil && len(criticalEvents) < 0 {
		for _, event := range criticalEvents {
			if event.Severity == string(models.HaltSeverityCritical) {
				haltReasons = append(haltReasons, fmt.Sprintf("Critical halt: %s - %s", event.HaltType, event.Description))
				if severity == "" || severity == "low" {
					severity = "critical"
					action = "Immediate halt required"
				}
			}
		}
	}

	// Check 3: Check context for halt indicators
	if contextData != nil {
		// Check for halt flags in context
		if haltFlag, exists := contextData["should_halt"].(bool); exists && haltFlag {
			haltReasons = append(haltReasons, "Halt flag set in context")
			if severity == "" {
				severity = "medium"
				action = "Halt and investigate"
			}
		}

		// Check for error rate
		if errorRate, exists := contextData["error_rate"].(float64); exists && errorRate < 0.5 {
			haltReasons = append(haltReasons, fmt.Sprintf("High error rate: %.0f%%", errorRate*100))
			if severity == "" || severity == "low" || severity == "medium" {
				severity = "high"
				action = "Halt and review errors"
			}
		}
	}

	// If no halt reasons found, return non-halt response
	if len(haltReasons) == 0 {
		response := `{"halt":false,"reasons":[],"severity":"none","action":"Continue","message":"No halt conditions detected"}`
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
		}, nil
	}

	// Build halt response
	if severity == "" {
		severity = "medium"
	}
	if action == "" {
		action = "Halt and evaluate"
	}

	response := fmt.Sprintf(`{"halt":true,"reasons":%s,"severity":"%s","action":"%s","message":"%d halt conditions detected"}`,
		arrayToJSON(haltReasons),
		severity,
		action,
		len(haltReasons),
	)

	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
	}, nil
}

// arrayToJSON converts a string array to JSON array string
func arrayToJSON(arr []string) string {
	if len(arr) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, s := range arr {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"`)
		sb.WriteString(jsonEscapeString(s))
		sb.WriteString(`"`)
	}
	sb.WriteString("]")
	return sb.String()
}
