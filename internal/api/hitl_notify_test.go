package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// stubChannelStore implements alerts.Store with only ListNotificationChannels
// (unexercised methods panic via the embedded interface).
type stubChannelStore struct {
	alerts.Store
	channels []notify.NotificationChannel
}

func (s *stubChannelStore) ListNotificationChannels(_ context.Context, orgID, _ string) ([]notify.NotificationChannel, error) {
	out := make([]notify.NotificationChannel, 0, len(s.channels))
	for _, c := range s.channels {
		if orgID == "" || c.OrgID == orgID {
			out = append(out, c)
		}
	}
	return out, nil
}

// captureNotifier records every alert delivered through the channel.
type captureNotifier struct {
	mu     sync.Mutex
	alerts []*models.Alert
}

func (n *captureNotifier) Notify(_ context.Context, alert *models.Alert, _ notify.NotificationChannel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.alerts = append(n.alerts, alert)
	return nil
}

func (n *captureNotifier) ValidateConfig(json.RawMessage) error { return nil }

func newTestApprovalNotifier(store alerts.Store) (*ApprovalNotifier, *captureNotifier, *notify.NotifierRegistry) {
	capture := &captureNotifier{}
	reg := notify.NewRegistry()
	reg.Register("test-channel", capture)
	notifier := NewApprovalNotifier(store, reg, hitl.DefaultApprovalTypes(), "https://oap.example.com", slog.New(slog.NewTextHandler(io.Discard, nil)))
	return notifier, capture, reg
}

func testApproval(id, orgID string) *hitl.ApprovalRequest {
	return &hitl.ApprovalRequest{
		ID:               id,
		ActionType:       "patch_deploy",
		RequesterAgentID: "agent-7",
		Urgency:          "critical",
		Status:           hitl.StatusPending,
		OrgID:            orgID,
		Payload:          map[string]any{"target": "ws-01"},
		ExpiresAt:        time.Now().Add(time.Hour),
	}
}

// TestApprovalNotifierDispatch verifies R2.1/R2.2: a scoped approval is
// rendered and delivered to the org's enabled channels with all four
// required content fields present.
func TestApprovalNotifierDispatch(t *testing.T) {
	store := &stubChannelStore{channels: []notify.NotificationChannel{
		{ID: "ch-1", OrgID: "org-a", Type: "test-channel", Enabled: true, Config: json.RawMessage(`{}`)},
		{ID: "ch-2", OrgID: "org-b", Type: "test-channel", Enabled: true, Config: json.RawMessage(`{}`)},
		{ID: "ch-3", OrgID: "org-a", Type: "test-channel", Enabled: false, Config: json.RawMessage(`{}`)},
	}}
	notifier, capture, _ := newTestApprovalNotifier(store)

	if err := notifier.SendApprovalRequest(context.Background(), testApproval("ap-1", "org-a")); err != nil {
		t.Fatalf("SendApprovalRequest: %v", err)
	}
	// Only the enabled org-a channel received the notification.
	capture.mu.Lock()
	got := capture.alerts
	capture.mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(got))
	}
	a := got[0]
	if a.Severity != "emergency" {
		t.Errorf("critical urgency → severity emergency, got %s", a.Severity)
	}
	if !strings.Contains(a.Message, "agent-7") || !strings.Contains(a.Message, "patch_deploy") || !strings.Contains(a.Message, "critical") {
		t.Errorf("body missing requester/action/urgency: %q", a.Message)
	}
	if url, _ := a.Metadata["approval_url"].(string); url != "https://oap.example.com/approvals/ap-1" {
		t.Errorf("approval_url = %q", url)
	}
	if kind, _ := a.Metadata["kind"].(string); kind != "approval_request" {
		t.Errorf("kind = %q", kind)
	}
}

// TestApprovalNotifierReminderPrefix verifies reminder rounds are marked.
func TestApprovalNotifierReminderPrefix(t *testing.T) {
	store := &stubChannelStore{channels: []notify.NotificationChannel{
		{ID: "ch-1", OrgID: "org-a", Type: "test-channel", Enabled: true, Config: json.RawMessage(`{}`)},
	}}
	notifier, capture, _ := newTestApprovalNotifier(store)

	req := testApproval("ap-2", "org-a")
	req.NotificationsSent = 1
	if err := notifier.SendReminder(context.Background(), req); err != nil {
		t.Fatalf("SendReminder: %v", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.alerts) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(capture.alerts))
	}
	if !strings.Contains(capture.alerts[0].Message, "Reminder") {
		t.Errorf("reminder body not marked: %q", capture.alerts[0].Message)
	}
	if kind, _ := capture.alerts[0].Metadata["kind"].(string); kind != "approval_reminder" {
		t.Errorf("kind = %q", kind)
	}
}

// TestApprovalNotifierCustomTemplate verifies R2.3: a per-type template
// overrides the default when configured.
func TestApprovalNotifierCustomTemplate(t *testing.T) {
	store := &stubChannelStore{channels: []notify.NotificationChannel{
		{ID: "ch-1", OrgID: "org-a", Type: "test-channel", Enabled: true, Config: json.RawMessage(`{}`)},
	}}
	capture := &captureNotifier{}
	reg := notify.NewRegistry()
	reg.Register("test-channel", capture)
	typeCfgs := hitl.DefaultApprovalTypes()
	for i := range typeCfgs {
		if typeCfgs[i].Type == "patch_deploy" {
			typeCfgs[i].Template = hitl.ApprovalTemplate{
				Subject: "Deploy {{.ActionType}} from {{.RequesterAgentID}}",
				Body:    "Custom body for {{.RequesterAgentID}} on {{.Summary}}",
			}
		}
	}
	notifier := NewApprovalNotifier(store, reg, typeCfgs, "https://oap.example.com", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := notifier.SendApprovalRequest(context.Background(), testApproval("ap-3", "org-a")); err != nil {
		t.Fatalf("SendApprovalRequest: %v", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.alerts) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(capture.alerts))
	}
	body := capture.alerts[0].Message
	if !strings.Contains(body, "Custom body for agent-7") || !strings.Contains(body, "target=ws-01") {
		t.Errorf("custom template not rendered: %q", body)
	}
	if subj, _ := capture.alerts[0].Metadata["subject"].(string); subj != "Deploy patch_deploy from agent-7" {
		t.Errorf("custom subject = %q", subj)
	}
}

// TestApprovalNotifierUnscoped verifies an unscoped approval delivers nothing.
func TestApprovalNotifierUnscoped(t *testing.T) {
	store := &stubChannelStore{channels: []notify.NotificationChannel{
		{ID: "ch-1", OrgID: "org-a", Type: "test-channel", Enabled: true, Config: json.RawMessage(`{}`)},
	}}
	notifier, capture, _ := newTestApprovalNotifier(store)

	if err := notifier.SendApprovalRequest(context.Background(), testApproval("ap-4", "")); err != nil {
		t.Fatalf("SendApprovalRequest: %v", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.alerts) != 0 {
		t.Errorf("unscoped approval delivered %d notifications", len(capture.alerts))
	}
}
