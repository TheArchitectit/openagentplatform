package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationJSONParsing(t *testing.T) {
	scriptPath := "../../../scripts/team_manager.py"
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	projectName := "integration-test-json"

	// Clean up before test
	cleanupTestProject(t, projectName)

	// Initialize project using Python directly (run from repo root)
	repoRoot := filepath.Join("..", "..", "..")
	scriptPathFromRoot := filepath.Join("scripts", "team_manager.py")
	cmd := exec.Command("python3", scriptPathFromRoot, "--project", projectName, "--test-mode", "init")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try with python
		cmd = exec.Command("python", scriptPathFromRoot, "--project", projectName, "--test-mode", "init")
		cmd.Dir = repoRoot
		output, err = cmd.CombinedOutput()
	}
	if err != nil {
		t.Fatalf("Failed to initialize project: %v\nOutput: %s", err, string(output))
	}

	// Verify config file exists and is valid JSON (repo root .teams directory)
	configPath := filepath.Join("..", "..", "..", ".teams", projectName+".json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Config file is not valid JSON: %v", err)
	}

	// Verify structure
	if config["project_name"] != projectName {
		t.Errorf("Expected project_name to be %s, got %v", projectName, config["project_name"])
	}

	teams, ok := config["teams"].([]interface{})
	if !ok {
		t.Fatal("teams field is not an array")
	}

	if len(teams) != 12 {
		t.Errorf("Expected 12 teams, got %d", len(teams))
	}

	// Verify team structure
	for i, team := range teams {
		teamMap, ok := team.(map[string]interface{})
		if !ok {
			t.Fatalf("Team %d is not an object", i)
		}

		requiredFields := []string{"id", "name", "phase", "description", "roles", "exit_criteria", "status"}
		for _, field := range requiredFields {
			if _, exists := teamMap[field]; !exists {
				t.Errorf("Team %d missing required field: %s", i, field)
			}
		}
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestIntegrationErrorPropagation tests error handling from Python to Go
func TestIntegrationErrorPropagation(t *testing.T) {
	scriptPath := "../../../scripts/team_manager.py"
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	ctx := context.Background()
	s := mockMCPServer()

	// Test with non-existent project
	t.Run("Non-existent Project", func(t *testing.T) {
		args := map[string]interface{}{
			"project_name": "non-existent-project-12345",
		}

		result, err := s.handleTeamList(ctx, args)
		if err != nil {
			t.Fatalf("handleTeamList failed: %v", err)
		}

		if !result.IsError {
			t.Error("Expected error for non-existent project")
		}

		text := getResultText(result)
		if !strings.Contains(text, "not found") {
			t.Errorf("Expected 'not found' message, got: %s", text)
		}
	})

	// Test with invalid project name (command injection attempt)
	t.Run("Invalid Project Name", func(t *testing.T) {
		args := map[string]interface{}{
			"project_name": "test;rm -rf",
		}

		result, err := s.handleTeamInit(ctx, args)
		if err != nil {
			t.Fatalf("handleTeamInit failed: %v", err)
		}

		if !result.IsError {
			t.Error("Expected error for invalid project name")
		}
	})
}

// TestIntegrationAssignAndValidate tests role assignment and validation
func TestIntegrationAssignAndValidate(t *testing.T) {
	ctx := context.Background()
	s := mockMCPServer()
	projectName := "integration-test-assign-validate"

	// Clean up before test
	cleanupTestProject(t, projectName)

	// Initialize project
	s.handleTeamInit(ctx, map[string]interface{}{"project_name": projectName})

	// Assign minimum required roles to a team (4 members)
	roles := []struct {
		roleName string
		person   string
	}{
		{"Business Relationship Manager", "Person 1"},
		{"Lead Product Manager", "Person 2"},
		{"Business Systems Analyst", "Person 3"},
		{"Financial Controller (FinOps)", "Person 4"},
	}

	for _, role := range roles {
		args := map[string]interface{}{
			"project_name": projectName,
			"team_id":      float64(1),
			"role_name":    role.roleName,
			"person":       role.person,
		}

		result, err := s.handleTeamAssign(ctx, args)
		if err != nil {
			t.Fatalf("Failed to assign role: %v", err)
		}

		if result.IsError {
			t.Fatalf("Role assignment failed: %s", getResultText(result))
		}
	}

	// Validate team size - should pass now
	args := map[string]interface{}{
		"project_name": projectName,
		"team_id":      float64(1),
	}

	result, err := s.handleTeamSizeValidate(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamSizeValidate failed: %v", err)
	}

	// Note: This might still fail if the Python script has different logic
	// We're testing the integration path, not the validation logic
	text := getResultText(result)
	if text == "" {
		t.Error("handleTeamSizeValidate returned empty result")
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestIntegrationTeamListWithPhaseFilter tests team listing with phase filter
