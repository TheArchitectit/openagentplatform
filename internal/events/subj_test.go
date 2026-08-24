package events

import "testing"

// --- agentIDFromSubject tests ---

func TestAgentIDFromSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"oap.agents.agent-1.heartbeat", "agent-1"},
		{"oap.agents.a1.heartbeat", "a1"},
		{"oap.agents.my.dotted.id.heartbeat", "my.dotted.id"},
		// Too few parts
		{"oap.agents.heartbeat", ""},
		{"oap.agents", ""},
		{"oap", ""},
		{"", ""},
		// Wrong prefix
		{"wrong.agents.a1.heartbeat", ""},
		{"oap.agents.a1.other", "a1"}, // technically "a1" between agents and last
		// Edge: just "oap.agents.X" (3 parts)
		{"oap.agents.x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got := agentIDFromSubject(tt.subject)
			if got != tt.want {
				t.Errorf("agentIDFromSubject(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}

// --- CheckAssignmentSubject test ---

func TestCheckAssignmentSubject(t *testing.T) {
	got := CheckAssignmentSubject("agent-42")
	want := "oap.agents.agent-42.checks"
	if got != want {
		t.Errorf("CheckAssignmentSubject = %q, want %q", got, want)
	}
}
