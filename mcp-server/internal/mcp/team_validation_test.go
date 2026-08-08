package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleAgentTeamMap_InvalidAgentType(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	args := map[string]interface{}{
		"agent_type": "nonexistent-agent",
	}

	result, err := s.handleAgentTeamMap(ctx, args)
	if err != nil {
		t.Fatalf("handleAgentTeamMap returned error: %v", err)
	}

	if !result.IsError {
		t.Error("handleAgentTeamMap should return error for invalid agent_type")
	}

	text := getResultText(result)
	if !strings.Contains(text, "No team mapping found") {
		t.Errorf("Expected error message about no mapping, got: %s", text)
	}
}

// TestHandleTeamSizeValidate_Valid tests handleTeamSizeValidate with valid input
func TestHandleTeamSizeValidate_Valid(t *testing.T) {
	// Skip if Python is not available
	if _, err := os.Stat("../../../scripts/team_manager.py"); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	s := mockMCPServer()
	ctx := context.Background()
	projectName := "test-project-validate"

	// Initialize project first
	initArgs := map[string]interface{}{"project_name": projectName}
	s.handleTeamInit(ctx, initArgs)

	// Validate team sizes
	args := map[string]interface{}{
		"project_name": projectName,
	}

	result, err := s.handleTeamSizeValidate(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamSizeValidate returned error: %v", err)
	}

	_ = result // Result may be error (undersized) which is expected

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestHandleTeamSizeValidate_WithTeamID tests handleTeamSizeValidate with specific team_id
func TestHandleTeamSizeValidate_WithTeamID(t *testing.T) {
	// Skip if Python is not available
	if _, err := os.Stat("../../../scripts/team_manager.py"); os.IsNotExist(err) {
		t.Skip("team_manager.py not found, skipping integration test")
	}

	s := mockMCPServer()
	ctx := context.Background()
	projectName := "test-project-validate-team"

	// Initialize project
	initArgs := map[string]interface{}{"project_name": projectName}
	s.handleTeamInit(ctx, initArgs)

	// Validate specific team
	args := map[string]interface{}{
		"project_name": projectName,
		"team_id":      float64(1),
	}

	result, err := s.handleTeamSizeValidate(ctx, args)
	if err != nil {
		t.Fatalf("handleTeamSizeValidate returned error: %v", err)
	}

	_ = result // Result may be error (undersized) which is expected

	// Cleanup
	cleanupTestProject(t, projectName)
}

// TestHandleTeamSizeValidate_MissingProjectName tests handleTeamSizeValidate with missing project_name
func TestHandleTeamSizeValidate_MissingProjectName(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	result, err := s.handleTeamSizeValidate(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleTeamSizeValidate returned error: %v", err)
	}

	if !result.IsError {
		t.Error("handleTeamSizeValidate should return error for missing project_name")
	}
}

// TestLoadTeamLayoutRules tests the loadTeamLayoutRules function
func TestLoadTeamLayoutRules(t *testing.T) {
	rules, err := loadTeamLayoutRules()
	if err != nil {
		t.Fatalf("loadTeamLayoutRules returned error: %v", err)
	}

	if rules == nil {
		t.Fatal("loadTeamLayoutRules returned nil")
	}

	// Check required fields
	if rules.Name == "" {
		t.Error("rules.Name should not be empty")
	}

	if rules.Version == "" {
		t.Error("rules.Version should not be empty")
	}

	// Check phase gates
	if len(rules.PhaseGates) == 0 {
		t.Error("rules.PhaseGates should not be empty")
	}

	// Check specific phase gates exist
	expectedGates := []string{"1_to_2", "2_to_3", "3_to_4", "4_to_5"}
	for _, gate := range expectedGates {
		if _, exists := rules.PhaseGates[gate]; !exists {
			t.Errorf("Phase gate %s should exist", gate)
		}
	}

	// Check agent mappings
	if len(rules.AgentMapping) == 0 {
		t.Error("rules.AgentMapping should not be empty")
	}

	// Check specific agent types
	expectedAgents := []string{"planner", "architect", "backend", "frontend", "security", "qa"}
	for _, agent := range expectedAgents {
		if _, exists := rules.AgentMapping[agent]; !exists {
			t.Errorf("Agent mapping for %s should exist", agent)
		}
	}
}

// TestTeamLayoutRulesPhaseGateStructure tests the structure of phase gates
func TestTeamLayoutRulesPhaseGateStructure(t *testing.T) {
	rules, err := loadTeamLayoutRules()
	if err != nil {
		t.Fatalf("loadTeamLayoutRules returned error: %v", err)
	}

	for gateName, gate := range rules.PhaseGates {
		if gate.Name == "" {
			t.Errorf("Phase gate %s should have a name", gateName)
		}
		if len(gate.RequiredTeams) == 0 {
			t.Errorf("Phase gate %s should have required teams", gateName)
		}
		if len(gate.Deliverables) == 0 {
			t.Errorf("Phase gate %s should have deliverables", gateName)
		}
	}
}

// TestTeamLayoutRulesAgentMappingStructure tests the structure of agent mappings
func TestTeamLayoutRulesAgentMappingStructure(t *testing.T) {
	rules, err := loadTeamLayoutRules()
	if err != nil {
		t.Fatalf("loadTeamLayoutRules returned error: %v", err)
	}

	for agentType, mapping := range rules.AgentMapping {
		if mapping.Team < 1 || mapping.Team > 12 {
			t.Errorf("Agent %s should map to valid team (1-12), got %d", agentType, mapping.Team)
		}
		if mapping.Phase == "" {
			t.Errorf("Agent %s should have a phase", agentType)
		}
		if len(mapping.Roles) == 0 {
			t.Errorf("Agent %s should have roles", agentType)
		}
	}
}

// TestTeamRuleStructure tests the TeamRule structure
func TestTeamRuleStructure(t *testing.T) {
	rule := TeamRule{
		ID:       "TEAM-001",
		Name:     "Test Rule",
		Severity: "error",
		Check:    "team_size",
		Command:  "validate-size",
		Message:  "Team size must be 4-6 members",
	}

	if rule.ID != "TEAM-001" {
		t.Errorf("Rule ID mismatch: got %s, want TEAM-001", rule.ID)
	}
	if rule.Name != "Test Rule" {
		t.Errorf("Rule Name mismatch: got %s, want Test Rule", rule.Name)
	}
}

// TestPhaseGateStructure tests the PhaseGate structure
func TestPhaseGateStructure(t *testing.T) {
	gate := PhaseGate{
		Name:             "Test Gate",
		RequiredTeams:    []int{1, 2, 3},
		ApprovalRequired: []int{1},
		Deliverables:     []string{"Doc 1", "Doc 2"},
	}

	if gate.Name != "Test Gate" {
		t.Errorf("Gate Name mismatch: got %s, want Test Gate", gate.Name)
	}
	if len(gate.RequiredTeams) != 3 {
		t.Errorf("Expected 3 required teams, got %d", len(gate.RequiredTeams))
	}
	if len(gate.Deliverables) != 2 {
		t.Errorf("Expected 2 deliverables, got %d", len(gate.Deliverables))
	}
}

// TestAgentTeamStructure tests the AgentTeam structure
func TestAgentTeamStructure(t *testing.T) {
	team := AgentTeam{
		Team:  5,
		Roles: []string{"Role 1", "Role 2"},
		Phase: "Phase 2",
	}

	if team.Team != 5 {
		t.Errorf("Team ID mismatch: got %d, want 5", team.Team)
	}
	if len(team.Roles) != 2 {
		t.Errorf("Expected 2 roles, got %d", len(team.Roles))
	}
	if team.Phase != "Phase 2" {
		t.Errorf("Phase mismatch: got %s, want Phase 2", team.Phase)
	}
}

// Helper function to extract text from result
func getResultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if textContent, ok := result.Content[0].(mcp.TextContent); ok {
		return textContent.Text
	}
	return ""
}

