package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHealthCheckerAggregatesWorstStatus(t *testing.T) {
	checker := NewHealthChecker()
	fixed := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	checker.now = func() time.Time { return fixed }
	checks := map[string]ComponentHealth{
		"agent-1": {Kind: "agent", Status: HealthHealthy},
		"backend": {Kind: "backend", Status: HealthDegraded},
		"service": {Kind: "service", Status: HealthHealthy},
	}
	for name, result := range checks {
		result := result
		if err := checker.Register(name, HealthCheckFunc(func(context.Context) ComponentHealth { return result })); err != nil {
			t.Fatal(err)
		}
	}
	report := checker.Check(context.Background())
	if report.Status != HealthDegraded {
		t.Fatalf("status = %q, want %q", report.Status, HealthDegraded)
	}
	if report.Counts[HealthHealthy] != 2 || report.Counts[HealthDegraded] != 1 {
		t.Fatalf("unexpected counts: %#v", report.Counts)
	}
	if len(report.Components) != 3 || report.Components[0].Name != "agent-1" {
		t.Fatalf("unexpected components: %#v", report.Components)
	}
	if !report.CheckedAt.Equal(fixed) {
		t.Fatalf("checked_at = %v, want %v", report.CheckedAt, fixed)
	}
}

func TestHealthCheckerEmptyIsUnknown(t *testing.T) {
	report := NewHealthChecker().Check(context.Background())
	if report.Status != HealthUnknown {
		t.Fatalf("status = %q, want %q", report.Status, HealthUnknown)
	}
}

func TestAlertManagerLifecycle(t *testing.T) {
	manager := NewAlertManager()
	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	if err := manager.Add(Alert{ID: "a-1", Source: "agent-1", Severity: "critical", Message: "offline"}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Minute)
	acknowledged, err := manager.Acknowledge("a-1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.State != AlertAcknowledged || acknowledged.AcknowledgedBy != "operator" {
		t.Fatalf("unexpected acknowledged alert: %#v", acknowledged)
	}

	now = now.Add(time.Minute)
	snoozed, err := manager.Snooze("a-1", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if snoozed.State != AlertSnoozed || snoozed.SnoozedUntil == nil || !snoozed.SnoozedUntil.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("unexpected snoozed alert: %#v", snoozed)
	}

	now = now.Add(time.Minute)
	resolved, err := manager.Resolve("a-1")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != AlertResolved || resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(now) {
		t.Fatalf("unexpected resolved alert: %#v", resolved)
	}
	if _, err := manager.Acknowledge("a-1", "operator"); !errors.Is(err, ErrInvalidAlertTransition) {
		t.Fatalf("acknowledge resolved error = %v", err)
	}

	alerts := manager.List(AlertFilter{State: AlertResolved, Severity: "critical"})
	if len(alerts) != 1 || alerts[0].ID != "a-1" {
		t.Fatalf("unexpected filtered alerts: %#v", alerts)
	}
}

func TestAlertManagerRejectsInvalidOperations(t *testing.T) {
	manager := NewAlertManager()
	if _, err := manager.Resolve("missing"); !errors.Is(err, ErrAlertNotFound) {
		t.Fatalf("resolve missing error = %v", err)
	}
	if err := manager.Add(Alert{ID: "a-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snooze("a-1", 0); err == nil {
		t.Fatal("expected invalid snooze duration")
	}
}

func TestComplianceScorecardComputesAgentAndOverallScores(t *testing.T) {
	scorecard := NewScorecard()
	fixed := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	scorecard.now = func() time.Time { return fixed }
	card := scorecard.Compute([]ComplianceResult{
		{AgentID: "agent-b", PolicyID: "p-1", Compliant: true},
		{AgentID: "agent-a", PolicyID: "p-1", Compliant: true},
		{AgentID: "agent-a", PolicyID: "p-2", Compliant: false},
		{AgentID: "agent-b", PolicyID: "p-2", Compliant: true},
	})
	if card.Passed != 3 || card.Total != 4 || card.OverallScore != 75 {
		t.Fatalf("unexpected overall scorecard: %#v", card)
	}
	if len(card.Agents) != 2 || card.Agents[0].AgentID != "agent-a" || card.Agents[0].Score != 50 || card.Agents[1].Score != 100 {
		t.Fatalf("unexpected agent scores: %#v", card.Agents)
	}
	if !card.ComputedAt.Equal(fixed) {
		t.Fatalf("computed_at = %v, want %v", card.ComputedAt, fixed)
	}
}

func TestComplianceScorecardEmpty(t *testing.T) {
	card := NewScorecard().Compute(nil)
	if card.Total != 0 || card.OverallScore != 0 || card.Agents == nil {
		t.Fatalf("unexpected empty scorecard: %#v", card)
	}
}
