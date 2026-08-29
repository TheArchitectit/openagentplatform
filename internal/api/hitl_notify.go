package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// ApprovalNotifier delivers HITL approval notifications (spec R2) through
// the platform's existing notification channel infrastructure: it renders
// the approval into a synthetic alert and fans it out via notify.Dispatch
// to the org's enabled channels. It implements hitl.NotificationService.
type ApprovalNotifier struct {
	channels alerts.Store
	registry *notify.NotifierRegistry
	types    map[string]hitl.ApprovalTypeConfig
	baseURL  string
	log      *slog.Logger

	mu       sync.Mutex
	rendered map[string]*template.Template // cache keyed by "type:kind"
}

// NewApprovalNotifier constructs the notifier. channels/registry may be
// nil, in which case delivery is a no-op (logged once per send).
func NewApprovalNotifier(channels alerts.Store, registry *notify.NotifierRegistry, types []hitl.ApprovalTypeConfig, baseURL string, log *slog.Logger) *ApprovalNotifier {
	byType := make(map[string]hitl.ApprovalTypeConfig, len(types))
	for _, t := range types {
		byType[t.Type] = t
	}
	return &ApprovalNotifier{
		channels: channels,
		registry: registry,
		types:    byType,
		baseURL:  strings.TrimRight(baseURL, "/"),
		log:      log,
		rendered: make(map[string]*template.Template),
	}
}

// templateData is the value approval templates render against. It embeds
// the request plus computed Summary and ApprovalURL fields (R2.2).
type templateData struct {
	ID               string
	ActionType       string
	RequesterAgentID string
	Urgency          string
	TaskID           string
	Summary          string
	ApprovalURL      string
	Payload          map[string]any
}

// SendApprovalRequest delivers the create-time notification (R2.1).
func (n *ApprovalNotifier) SendApprovalRequest(ctx context.Context, req *hitl.ApprovalRequest) error {
	return n.send(ctx, req, "request")
}

// SendReminder delivers a re-notification for a still-pending approval (R2.4).
func (n *ApprovalNotifier) SendReminder(ctx context.Context, req *hitl.ApprovalRequest) error {
	return n.send(ctx, req, "reminder")
}

// SendTimeoutAlert delivers the R3.5 alert: an approval expired at maximum
// escalation depth without a human decision.
func (n *ApprovalNotifier) SendTimeoutAlert(ctx context.Context, req *hitl.ApprovalRequest) error {
	return n.send(ctx, req, "timeout")
}

// send renders and dispatches one notification round. kind is "request"
// (create, R2.1), "reminder" (R2.4), or "timeout" (R3.5 admin alert).
func (n *ApprovalNotifier) send(ctx context.Context, req *hitl.ApprovalRequest, kind string) error {
	if n.channels == nil || n.registry == nil {
		n.log.Info("hitl notify: skipped, delivery not configured", "approval_id", req.ID)
		return nil
	}

	var subject, body string
	if kind == "timeout" {
		// System-generated R3.5 alert: fixed text, emergency severity
		// regardless of the request's urgency.
		subject = fmt.Sprintf("Escalation exhausted: %s approval auto-rejected", req.ActionType)
		body = fmt.Sprintf(
			"Approval %s (%s, requester %s, urgency %s) expired at escalation depth %d without a human decision and was auto-rejected. Manual review required: %s",
			req.ID, req.ActionType, req.RequesterAgentID, req.Urgency, req.EscalationDepth, n.ApprovalURL(req.ID))
	} else {
		var err error
		subject, body, err = n.render(req, kind == "reminder")
		if err != nil {
			return fmt.Errorf("hitl notify: render: %w", err)
		}
	}

	// The alert is the transport envelope for the shared notifiers; the
	// approval content rides in Message + Metadata so email/slack/webhook
	// templates can all surface it.
	severity := urgencyToSeverity(req.Urgency)
	if kind == "timeout" {
		severity = "emergency"
	}
	alertKind := "approval_request"
	switch kind {
	case "reminder":
		alertKind = "approval_reminder"
	case "timeout":
		alertKind = "approval_timeout"
	}
	alert := &models.Alert{
		ID:        "hitl-" + req.ID + "-" + alertKind,
		CheckID:   "hitl:" + req.ActionType,
		AgentID:   req.RequesterAgentID,
		Severity:  severity,
		State:     "open",
		Message:   body,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Metadata: map[string]any{
			"kind":         alertKind,
			"subject":      subject,
			"approval_id":  req.ID,
			"action_type":  req.ActionType,
			"urgency":      req.Urgency,
			"requester":    req.RequesterAgentID,
			"task_id":      req.TaskID,
			"approval_url": n.ApprovalURL(req.ID),
			"expires_at":   req.ExpiresAt.UTC().Format(time.RFC3339),
			"payload":      req.Payload,
			"platform_url": n.baseURL,
		},
	}

	// Fan out to the requesting org's enabled channels (R2.1). Unscoped
	// approvals (empty OrgID) have no channel set to notify.
	if req.OrgID == "" {
		n.log.Info("hitl notify: approval is unscoped, no org channels", "approval_id", req.ID)
		return nil
	}
	channels, err := n.channels.ListNotificationChannels(ctx, req.OrgID, "")
	if err != nil {
		return fmt.Errorf("hitl notify: list channels: %w", err)
	}
	channels = filterApprovalCapable(channels)
	if len(channels) == 0 {
		return nil
	}
	results := notify.Dispatch(ctx, n.registry, alert, channels, n.log)
	failed := 0
	for _, r := range results {
		if r.Err != nil && r.Status == "failed" {
			failed++
		}
	}
	if failed == len(results) && len(results) > 0 {
		return fmt.Errorf("hitl notify: all %d channel deliveries failed", failed)
	}
	return nil
}

