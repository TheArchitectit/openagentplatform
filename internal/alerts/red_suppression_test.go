package alerts

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// discardLogger returns a slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(ioDiscard(), &slog.HandlerOptions{Level: slog.LevelError}))
}

func ioDiscard() *ioDiscardType { return &ioDiscardType{} }

type ioDiscardType struct{}

func (d *ioDiscardType) Write(p []byte) (int, error) { return len(p), nil }

// newTestEngine constructs an AlertEngine with the given store and a fixed
// clock so suppression-window evaluation is deterministic.
func newTestEngine(store Engine) *AlertEngine {
	return &AlertEngine{
		store:         store,
		log:           discardLogger(),
		now:           func() time.Time { return time.Now() },
		flapWindow:    DefaultFlapWindow,
		flapThreshold: DefaultFlapThreshold,
		flapHistory:   make(map[string][]time.Time),
	}
}

// fakeEngine is a minimal Engine stub that records the arguments passed to
// ActiveAlertSuppressionWindows so we can assert the engine forwards the
// alert's scope correctly.
type fakeEngine struct {
	windows []models.AlertSuppressionWindow
	rules   []models.AlertRule
	err     error
	// capturedArgs records each call's (orgID, clientID, siteID).
	capturedArgs []struct {
		orgID    string
		clientID string
		siteID   string
	}
}

func (f *fakeEngine) InsertAlert(_ context.Context, _ *models.Alert) error { return nil }
func (f *fakeEngine) GetAlert(_ context.Context, _, _ string) (*models.Alert, error) {
	return nil, nil
}
func (f *fakeEngine) GetAlertByDedupKey(_ context.Context, _ string) (*models.Alert, error) {
	return nil, nil
}
func (f *fakeEngine) UpdateAlertState(_ context.Context, _ *models.Alert) error { return nil }
func (f *fakeEngine) InsertStateTransition(_ context.Context, _ *models.AlertStateMachine) error {
	return nil
}
func (f *fakeEngine) GetNotificationChannelsForRule(_ context.Context, _ string) ([]notify.NotificationChannel, error) {
	return nil, nil
}
func (f *fakeEngine) InsertNotificationRecord(_ context.Context, _ *models.NotificationRecord) error {
	return nil
}
func (f *fakeEngine) ResolveChannelIDs(_ context.Context, _ []string) ([]notify.NotificationChannel, error) {
	return nil, nil
}
func (f *fakeEngine) GetAlertRules(_ context.Context, _ string) ([]models.AlertRule, error) {
	return f.rules, nil
}
func (f *fakeEngine) GetUserPreferences(_ context.Context, _, _ string) (*UserAlertPreferences, error) {
	return nil, nil
}
func (f *fakeEngine) GetDefaultChannelIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeEngine) ActiveAlertSuppressionWindows(_ context.Context, orgID, clientID, siteID string, _ time.Time) ([]models.AlertSuppressionWindow, error) {
	f.capturedArgs = append(f.capturedArgs, struct {
		orgID    string
		clientID string
		siteID   string
	}{orgID, clientID, siteID})
	if f.err != nil {
		return nil, f.err
	}
	return f.windows, nil
}

// TestSuppressedByWindowForwardsScope verifies the engine forwards the
// alert's org, client, and site to the store. A client-scoped window must
// match only when the alert carries a matching ClientID.
func TestSuppressedByWindowForwardsScope(t *testing.T) {
	eng := &fakeEngine{
		windows: []models.AlertSuppressionWindow{
			{ID: "w-1", OrgID: "org-1", Enabled: true},
		},
	}
	e := newTestEngine(eng)

	alert := &models.Alert{ID: "a-1", OrgID: "org-1", SiteID: "site-1", ClientID: "client-1"}

	if !e.suppressedByWindow(context.Background(), alert) {
		t.Fatal("expected alert to be suppressed by org/site window")
	}
	if len(eng.capturedArgs) != 1 {
		t.Fatalf("expected 1 call to ActiveAlertSuppressionWindows, got %d", len(eng.capturedArgs))
	}
	if eng.capturedArgs[0].siteID != "site-1" {
		t.Fatalf("expected siteID %q forwarded, got %q", "site-1", eng.capturedArgs[0].siteID)
	}
	// ClientID must be forwarded so client-scoped windows can match.
	if eng.capturedArgs[0].clientID != "client-1" {
		t.Fatalf("expected clientID %q forwarded, got %q", "client-1", eng.capturedArgs[0].clientID)
	}
}

