package alerts

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakeRuleStore is a RuleQuerier stub.
type fakeRuleStore struct {
	rules []models.AlertRule
	err   error
}

func (f *fakeRuleStore) GetAlertRules(_ context.Context, _ string) ([]models.AlertRule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rules, nil
}

// fakeAgentQuerier is an AgentQuerier stub returning a fixed agent list.
type fakeAgentQuerier struct {
	agents []models.Agent
	err    error
}

func (f *fakeAgentQuerier) ListSilentAgents(_ context.Context, _, _ string, _ time.Time) ([]models.Agent, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.agents, nil
}

// capturingPub is a Publisher stub recording every payload it received.
type capturingPub struct {
	mu       sync.Mutex
	payloads [][]byte
	subjects []string
}

func (c *capturingPub) Publish(_ context.Context, subject string, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.payloads = append(c.payloads, payload)
	c.subjects = append(c.subjects, subject)
	return nil
}

// ptr is a small helper to take the address of an int literal.
func ptr(i int) *int { return &i }

func TestSilenceEvaluatorFiresWhenSilent(t *testing.T) {
	now := time.Now()
	orgID := "org-1"
	rule := models.AlertRule{
		ID:                    "rule-1",
		OrgID:                 orgID,
		Enabled:               true,
		MinSeverity:           "warning",
		OfflineSilenceSeconds: ptr(3600), // 1h
	}
	// Agent last_seen is 2h ago -> silent past the 1h threshold.
	silentAgent := models.Agent{ID: "agent-silent", OrgID: orgID, Hostname: "old-host", LastSeen: now.Add(-2 * time.Hour)}

	pub := &capturingPub{}
	eval := NewSilenceEvaluator(
		&fakeRuleStore{rules: []models.AlertRule{rule}},
		&fakeAgentQuerier{agents: []models.Agent{silentAgent}},
		pub,
		nil,
	)
	eval.now = func() time.Time { return now }
	eval.sweep(context.Background())

	if len(pub.payloads) != 1 {
		t.Fatalf("expected 1 published alert, got %d", len(pub.payloads))
	}
	if len(pub.subjects) != 1 || pub.subjects[0] != events.SubjectAlertEvents {
		t.Fatalf("expected publish on %s, got %v", events.SubjectAlertEvents, pub.subjects)
	}
}

func TestSilenceEvaluatorQuietWhenRecent(t *testing.T) {
	now := time.Now()
	rule := models.AlertRule{
		ID:                    "rule-2",
		OrgID:                 "org-1",
		Enabled:               true,
		OfflineSilenceSeconds: ptr(3600), // 1h
	}
	// The AgentQuerier returns no agents (none meet the stale-before cutoff
	// because the evaluator queries with staleBefore = now - 1h, which already
	// excludes any agent last seen within the last hour).
	pub := &capturingPub{}
	eval := NewSilenceEvaluator(
		&fakeRuleStore{rules: []models.AlertRule{rule}},
		&fakeAgentQuerier{agents: []models.Agent{}},
		pub,
		nil,
	)
	eval.now = func() time.Time { return now }
	eval.sweep(context.Background())

	if len(pub.payloads) != 0 {
		t.Fatalf("expected 0 published alerts for a recent agent, got %d", len(pub.payloads))
	}
}

func TestSilenceEvaluatorSkipsRulesWithoutCondition(t *testing.T) {
	now := time.Now()
	// Rule without OfflineSilenceSeconds must never fire.
	rule := models.AlertRule{ID: "rule-3", OrgID: "org-1", Enabled: true}
	stale := models.Agent{ID: "agent-stale", LastSeen: now.Add(-48 * time.Hour)}

	pub := &capturingPub{}
	eval := NewSilenceEvaluator(
		&fakeRuleStore{rules: []models.AlertRule{rule}},
		&fakeAgentQuerier{agents: []models.Agent{stale}},
		pub,
		nil,
	)
	eval.now = func() time.Time { return now }
	eval.sweep(context.Background())

	if len(pub.payloads) != 0 {
		t.Fatalf("expected no alerts for a rule without the silence condition, got %d", len(pub.payloads))
	}
}

func TestSilenceEvaluatorDisabledRuleNoFire(t *testing.T) {
	now := time.Now()
	rule := models.AlertRule{
		ID:                    "rule-4",
		OrgID:                 "org-1",
		Enabled:               false, // disabled
		OfflineSilenceSeconds: ptr(3600),
	}
	stale := models.Agent{ID: "agent-stale", LastSeen: now.Add(-48 * time.Hour)}

	pub := &capturingPub{}
	eval := NewSilenceEvaluator(
		&fakeRuleStore{rules: []models.AlertRule{rule}},
		&fakeAgentQuerier{agents: []models.Agent{stale}},
		pub,
		nil,
	)
	eval.now = func() time.Time { return now }
	eval.sweep(context.Background())

	if len(pub.payloads) != 0 {
		t.Fatalf("expected no alerts for a disabled rule, got %d", len(pub.payloads))
	}
}
