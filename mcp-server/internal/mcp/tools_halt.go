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

func (s *MCPServer) handleRecordHalt(ctx context.Context, args map[string]interface{}) (result *mcp.CallToolResult, err error) {
	// Panic recovery to prevent HTTP 500
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in handleRecordHalt", "recover", r)
			result = &mcp.CallToolResult{
				Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"Internal server error"}`}},
				IsError: true,
			}
		}
	}()

	sessionToken, _ := args["session_token"].(string)
	haltType, _ := args["halt_type"].(string)
	description, _ := args["description"].(string)
	severity, _ := args["severity"].(string)
	contextData, _ := args["context"].(interface{})

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	if haltType == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"halt_type is required"}`}},
			IsError: true,
		}, nil
	}

	if description == "" {
		description = "Unspecified halt condition"
	}

	if severity == "" {
		severity = "medium"
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"Invalid session token"}`}},
			IsError: true,
		}, nil
	}

	// Check if database is available
	if s.db == nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"Database not available"}`}},
			IsError: true,
		}, nil
	}

	// Safe type assertion for contextData
	var contextMap map[string]interface{}
	if contextData != nil {
		if cm, ok := contextData.(map[string]interface{}); ok {
			contextMap = cm
		}
	}

	// Record the halt event
	haltStore := database.NewHaltEventStore(s.db)
	recordID, haltErr := haltStore.Create(ctx, sessionToken, haltType, severity, description, contextMap)
	if haltErr != nil {
		slog.Error("Failed to record halt", "error", haltErr, "session_token", sessionToken)
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"success":false,"error":"Failed to record halt: %s"}`, jsonEscapeString(haltErr.Error()))}},
			IsError: true,
		}, nil
	}

	// Return success confirmation
	response := fmt.Sprintf(`{"success":true,"halt_id":"%s","recorded_at":"%s","status":"recorded"}`,
		recordID.ID,
		time.Now().Format(time.RFC3339),
	)

	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
	}, nil
}

// handleAcknowledgeHalt acknowledges a halt event and sets resolution status
func (s *MCPServer) handleAcknowledgeHalt(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionToken, _ := args["session_token"].(string)
	haltID, _ := args["halt_id"].(string)
	resolution, _ := args["resolution"].(string)

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	if haltID == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"halt_id is required"}`}},
			IsError: true,
		}, nil
	}

	if resolution == "" {
		resolution = string(models.ResolutionPending)
	}

	// Validate session exists
	s.sessionsMu.RLock()
	_, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"Invalid session token"}`}},
			IsError: true,
		}, nil
	}

	// Get UUID from halt_id
	haltUUID := uuid.UUID{}
	if err := haltUUID.UnmarshalBinary([]byte(haltID)); err != nil {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"Invalid halt_id format"}`}},
			IsError: true,
		}, nil
	}

	// Acknowledge the halt event
	haltStore := database.NewHaltEventStore(s.db)
	_, err := haltStore.Acknowledge(ctx, haltUUID, resolution)
	if err != nil {
		slog.Error("Failed to acknowledge halt", "error", err, "halt_id", haltID)
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"success":false,"error":"Failed to acknowledge halt: %s"}`, jsonEscapeString(err.Error()))}},
			IsError: true,
		}, nil
	}

	// Return success confirmation
	response := fmt.Sprintf(`{"success":true,"halt_id":"%s","acknowledged_at":"%s","resolution":"%s"}`,
		haltID,
		time.Now().Format(time.RFC3339),
		resolution,
	)

	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: response}},
	}, nil
}
