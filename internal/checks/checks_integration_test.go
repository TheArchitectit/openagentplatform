package checks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// --- Mock store + integration test ---

type mockResultStore struct {
	mu      sync.Mutex
	results []models.CheckResult
	inserts int
}

func (m *mockResultStore) InsertCheckResult(_ context.Context, r *models.CheckResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inserts++
	m.results = append(m.results, *r)
	return nil
}

func (m *mockResultStore) ListRecentResults(_ context.Context, agentID, checkID string, limit int) ([]models.CheckResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.CheckResult
	for _, r := range m.results {
		if r.AgentID == agentID && r.CheckID == checkID {
			out = append(out, r)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

type mockCheckLookup struct {
	defs map[string]*models.CheckDefinition
}

func (m *mockCheckLookup) GetCheck(_ context.Context, id string) (*models.CheckDefinition, error) {
	d, ok := m.defs[id]
	if !ok {
		return nil, errors.New("check not found")
	}
	return d, nil
}

func TestIntegration_EvaluateFlow(t *testing.T) {
	store := &mockResultStore{}
	checks := &mockCheckLookup{
		defs: map[string]*models.CheckDefinition{
			"c1": {ID: "c1", IntervalSeconds: 60},
		},
	}

	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))
	ing := NewResultIngestor(ResultIngestorConfig{
		Store:     store,
		Checks:    checks,
		Evaluator: ev,
	})

	// Pre-populate the store with 2 prior failures (evaluate fetches
	// recent results from the store but does not insert them).
	store.results = []models.CheckResult{
		{AgentID: "agent-1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * time.Minute)},
		{AgentID: "agent-1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * time.Minute)},
	}

	raw := rawResult{
		AgentID:   "agent-1",
		CheckID:   "c1",
		Status:    StatusCrit,
		Message:   "disk full",
		Timestamp: now,
	}
	model := &models.CheckResult{
		AgentID:   raw.AgentID,
		CheckID:   raw.CheckID,
		Status:    raw.Status,
		Message:   raw.Message,
		Timestamp: raw.Timestamp,
	}

	// 3rd consecutive failure (2 in store + current) should fire.
	eval, alert := ing.evaluate(context.Background(), raw, model)
	if !eval.AlertNeeded {
		t.Error("3rd consecutive failure should fire")
	}
	if alert == nil {
		t.Fatal("alert payload should be non-nil")
	}
	if alert.Severity != SeverityCrit {
		t.Errorf("alert severity = %q, want %q", alert.Severity, SeverityCrit)
	}
	if alert.Message == "" {
		t.Error("alert message should not be empty")
	}
}

func TestBuildAlertMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  rawResult
		eval Evaluation
		want string
	}{
		{
			name: "uses message field",
			raw:  rawResult{CheckID: "c1", Status: "crit", Message: "disk full"},
			eval: Evaluation{Reason: "threshold exceeded"},
			want: "disk full",
		},
		{
			name: "falls back to output",
			raw:  rawResult{CheckID: "c1", Status: "crit", Output: "timeout"},
			eval: Evaluation{Reason: "threshold exceeded"},
			want: "timeout",
		},
		{
			name: "falls back to generated message",
			raw:  rawResult{CheckID: "c1", Status: "crit"},
			eval: Evaluation{Reason: "threshold exceeded"},
			want: "check c1 is crit",
		},
		{
			name: "appends non-default reason",
			raw:  rawResult{CheckID: "c1", Status: "crit", Message: "oops"},
			eval: Evaluation{Reason: "something custom"},
			want: "oops (something custom)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAlertMessage(tt.raw, tt.eval)
			if got != tt.want {
				t.Errorf("buildAlertMessage = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSameResult(t *testing.T) {
	ts := time.Now()
	prev := models.CheckResult{AgentID: "a1", CheckID: "c1", Timestamp: ts}
	raw := rawResult{AgentID: "a1", CheckID: "c1", Timestamp: ts}
	if !sameResult(prev, raw) {
		t.Error("should match")
	}

	raw2 := rawResult{AgentID: "a1", CheckID: "c1", Timestamp: ts.Add(time.Second)}
	if sameResult(prev, raw2) {
		t.Error("different timestamp should not match")
	}

	raw3 := rawResult{AgentID: "a2", CheckID: "c1", Timestamp: ts}
	if sameResult(prev, raw3) {
		t.Error("different agent should not match")
	}
}

func TestExtractAgentIDFromResultSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"oap.agents.agent-1.results", "agent-1"},
		{"oap.agents.my.dotted.id.results", "my.dotted.id"},
		{"oap.agents.a1.results", "a1"},
		{"oap.agents.results", ""},
		{"oap.other.agent.results", ""},
		{"random.subject", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got := extractAgentIDFromResultSubject(tt.subject)
			if got != tt.want {
				t.Errorf("extractAgentIDFromResultSubject(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}
