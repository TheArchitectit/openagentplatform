package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
)

// mockMCPServer creates a minimal MCPServer for testing

func mockMCPServer() *MCPServer {
	return &MCPServer{
		sessions: make(map[string]*Session),
	}
}

// TestValidateProjectName tests the project name validation function
func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		project string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple name",
			project: "my-project",
			wantErr: false,
		},
		{
			name:    "valid with underscore",
			project: "my_project_123",
			wantErr: false,
		},
		{
			name:    "valid with numbers",
			project: "project123",
			wantErr: false,
		},
		{
			name:    "empty name",
			project: "",
			wantErr: true,
			errMsg:  "project_name is required",
		},
		{
			name:    "too long",
			project: strings.Repeat("a", 65),
			wantErr: true,
			errMsg:  "project_name must be 64 characters or less",
		},
		{
			name:    "invalid with space",
			project: "my project",
			wantErr: true,
			errMsg:  "project_name must contain only letters, numbers, hyphens, and underscores",
		},
		{
			name:    "invalid with special char",
			project: "project;rm -rf",
			wantErr: true,
			errMsg:  "project_name must contain only letters, numbers, hyphens, and underscores",
		},
		{
			name:    "invalid with slash",
			project: "project/test",
			wantErr: true,
			errMsg:  "project_name must contain only letters, numbers, hyphens, and underscores",
		},
		{
			name:    "invalid with dot",
			project: "project.json",
			wantErr: true,
			errMsg:  "project_name must contain only letters, numbers, hyphens, and underscores",
		},
		{
			name:    "command injection attempt",
			project: "project$(whoami)",
			wantErr: true,
			errMsg:  "project_name must contain only letters, numbers, hyphens, and underscores",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProjectName(tt.project)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateProjectName(%q) expected error, got nil", tt.project)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateProjectName(%q) error = %v, want error containing %q", tt.project, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateProjectName(%q) unexpected error: %v", tt.project, err)
				}
			}
		})
	}
}

// TestHandleTeamInit_Valid tests handleTeamInit with valid input
func TestHandleTeamInit_Valid(t *testing.T) {
	// Skip if Python is not available
	if _, err := os.Stat("../../../scripts/team_manager.py"); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	s := mockMCPServer()
	ctx := context.Background()

	// Use a unique project name for testing
	projectName := "test-project-init"
	args := map[string]interface{}{
		"project_name": projectName,
	}

	result, err := s.handleTeamInit(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamInit returned error: %v", err)
	}

	if result == nil {
		t.Fatal("handleTeamInit returned nil result")
	}

	// Check that result is not an error
	if result.IsError {
		t.Errorf("handleTeamInit returned error result: %v", getResultText(result))
	}

	// Check for expected content
	text := getResultText(result)
	if !strings.Contains(text, "Initialized") && !strings.Contains(text, "Initialized project") {
		t.Errorf("handleTeamInit result does not contain expected content: %s", text)
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestHandleTeamInit_MissingProjectName tests handleTeamInit with missing project_name
func TestHandleTeamInit_MissingProjectName(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "nil args",
			args: nil,
		},
		{
			name: "empty args",
			args: map[string]interface{}{},
		},
		{
			name: "empty project_name",
			args: map[string]interface{}{"project_name": ""},
		},
		{
			name: "wrong type",
			args: map[string]interface{}{"project_name": 123},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.handleTeamInit(ctx, tt.args)
			if err != nil {
				t.Fatalf("handleTeamInit returned error: %v", err)
			}

			if result == nil {
				t.Fatal("handleTeamInit returned nil result")
			}

			if !result.IsError {
				t.Error("handleTeamInit should return error result for invalid input")
			}

			text := getResultText(result)
			if !strings.Contains(text, "project_name is required") {
				t.Errorf("handleTeamInit error should mention 'project_name is required', got: %s", text)
			}
		})
	}
}

// TestHandleTeamInit_InvalidProjectName tests handleTeamInit with invalid project names
func TestHandleTeamInit_InvalidProjectName(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name    string
		project string
	}{
		{
			name:    "with spaces",
			project: "invalid project",
		},
		{
			name:    "with semicolon",
			project: "project;rm -rf",
		},
		{
			name:    "too long",
			project: strings.Repeat("a", 65),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{"project_name": tt.project}
			result, err := s.handleTeamInit(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamInit returned error: %v", err)
			}

			if !result.IsError {
				t.Error("handleTeamInit should return error result for invalid project name")
			}
		})
	}
}

// TestHandleTeamList_Valid tests handleTeamList with valid input
func TestHandleTeamList_Valid(t *testing.T) {
	// Skip if Python is not available
	if _, err := os.Stat("../../../scripts/team_manager.py"); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	s := mockMCPServer()
	ctx := context.Background()
	projectName := "test-project-list"

	// Initialize project first
	initArgs := map[string]interface{}{"project_name": projectName}
	s.handleTeamInit(ctx, initArgs)

	// Now list teams
	args := map[string]interface{}{
		"project_name": projectName,
	}

	result, err := s.handleTeamList(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamList returned error: %v", err)
	}

	if result.IsError {
		t.Errorf("handleTeamList returned error result: %v", getResultText(result))
	}

	text := getResultText(result)
	if text == "" {
		t.Error("handleTeamList returned empty result")
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestHandleTeamList_MissingProjectName tests handleTeamList with missing project_name
func TestHandleTeamList_MissingProjectName(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	result, err := s.handleTeamList(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleTeamList returned error: %v", err)
	}

	if !result.IsError {
		t.Error("handleTeamList should return error for missing project_name")
	}
}

// TestHandleTeamList_WithPhaseFilter tests handleTeamList with phase filter (SEC-010)
func TestHandleTeamList_WithPhaseFilter(t *testing.T) {
	// Skip if Python is not available
	if _, err := os.Stat("../../../scripts/team_manager.py"); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	s := mockMCPServer()
	ctx := context.Background()
	projectName := "test-project-list-phase"

	// Initialize project
	initArgs := map[string]interface{}{"project_name": projectName}
	s.handleTeamInit(ctx, initArgs)

	// List with phase filter - SEC-010: Now uses strict "Phase 1" format
	args := map[string]interface{}{
		"project_name": projectName,
		"phase":        "Phase 1",
	}

	result, err := s.handleTeamList(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamList returned error: %v", err)
	}

	if result.IsError {
		t.Errorf("handleTeamList returned error result: %v", getResultText(result))
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestHandleTeamAssign_Valid tests handleTeamAssign with valid input
