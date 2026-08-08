package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationTeamListWithPhaseFilter(t *testing.T) {
	ctx := context.Background()
	s := mockMCPServer()
	projectName := "integration-test-phase-filter"

	// Clean up before test
	cleanupTestProject(t, projectName)

	// Initialize project
	s.handleTeamInit(ctx, map[string]interface{}{"project_name": projectName})

	// Test different phases (SEC-010: Only Phase 1, Phase 2, Phase 3 are valid)
	phases := []string{
		"Phase 1",
		"Phase 2",
		"Phase 3",
	}

	for _, phase := range phases {
		t.Run("Phase: "+phase, func(t *testing.T) {
			args := map[string]interface{}{
				"project_name": projectName,
				"phase":        phase,
			}

			result, err := s.handleTeamList(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamList failed: %v", err)
			}

			if result.IsError {
				t.Fatalf("handleTeamList returned error: %s", getResultText(result))
			}

			text := getResultText(result)

			// Verify the team list returned successfully (phase filter applied)
			if text == "" {
				t.Errorf("Expected non-empty output for phase '%s'", phase)
			}
		})
	}

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestIntegrationMultipleProjects tests handling multiple projects
func TestIntegrationMultipleProjects(t *testing.T) {
	ctx := context.Background()
	s := mockMCPServer()

	projects := []string{
		"integration-test-multi-1",
		"integration-test-multi-2",
		"integration-test-multi-3",
	}

	// Clean up before test
	for _, project := range projects {
		cleanupTestProject(t, project)
	}

	// Initialize all projects
	for _, project := range projects {
		args := map[string]interface{}{
			"project_name": project,
		}

		result, err := s.handleTeamInit(ctx, args)
		if err != nil {
			t.Fatalf("Failed to initialize project %s: %v", project, err)
		}

		if result.IsError {
			t.Fatalf("Error initializing project %s: %s", project, getResultText(result))
		}
	}

	// Verify each project has separate config (repo root .teams directory)
	for _, project := range projects {
		configPath := filepath.Join("..", "..", "..", ".teams", project+".json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Errorf("Config file for project %s was not created", project)
		}
	}

	// Clean up
	for _, project := range projects {
		cleanupTestProject(t, project)
	}
}

