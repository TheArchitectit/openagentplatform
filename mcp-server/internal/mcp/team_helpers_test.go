package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestValidatePhase(t *testing.T) {
	tests := []struct {
		name    string
		phase   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid Phase 1",
			phase:   "Phase 1",
			wantErr: false,
		},
		{
			name:    "valid Phase 2",
			phase:   "Phase 2",
			wantErr: false,
		},
		{
			name:    "valid Phase 3",
			phase:   "Phase 3",
			wantErr: false,
		},
		{
			name:    "empty phase (optional)",
			phase:   "",
			wantErr: false,
		},
		{
			name:    "invalid - old full name Phase 1",
			phase:   "Phase 1: Strategy, Governance & Planning",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - old full name Phase 2",
			phase:   "Phase 2: Platform & Foundation",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - Phase 4",
			phase:   "Phase 4",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - Phase 5",
			phase:   "Phase 5",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - arbitrary string",
			phase:   "Phase 99",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - command injection attempt",
			phase:   "Phase 1; rm -rf /",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - path traversal attempt",
			phase:   "Phase 1/../../../etc/passwd",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - null byte injection",
			phase:   "Phase 1\x00",
			wantErr: true,
			errMsg:  "invalid phase",
		},
		{
			name:    "invalid - newline injection",
			phase:   "Phase 1\ncommand",
			wantErr: true,
			errMsg:  "invalid phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePhase(tt.phase)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validatePhase(%q) expected error, got nil", tt.phase)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validatePhase(%q) error = %v, want error containing %q", tt.phase, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validatePhase(%q) unexpected error: %v", tt.phase, err)
				}
			}
		})
	}
}

// TestSanitizePhase tests the phase sanitization function (SEC-010)
func TestSanitizePhase(t *testing.T) {
	tests := []struct {
		name     string
		phase    string
		expected string
	}{
		{
			name:     "valid Phase 1",
			phase:    "Phase 1",
			expected: "Phase 1",
		},
		{
			name:     "valid Phase 2",
			phase:    "Phase 2",
			expected: "Phase 2",
		},
		{
			name:     "valid Phase 3",
			phase:    "Phase 3",
			expected: "Phase 3",
		},
		{
			name:     "empty phase",
			phase:    "",
			expected: "",
		},
		{
			name:     "invalid - returns empty",
			phase:    "Phase 1; rm -rf /",
			expected: "",
		},
		{
			name:     "invalid Phase 4 returns empty",
			phase:    "Phase 4",
			expected: "",
		},
		{
			name:     "path traversal returns empty",
			phase:    "../../../etc/passwd",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizePhase(tt.phase)
			if result != tt.expected {
				t.Errorf("sanitizePhase(%q) = %q, want %q", tt.phase, result, tt.expected)
			}
		})
	}
}

// TestHandleTeamAssign_InvalidRole tests handleTeamAssign with invalid role (SEC-002)
func TestHandleTeamAssign_InvalidRole(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name      string
		roleName  string
		wantError string
	}{
		{
			name:      "invalid role name",
			roleName:  "Hacker Role",
			wantError: "invalid role_name",
		},
		{
			name:      "role with control characters",
			roleName:  "Lead Product Manager\x00",
			wantError: "invalid control characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"project_name": "test-project",
				"team_id":      float64(1),
				"role_name":    tt.roleName,
				"person":       "john_doe",
			}
			result, err := s.handleTeamAssign(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamAssign returned error: %v", err)
			}

			if !result.IsError {
				t.Error("handleTeamAssign should return error for invalid role_name")
			}

			text := getResultText(result)
			if !strings.Contains(text, tt.wantError) {
				t.Errorf("Expected error containing %q, got: %s", tt.wantError, text)
			}
		})
	}
}

// TestHandleTeamAssign_InvalidPerson tests handleTeamAssign with invalid person (SEC-003)
func TestHandleTeamAssign_InvalidPerson(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name      string
		person    string
		wantError string
	}{
		{
			name:      "person with semicolon",
			person:    "john; rm -rf /",
			wantError: "forbidden pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"project_name": "test-project",
				"team_id":      float64(1),
				"role_name":    "Lead Product Manager",
				"person":       tt.person,
			}
			result, err := s.handleTeamAssign(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamAssign returned error: %v", err)
			}

			if !result.IsError {
				t.Error("handleTeamAssign should return error for invalid person")
			}

			text := getResultText(result)
			if !strings.Contains(text, tt.wantError) {
				t.Errorf("Expected error containing %q, got: %s", tt.wantError, text)
			}
		})
	}
}

// TestHandleTeamList_InvalidPhase tests handleTeamList with invalid phase (SEC-010)
func TestHandleTeamList_InvalidPhase(t *testing.T) {
	s := mockMCPServer()
	ctx := context.Background()

	tests := []struct {
		name      string
		phase     string
		wantError string
	}{
		{
			name:      "invalid phase number",
			phase:     "Phase 99",
			wantError: "invalid phase",
		},
		{
			name:      "command injection attempt",
			phase:     "Phase 1; rm -rf /",
			wantError: "invalid phase",
		},
		{
			name:      "old format phase",
			phase:     "Phase 1: Strategy, Governance & Planning",
			wantError: "invalid phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"project_name": "test-project",
				"phase":        tt.phase,
			}

			result, err := s.handleTeamList(ctx, args)
			if err != nil {
				t.Fatalf("handleTeamList returned error: %v", err)
			}

			if !result.IsError {
				t.Errorf("handleTeamList should return error for invalid phase: %s", tt.phase)
			}

			text := getResultText(result)
			if !strings.Contains(text, tt.wantError) {
				t.Errorf("Expected error containing %q, got: %s", tt.wantError, text)
			}
		})
	}
}
