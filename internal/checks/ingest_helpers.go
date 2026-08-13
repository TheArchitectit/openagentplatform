package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// CheckResultEvent is the payload published to oap.events.checks.result
// for each ingested result. Consumers (WebSocket hub, third-party
// integrations) use this to deliver live updates.
type CheckResultEvent struct {
	Type      string              `json:"type"`
	Timestamp time.Time           `json:"timestamp"`
	Result    *CheckResultPayload `json:"result"`
	Alert     *AlertPayload       `json:"alert,omitempty"`
}

// CheckResultPayload mirrors models.CheckResult with the additional
// fields an agent sends (CheckType, DurationMs) that the persistence
// layer doesn't currently store. We carry them through the broadcast
// so dashboards can render the full check execution context.
type CheckResultPayload struct {
	AgentID    string         `json:"agent_id"`
	CheckID    string         `json:"check_id"`
	CheckType  string         `json:"check_type,omitempty"`
	Status     string         `json:"status"`
	Output     string         `json:"output,omitempty"`
	Value      float64        `json:"value"`
	Message    string         `json:"message"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// AlertPayload is the alert lifecycle event published to
// oap.events.alerts. The WebSocket hub forwards these to dashboards so
// operators see fire/clear events in real time.
type AlertPayload struct {
	Type      string    `json:"type"` // "alert.fired" or "alert.resolved"
	AgentID   string    `json:"agent_id"`
	CheckID   string    `json:"check_id"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// rawResult is the wire format agents send on the results subject. It
// carries everything we need to persist and broadcast; the model
// struct (models.CheckResult) is a subset used by the API layer.
type rawResult struct {
	AgentID    string         `json:"agent_id"`
	CheckID    string         `json:"check_id"`
	CheckType  string         `json:"check_type,omitempty"`
	Status     string         `json:"status"`
	Output     string         `json:"output,omitempty"`
	Value      float64        `json:"value"`
	Message    string         `json:"message"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// evaluate runs the threshold evaluator and returns the decision plus
// the alert payload to publish (if any). The function tolerates store
// and lookup failures: a missing check definition or recent-results
// list is treated as "no context" and the evaluator falls back to its
// defaults.
func (r *ResultIngestor) evaluate(ctx context.Context, raw rawResult, model *models.CheckResult) (Evaluation, *AlertPayload) {
	var (
		checkDef  *models.CheckDefinition
		prev      []models.CheckResult
		alertType string = "alert.fired"
	)

	if r.checks != nil {
		cd, err := r.checks.GetCheck(ctx, raw.CheckID)
		if err != nil {
			// Tolerate: missing check definitions are common when an
			// agent reports on a check that was deleted. We still
			// evaluate with nil context.
			r.log.Debug("check definition lookup failed",
				"check_id", raw.CheckID, "err", err)
		} else {
			checkDef = cd
		}
	}

	if r.store != nil {
		// Fetch a small history for consecutive-failure counting. 20 is
		// well above the default consecutive-failure threshold (3) and
		// covers the typical lookback window.
		results, err := r.store.ListRecentResults(ctx, raw.AgentID, raw.CheckID, 20)
		if err != nil {
			r.log.Debug("recent results lookup failed",
				"agent_id", raw.AgentID,
				"check_id", raw.CheckID,
				"err", err)
		} else {
			// Exclude the result we just inserted (it would otherwise
			// double-count). The list is ordered oldest -> newest so
			// the last element, if any, is the most recent prior
			// result.
			prev = results
			if n := len(prev); n > 0 && sameResult(prev[n-1], raw) {
				prev = prev[:n-1]
			}
		}
	}

	eval := r.evaluator.Evaluate(model, checkDef, prev)
	if !eval.AlertNeeded {
		return eval, nil
	}

	payload := &AlertPayload{
		Type:      alertType,
		AgentID:   raw.AgentID,
		CheckID:   raw.CheckID,
		Severity:  eval.Severity,
		Status:    "firing",
		Message:   buildAlertMessage(raw, eval),
		Timestamp: raw.Timestamp,
	}
	return eval, payload
}

// publish marshals and publishes a payload on the given subject. Errors
// are logged but do not propagate; broadcast is best-effort.
func (r *ResultIngestor) publish(ctx context.Context, subject string, payload any) {
	if r.client == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		r.log.Warn("event marshal failed",
			"subject", subject, "err", err)
		return
	}
	if err := r.client.Publish(ctx, subject, data); err != nil {
		r.log.Warn("event publish failed",
			"subject", subject, "err", err)
	}
}

// buildAlertMessage composes a human-readable message for the alert
// payload. The agent's own message is preferred; the evaluator's
// reason is appended when available.
func buildAlertMessage(raw rawResult, eval Evaluation) string {
	msg := strings.TrimSpace(raw.Message)
	if msg == "" {
		msg = raw.Output
	}
	if msg == "" {
		msg = fmt.Sprintf("check %s is %s", raw.CheckID, raw.Status)
	}
	if eval.Reason != "" && eval.Reason != "threshold exceeded" {
		msg = msg + " (" + eval.Reason + ")"
	}
	return msg
}

// sameResult returns true when the most recent previously-stored result
// is the same one we just inserted. We compare on agent_id + check_id +
// timestamp to avoid the rare case where two results for the same
// (agent, check) share a microsecond.
func sameResult(prev models.CheckResult, raw rawResult) bool {
	return prev.AgentID == raw.AgentID &&
		prev.CheckID == raw.CheckID &&
		prev.Timestamp.Equal(raw.Timestamp)
}

// extractAgentIDFromResultSubject pulls the agent_id segment out of
// "oap.agents.<id>.results". Mirrors events.agentIDFromSubject but
// avoids a cross-package import cycle by duplicating the ~10 lines of
// string splitting.
func extractAgentIDFromResultSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 4 || parts[0] != "oap" || parts[1] != "agents" {
		return ""
	}
	// id is everything between "agents" and the trailing "results".
	return strings.Join(parts[2:len(parts)-1], ".")
}