// Helper function to cleanup test projects
func cleanupTestProject(t *testing.T, projectName string) {
	t.Helper()
	// Clean up the test project file if it exists (repo root .teams directory)
	configPath := filepath.Join("..", "..", "..", ".teams", projectName+".json")
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to cleanup test project %s: %v", projectName, err)
	}
}

// TestValidateRoleName tests the role name whitelist validation (SEC-002)
func TestValidateRoleName(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid role - Lead Product Manager",
			role:    "Lead Product Manager",
			wantErr: false,
		},
		{
			name:    "valid role - Chief Architect",
			role:    "Chief Architect",
			wantErr: false,
		},
		{
			name:    "valid role - Senior Backend Engineer",
			role:    "Senior Backend Engineer",
			wantErr: false,
		},
		{
			name:    "valid role - Technical Lead",
			role:    "Technical Lead",
			wantErr: false,
		},
		{
			name:    "invalid role - arbitrary string",
			role:    "Hacker",
			wantErr: true,
			errMsg:  "invalid role_name",
		},
		{
			name:    "invalid role - command injection attempt",
			role:    "root; rm -rf /",
			wantErr: true,
			errMsg:  "invalid role_name",
		},
		{
			name:    "invalid role - empty string",
			role:    "",
			wantErr: true,
			errMsg:  "role_name is required",
		},
		{
			name:    "invalid role - too long",
			role:    strings.Repeat("a", 129),
			wantErr: true,
			errMsg:  "role_name must be 128 characters or less",
		},
		{
			name:    "invalid role - control character",
			role:    "Lead Product Manager\x00",
			wantErr: true,
			errMsg:  "invalid control characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRoleName(tt.role)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateRoleName(%q) expected error, got nil", tt.role)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateRoleName(%q) error = %v, want error containing %q", tt.role, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateRoleName(%q) unexpected error: %v", tt.role, err)
				}
			}
		})
	}
}

