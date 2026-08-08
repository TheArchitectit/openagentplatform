package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestHandleTeamAssign_Valid(t *testing.T) {
	// Skip if Python is not available
	if _, err := os.Stat("../../../scripts/team_manager.py"); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	s := mockMCPServer()
	ctx := context.Background()
	projectName := "test-project-assign"

	// Initialize project first
	initArgs := map[string]interface{}{"project_name": projectName}
	s.handleTeamInit(ctx, initArgs)

	// Assign role
	args := map[string]interface{}{
		"project_name": projectName,
		"team_id":      float64(1),
		"role_name":    "Business Relationship Manager",
		"person":       "John Doe",
	}

	result, err := s.handleTeamAssign(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamAssign returned error: %v", err)
	}

	if result.IsError {
		t.Errorf("handleTeamAssign returned error result: %v", getResultText(result))
	}

	text := getResultText(result)
	if !strings.Contains(text, "Assigned") && !strings.Contains(text, "John Doe") {
		t.Errorf("handleTeamAssign result does not contain expected content: %s", text)
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestHandleTeamAssign_MissingFields tests handleTeamAssign with missing required fields
func TestHandleTeamAssign_MissingFields(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "missing project_name",
			args: map[string]interface{}{
				"team_id":   float64(1),
				"role_name": "Test Role",
				"person":    "Test Person",
			},
		},
		{
			name: "missing team_id",
			args: map[string]interface{}{
				"project_name": "test",
				"role_name":    "Test Role",
				"person":       "Test Person",
			},
		},
		{
			name: "missing role_name",
			args: map[string]interface{}{
				"project_name": "test",
				"team_id":      float64(1),
				"person":       "Test Person",
			},
		},
		{
			name: "missing person",
			args: map[string]interface{}{
				"project_name": "test",
				"team_id":      float64(1),
				"role_name":    "Test Role",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.handleTeamAssign(ctx, tt.args)
			if err != nil {
				t.Fatalf("handleTeamAssign returned error: %v", err)
			}

			if !result.IsError {
				t.Errorf("handleTeamAssign should return error for %s", tt.name)
			}
		})
	}
}

// TestHandleTeamAssign_InvalidTeamID tests handleTeamAssign with invalid team_id
func TestHandleTeamAssign_InvalidTeamID(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name   string
		teamID float64
	}{
		{
			name:   "team_id zero",
			teamID: 0,
		},
		{
			name:   "team_id negative",
			teamID: -1,
		},
		{
			name:   "team_id too high",
			teamID: 13,
		},
		{
			name:   "team_id 99",
			teamID: 99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"project_name": "test-project",
				"team_id":      tt.teamID,
				"role_name":    "Test Role",
				"person":       "Test Person",
			}
			result, err := s.handleTeamAssign(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamAssign returned error: %v", err)
			}

			if !result.IsError {
				t.Error("handleTeamAssign should return error for invalid team_id")
			}

			text := getResultText(result)
			if !strings.Contains(text, "team_id must be between 1 and 12") {
				t.Errorf("Expected error message about team_id range, got: %s", text)
			}
		})
	}
}

// TestHandleTeamStatus_Valid tests handleTeamStatus with valid input