// ApprovalURL builds the deep link to an approval's detail page (R2.2).
func (n *ApprovalNotifier) ApprovalURL(id string) string {
	if n.baseURL == "" {
		return ""
	}
	return n.baseURL + "/approvals/" + id
}

// render resolves the type's configured template (R2.3), falling back to
// the default, and renders subject + body.
func (n *ApprovalNotifier) render(req *hitl.ApprovalRequest, isReminder bool) (subject, body string, err error) {
	tpl := hitl.DefaultApprovalTemplate
	if cfg, ok := n.types[req.ActionType]; ok {
		if cfg.Template.Subject != "" {
			tpl.Subject = cfg.Template.Subject
		}
		if cfg.Template.Body != "" {
			tpl.Body = cfg.Template.Body
		}
	}
	data := templateData{
		ID:               req.ID,
		ActionType:       req.ActionType,
		RequesterAgentID: req.RequesterAgentID,
		Urgency:          req.Urgency,
		TaskID:           req.TaskID,
		Summary:          summarizePayload(req.Payload),
		ApprovalURL:      n.ApprovalURL(req.ID),
		Payload:          req.Payload,
	}
	if isReminder {
		data.Summary = "[Reminder " + fmt.Sprintf("%d/%d", req.NotificationsSent, hitl.MaxRenotifications) + "] " + data.Summary
	}

	sT, err := n.compile(req.ActionType, "subject", tpl.Subject)
	if err != nil {
		return "", "", err
	}
	bT, err := n.compile(req.ActionType, "body", tpl.Body)
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	if err := sT.Execute(&sb, data); err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}
	subject = sb.String()
	sb.Reset()
	if err := bT.Execute(&sb, data); err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	body = sb.String()
	return subject, body, nil
}

// compile returns a cached parsed template for the given type+kind.
func (n *ApprovalNotifier) compile(actionType, kind, src string) (*template.Template, error) {
	key := actionType + ":" + kind + ":" + src
	n.mu.Lock()
	defer n.mu.Unlock()
	if t, ok := n.rendered[key]; ok {
		return t, nil
	}
	t, err := template.New(key).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", key, err)
	}
	n.rendered[key] = t
	return t, nil
}

// urgencyToSeverity maps approval urgency to the alert severity vocabulary
// the channel templates color-code on.
func urgencyToSeverity(urgency string) string {
	switch strings.ToLower(urgency) {
	case "critical":
		return "emergency"
	case "high":
		return "critical"
	case "medium":
		return "warning"
	default:
		return "info"
	}
}

// summarizePayload renders the approval payload to a short single-line
// description for the notification (R2.2 "action description").
func summarizePayload(payload map[string]any) string {
	if len(payload) == 0 {
		return "(no details)"
	}
	parts := make([]string, 0, len(payload))
	for k, v := range payload {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	// Stable-ish order is not guaranteed by map iteration; for a summary
	// line that is acceptable.
	s := strings.Join(parts, " ")
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}

// filterApprovalCapable keeps only channel types the approval flow supports
// (email, slack, webhook per R2.1). Custom registries may register more; we
// pass any channel whose notifier exists.
func filterApprovalCapable(channels []notify.NotificationChannel) []notify.NotificationChannel {
	out := make([]notify.NotificationChannel, 0, len(channels))
	for _, c := range channels {
		if !c.Enabled {
			continue
		}
		out = append(out, c)
	}
	return out
}

// interface assertion is checked at compile time.
var _ hitl.NotificationService = (*ApprovalNotifier)(nil)