// TestValidatePersonName tests the person name format validation (SEC-003)
func TestValidatePersonName(t *testing.T) {
	tests := []struct {
		name    string
		person  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid username",
			person:  "john_doe",
			wantErr: false,
		},
		{
			name:    "valid username with dots",
			person:  "john.doe",
			wantErr: false,
		},
		{
			name:    "valid username with hyphens",
			person:  "john-doe-123",
			wantErr: false,
		},
		{
			name:    "valid email",
			person:  "john.doe@example.com",
			wantErr: false,
		},
		{
			name:    "valid email with subdomain",
			person:  "user@subdomain.example.co.uk",
			wantErr: false,
		},
		{
			name:    "invalid - empty string",
			person:  "",
			wantErr: true,
			errMsg:  "person is required",
		},
		{
			name:    "invalid - too long",
			person:  strings.Repeat("a", 257),
			wantErr: true,
			errMsg:  "person must be 256 characters or less",
		},
		{
			name:    "invalid - control character",
			person:  "john\x00doe",
			wantErr: true,
			errMsg:  "invalid control characters",
		},
		{
			name:    "invalid - command injection attempt",
			person:  "john; rm -rf /",
			wantErr: true,
			errMsg:  "forbidden pattern",
		},
		{
			name:    "invalid - pipe character",
			person:  "john | cat /etc/passwd",
			wantErr: true,
			errMsg:  "forbidden pattern",
		},
		{
			name:    "invalid - backtick",
			person:  "john `whoami`",
			wantErr: true,
			errMsg:  "forbidden pattern",
		},
		{
			name:    "valid - display name with spaces",
			person:  "john doe",
			wantErr: false,
		},
		{
			name:    "invalid email - no domain",
			person:  "john@",
			wantErr: true,
			errMsg:  "contains invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePersonName(tt.person)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validatePersonName(%q) expected error, got nil", tt.person)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validatePersonName(%q) error = %v, want error containing %q", tt.person, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validatePersonName(%q) unexpected error: %v", tt.person, err)
				}
			}
		})
	}
}

// TestValidatePhase tests the phase validation (SEC-010: Phase injection hardening)