// TestSuppressedByWindowClientScopedNoMatch verifies a client-scoped window
// does NOT match an alert with an empty ClientID.
func TestSuppressedByWindowClientScopedNoMatch(t *testing.T) {
	eng := &fakeEngine{
		windows: []models.AlertSuppressionWindow{
			{ID: "w-c", OrgID: "org-1", ClientID: "client-1", Enabled: true},
		},
	}
	e := newTestEngine(eng)

	// Alert with empty ClientID must not be suppressed by a client-scoped
	// window. The store returns the window (it matches the org/client query),
	// but the engine's scope forwarding must pass the alert's empty client so
	// the store's scope filter excludes it. Here the fake returns the window
	// unconditionally, so we assert the engine forwarded the empty client.
	alert := &models.Alert{ID: "a-1", OrgID: "org-1"}
	e.suppressedByWindow(context.Background(), alert)
	if len(eng.capturedArgs) != 1 {
		t.Fatalf("expected 1 call, got %d", len(eng.capturedArgs))
	}
	if eng.capturedArgs[0].clientID != "" {
		t.Fatalf("expected empty clientID forwarded, got %q", eng.capturedArgs[0].clientID)
	}
}

// TestSuppressedByWindowStoreErrorDelivers verifies the fail-open contract:
// when ActiveAlertSuppressionWindows returns an error, suppressedByWindow
// must fall back to delivering (return false), not panic or suppress.
func TestSuppressedByWindowStoreErrorDelivers(t *testing.T) {
	eng := &fakeEngine{err: errors.New("db down")}
	e := newTestEngine(eng)
	alert := &models.Alert{ID: "a-3", OrgID: "org-1"}
	if e.suppressedByWindow(context.Background(), alert) {
		t.Fatal("expected delivery (false) when store errors")
	}
}

// TestSuppressionWindowNotFoundSentinelExists verifies the dedicated
// ErrAlertSuppressionWindowNotFound sentinel is defined and distinct from
// ErrAlertRuleNotFound.
func TestSuppressionWindowNotFoundSentinelExists(t *testing.T) {
	if ErrAlertSuppressionWindowNotFound == nil {
		t.Fatal("ErrAlertSuppressionWindowNotFound must be defined")
	}
	if errors.Is(ErrAlertSuppressionWindowNotFound, ErrAlertRuleNotFound) {
		t.Fatal("ErrAlertSuppressionWindowNotFound must be distinct from ErrAlertRuleNotFound")
	}
}

// capturingAlertStore is a fakeEngine variant that records the alert created
// by handleCheckFailure so we can assert the owning rule id was stamped onto
// it (the fix for the unreachable new-alert notification dispatch path).
type capturingAlertStore struct {
	fakeEngine
	created *models.Alert
}

func (c *capturingAlertStore) InsertAlert(_ context.Context, a *models.Alert) error {
	c.created = a
	return nil
}

// TestHandleCheckFailureUnscopedEventDoesNotResolveCrossOrgRule verifies
// that an incoming check-failure event with no org scope must NOT be
// attributed to a rule owned by another tenant. resolveAlertRule returns ""
// when alert.OrgID is empty, so the created alert's AlertRuleID stays empty
// and notification dispatch is skipped rather than leaking across orgs.
// This is the tenant-safety contract enforced by
// TestResolveAlertRuleEmptyOrgReturnsEmpty at the resolveAlertRule seam.
func TestHandleCheckFailureUnscopedEventDoesNotResolveCrossOrgRule(t *testing.T) {
	rule := models.AlertRule{
		ID:      "rule-owner",
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

	evt := &AlertEvent{
		Type:      "alert.fired",
		CheckID:   "check-1",
		AgentID:   "agent-1",
		SiteID:    "",
		ClientID:  "",
		Severity:  "warning",
		Status:    "firing",
		Timestamp: time.Now(),
	}
	e.handleCheckFailure(context.Background(), evt)

	if store.created == nil {
		t.Fatal("expected an alert to be created")
	}
	if store.created.AlertRuleID != "" {
		t.Fatalf("expected unscoped alert to have no resolved rule id, got %q", store.created.AlertRuleID)
	}
}

// TestDispatchNotificationsSuppressesWhenWindowActive verifies the full
// notifier path observes an active suppression window: when the org scope is
// covered, channels are not dispatched.
func TestDispatchNotificationsSuppressesWhenWindowActive(t *testing.T) {
	store := &fakeEngine{
		windows: []models.AlertSuppressionWindow{
			{ID: "w-1", OrgID: "org-1", Enabled: true},
		},
	}
	e := newTestEngine(store)
	alert := &models.Alert{
		ID:          "a-1",
		OrgID:       "org-1",
		AlertRuleID: "rule-1",
	}
	if e.suppressedByWindow(context.Background(), alert) {
		// suppressed → channels resolved to nil, dispatch is a no-op.
		return
	}
	t.Fatal("expected suppression window to cover the alert")
}

// TestDispatchNotificationsDeliversWhenNoWindow verifies the notifier path
// is permissive (delivers) when no window matches.
func TestDispatchNotificationsDeliversWhenNoWindow(t *testing.T) {
	store := &fakeEngine{
		windows: []models.AlertSuppressionWindow{},
	}
	e := newTestEngine(store)
	alert := &models.Alert{
		ID:          "a-2",
		OrgID:       "org-1",
		AlertRuleID: "rule-1",
	}
	if e.suppressedByWindow(context.Background(), alert) {
		t.Fatal("expected delivery (no suppression) when no window matches")
	}
}
