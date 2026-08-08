package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrationFullWorkflow tests the complete workflow: init -> assign -> list -> status

func TestIntegrationFullWorkflow(t *testing.T) {
	// Check if team_manager.py exists
	scriptPath := "../../../scripts/team_manager.py"
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	ctx := context.Background()
	s := mockMCPServer()
	projectName := "integration-test-workflow"

	// Clean up before test
	cleanupTestProject(t, projectName)

	// Step 1: Initialize project
	t.Run("Step 1: Initialize Project", func(t *testing.T) {
		args := map[string]interface{}{
			"project_name": projectName,
		}

		result, err := s.handleTeamInit(ctx, args)
		if err != nil {
			t.Fatalf("handleTeamInit failed: %v", err)
		}

		if result.IsError {
			t.Fatalf("handleTeamInit returned error: %s", getResultText(result))
		}

		text := getResultText(result)
		if !strings.Contains(text, "Initialized") {
			t.Errorf("Expected initialization message, got: %s", text)
		}

		// Verify file was created (repo root .teams directory)
		configPath := filepath.Join("..", "..", "..", ".teams", projectName+".json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Errorf("Config file was not created: %s", configPath)
		}
	})

	// Step 2: Assign roles to teams
	t.Run("Step 2: Assign Roles", func(t *testing.T) {
		assignments := []struct {
			teamID   float64
			roleName string
			person   string
		}{
			{1, "Business Relationship Manager", "Alice Smith"},
			{1, "Lead Product Manager", "Bob Jones"},
			{2, "Chief Architect", "Carol White"},
			{7, "Senior Backend Engineer", "David Brown"},
			{7, "Senior Frontend Engineer", "Eve Davis"},
		}

		for _, assignment := range assignments {
			args := map[string]interface{}{
				"project_name": projectName,
				"team_id":      assignment.teamID,
				"role_name":    assignment.roleName,
				"person":       assignment.person,
			}

			result, err := s.handleTeamAssign(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamAssign failed for team %d: %v", int(assignment.teamID), err)
			}

			if result.IsError {
				t.Errorf("handleTeamAssign returned error for team %d: %s", int(assignment.teamID), getResultText(result))
			}

			text := getResultText(result)
			if !strings.Contains(text, "Assigned") {
				t.Errorf("Expected assignment confirmation, got: %s", text)
			}
		}
	})

	// Step 3: List teams
	t.Run("Step 3: List Teams", func(t *testing.T) {
		args := map[string]interface{}{
			"project_name": projectName,
		}

		result, err := s.handleTeamList(ctx, args)
		if err != nil {
			t.Fatalf("handleTeamList failed: %v", err)
		}

		if result.IsError {
			t.Fatalf("handleTeamList returned error: %s", getResultText(result))
		}

		text := getResultText(result)

		// Verify team structure in output
		if !strings.Contains(text, "Team 1:") {
			t.Error("Expected Team 1 in output")
		}
		if !strings.Contains(text, "Team 7:") {
			t.Error("Expected Team 7 in output")
		}

		// Verify assigned names appear
		if !strings.Contains(text, "Alice Smith") {
			t.Error("Expected Alice Smith in output")
		}
		if !strings.Contains(text, "Carol White") {
			t.Error("Expected Carol White in output")
		}
	})

	// Step 4: Get phase status
	t.Run("Step 4: Get Phase Status", func(t *testing.T) {
		args := map[string]interface{}{
			"project_name": projectName,
		}

		result, err := s.handleTeamStatus(ctx, args)
		if err != nil {
			t.Fatalf("handleTeamStatus failed: %v", err)
		}

		if result.IsError {
			t.Fatalf("handleTeamStatus returned error: %s", getResultText(result))
		}

		text := getResultText(result)
		if text == "" {
			t.Error("handleTeamStatus returned empty result")
		}
	})

	// Step 5: Validate team sizes
	t.Run("Step 5: Validate Team Sizes", func(t *testing.T) {
		args := map[string]interface{}{
			"project_name": projectName,
		}

		result, err := s.handleTeamSizeValidate(ctx, args)
		if err != nil {
			t.Fatalf("handleTeamSizeValidate failed: %v", err)
		}

		// This may return error if teams are undersized (which they will be)
		// That's expected behavior - we're testing the integration, not the validation logic
		text := getResultText(result)
		if text == "" {
			t.Error("handleTeamSizeValidate returned empty result")
		}
	})

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestIntegrationPhaseGateCheck tests phase gate checking with Python integration
func TestIntegrationPhaseGateCheck(t *testing.T) {
	ctx := context.Background()
	s := mockMCPServer()
	projectName := "integration-test-gates"

	// Clean up before test
	cleanupTestProject(t, projectName)

	// Initialize project first
	s.handleTeamInit(ctx, map[string]interface{}{"project_name": projectName})

	// Test various phase transitions
	transitions := []struct {
		fromPhase float64
		toPhase   float64
		shouldPass bool
	}{
		{1, 2, true},   // Valid: 1_to_2
		{2, 3, true},   // Valid: 2_to_3
		{3, 4, true},   // Valid: 3_to_4
		{4, 5, true},   // Valid: 4_to_5
		{5, 6, false},  // Invalid: no 5_to_6 gate
		{1, 5, false},  // Invalid: no 1_to_5 gate
	}

	for _, tc := range transitions {
		t.Run(fmt.Sprintf("Phase %d to %d", int(tc.fromPhase), int(tc.toPhase)), func(t *testing.T) {
			args := map[string]interface{}{
				"project_name": projectName,
				"from_phase":   tc.fromPhase,
				"to_phase":     tc.toPhase,
			}

			result, err := s.handlePhaseGateCheck(ctx, args)
			if err != nil {
				t.Fatalf("handlePhaseGateCheck failed: %v", err)
			}

			if tc.shouldPass && result.IsError {
				t.Errorf("Expected phase gate %d_to_%d to pass, got error: %s",
					int(tc.fromPhase), int(tc.toPhase), getResultText(result))
			}

			if !tc.shouldPass && !result.IsError {
				t.Errorf("Expected phase gate %d_to_%d to fail, but it passed",
					int(tc.fromPhase), int(tc.toPhase))
			}
		})
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestIntegrationAgentTeamMap tests agent team mapping
func TestIntegrationAgentTeamMap(t *testing.T) {
	ctx := context.Background()
	s := mockMCPServer()

	agentTypes := []string{
		"planner",
		"architect",
		"infrastructure",
		"platform",
		"backend",
		"frontend",
		"security",
		"qa",
		"sre",
		"ops",
	}

	for _, agentType := range agentTypes {
		t.Run("Agent: "+agentType, func(t *testing.T) {
			args := map[string]interface{}{
				"agent_type": agentType,
			}

			result, err := s.handleAgentTeamMap(ctx, args)
			if err != nil {
				t.Fatalf("handleAgentTeamMap failed: %v", err)
			}

			if result.IsError {
				t.Errorf("handleAgentTeamMap returned error for agent %s: %s",
					agentType, getResultText(result))
			}

			text := getResultText(result)

			// Verify structure
			if !strings.Contains(text, "Agent Team Assignment") {
				t.Error("Expected 'Agent Team Assignment' header")
			}
			if !strings.Contains(text, "Agent Type:") {
				t.Error("Expected 'Agent Type' field")
			}
			if !strings.Contains(text, "Assigned Team:") {
				t.Error("Expected 'Assigned Team' field")
			}
			if !strings.Contains(text, "Phase:") {
				t.Error("Expected 'Phase' field")
			}
			if !strings.Contains(text, "Roles:") {
				t.Error("Expected 'Roles' field")
			}
		})
	}
}

// TestIntegrationJSONParsing tests JSON output from Python is parsed correctly
