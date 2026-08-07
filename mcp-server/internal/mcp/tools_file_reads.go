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

func (s *MCPServer) handleVerifyFileRead(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionToken, _ := args["session_token"].(string)
	filePath, _ := args["file_path"].(string)

	// Validate required parameters
	if sessionToken == "" {
		result := models.FileReadVerificationResult{
			Valid:   false,
			Message: "session_token is required",
		}
		return buildToolResult(result, true)
	}

	if filePath == "" {
		result := models.FileReadVerificationResult{
			Valid:   false,
			Message: "file_path is required",
		}
		return buildToolResult(result, true)
	}

	// Validate session exists
	s.sessionsMu.RLock()
	session, exists := s.sessions[sessionToken]
	s.sessionsMu.RUnlock()

	if !exists {
		result := models.FileReadVerificationResult{
			Valid:     true,
			WasRead:   false,
			Message:   "Session not found or expired",
			SessionID: sessionToken,
			FilePath:  filePath,
		}
		return buildToolResult(result, false)
	}

	// Look up file read record using FileReadStore
	fileReadStore := database.NewFileReadStore(s.db)
	record, err := fileReadStore.GetBySessionAndPath(ctx, sessionToken, filePath)

	if err != nil {
		// File has not been read
		result := models.FileReadVerificationResult{
			Valid:     true,
			WasRead:   false,
			Message:   "File has not been read",
			SessionID: session.ID,
			FilePath:  filePath,
		}
		return buildToolResult(result, false)
	}

	// File was read - return success with timestamp
	result := models.FileReadVerificationResult{
		Valid:     true,
		WasRead:   true,
		ReadAt:    record.ReadAt.Format(time.RFC3339),
		SessionID: session.ID,
		FilePath:  filePath,
	}
	return buildToolResult(result, false)
}

// Helper function to format time for JSON responses
func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

// handleRecordFileRead records that a file was read via MCP Read tool
func (s *MCPServer) handleRecordFileRead(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	sessionToken, _ := args["session_token"].(string)
	filePath, _ := args["file_path"].(string)

	// Validate required parameters
	if sessionToken == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"session_token is required"}`}},
			IsError: true,
		}, nil
	}

	if filePath == "" {
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: `{"success":false,"error":"file_path is required"}`}},
			IsError: true,
		}, nil
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

	// Record the file read
	fileReadStore := database.NewFileReadStore(s.db)
	err := fileReadStore.CreateWithStrings(ctx, sessionToken, filePath)
	if err != nil {
		slog.Error("Failed to record file read", "error", err, "session_token", sessionToken, "file_path", filePath)
		return &mcp.CallToolResult{
			Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"success":false,"error":"Failed to record file read: %s"}`, jsonEscapeString(err.Error()))}},
			IsError: true,
		}, nil
	}

	// Return success confirmation
	return &mcp.CallToolResult{
		Content: []interface{}{mcp.TextContent{Type: "text", Text: fmt.Sprintf(`{"success":true,"session_token":"%s","file_path":"%s","recorded_at":"%s"}`, jsonEscapeString(sessionToken), jsonEscapeString(filePath), time.Now().Format(time.RFC3339))}},
	}, nil
}
