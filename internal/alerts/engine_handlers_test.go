package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// TestResolveAlertRuleEmptyOrgReturnsEmpty verifies that resolveAlertRule
// refuses to select a cross-org rule when the alert carries no org scope.
// An unscoped event must not attribute itself to a rule owned by another
// tenant, so notifications never leak across org boundaries.
func TestResolveAlertRuleEmptyOrgReturnsEmpty(t *testing.T) {
	rule := models.AlertRule{
		ID:      "rule-cross-org",
		OrgID:   "other-org",
		Enabled: true,
		CheckID: "check-1",
	}
	store := &capturingAlertStore{
		fakeEngine: fakeEngine{
			rules: []models.AlertRule{rule},
		},
	}
	e := newTestEngine(store)

	// Alert with no org scope. Even though a matching rule exists in
	// another org, resolveAlertRule must return "" without querying the
	// store, so no cross-org rule is selected.
	alert := &models.Alert{
		ID:       "a-1",
		OrgID:    "",
		CheckID:  "check-1",
		AgentID:  "agent-1",
		SiteID:   "site-1",
		ClientID: "client-1",
	}

	got := e.resolveAlertRule(context.Background(), alert)
	if got != "" {
		t.Fatalf("expected empty rule id for unscoped alert, got %q", got)
	}
}

// TestResolveAlertRuleEmptyOrgDoesNotQuery verifies that when OrgID is empty
// resolveAlertRule does not touch the store at all (no GetAlertRules call),
// so an unscoped event cannot be attributed to any rule.
func TestResolveAlertRuleEmptyOrgDoesNotQuery(t *testing.T) {
	// A store that panics if GetAlertRules is ever called proves the
	// unscoped path short-circuits before the query.
	panicStore := &panicRuleStore{}
	e := newTestEngine(panicStore)

	alert := &models.Alert{
		ID:      "a-1",
		OrgID:   "",
		CheckID: "check-1",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("resolveAlertRule must not query store for unscoped alert, got panic: %v", r)
		}
	}()
	got := e.resolveAlertRule(context.Background(), alert)
	if got != "" {
		t.Fatalf("expected empty rule id, got %q", got)
	}
}

// panicRuleStore is an Engine stub whose GetAlertRules panics, proving the
// unscoped path never reaches the store.
type panicRuleStore struct{}

func (p *panicRuleStore) InsertAlert(_ context.Context, _ *models.Alert) error {
	return nil
}
func (p *panicRuleStore) GetAlert(_ context.Context, _, _ string) (*models.Alert, error) {
	return nil, nil
}
func (p *panicRuleStore) GetAlertByDedupKey(_ context.Context, _ string) (*models.Alert, error) {
	return nil, nil
}
func (p *panicRuleStore) UpdateAlertState(_ context.Context, _ *models.Alert) error {
	return nil
}
func (p *panicRuleStore) InsertStateTransition(_ context.Context, _ *models.AlertStateMachine) error {
	return nil
}
func (p *panicRuleStore) GetNotificationChannelsForRule(_ context.Context, _ string) ([]notify.NotificationChannel, error) {
	return nil, nil
}
func (p *panicRuleStore) InsertNotificationRecord(_ context.Context, _ *models.NotificationRecord) error {
	return nil
}
func (p *panicRuleStore) ResolveChannelIDs(_ context.Context, _ []string) ([]notify.NotificationChannel, error) {
	return nil, nil
}
func (p *panicRuleStore) GetAlertRules(_ context.Context, _ string) ([]models.AlertRule, error) {
	panic("GetAlertRules must not be called for an unscoped alert")
}
func (p *panicRuleStore) GetUserPreferences(_ context.Context, _, _ string) (*UserAlertPreferences, error) {
	return nil, nil
}
func (p *panicRuleStore) GetDefaultChannelIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (p *panicRuleStore) ActiveAlertSuppressionWindows(_ context.Context, _, _, _ string, _ time.Time) ([]models.AlertSuppressionWindow, error) {
	return nil, nil
}
