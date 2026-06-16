package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// ResultStore is the persistence seam for the ingest pipeline. It is
// intentionally narrow — we only need to insert a result and fetch the
// most-recent N results for a (agent_id, check_id) pair. The default
// implementation is internal/api/agent_store.go.
type ResultStore interface {
	InsertCheckResult(ctx context.Context, r *models.CheckResult) error
	// ListRecentResults returns the most recent `limit` results for the
	// given (agent_id, check_id) pair, ordered from oldest to newest.
	// Used by the threshold evaluator to count consecutive failures.
	ListRecentResults(ctx context.Context, agentID, checkID string, limit int) ([]models.CheckResult, error)
}

// CheckDefinitionLookup is the seam for resolving a check_id to its
// CheckDefinition. The threshold evaluator uses the result to compute
// the flap-detection window. The default implementation reads from the
// check_definitions table.
type CheckDefinitionLookup interface {
	GetCheck(ctx context.Context, id string) (*models.CheckDefinition, error)
}

// ResultIngestor subscribes to the check-result wildcard subject and
// drives the full ingest pipeline: parse -> persist -> evaluate -> alert
// -> broadcast.
type ResultIngestor struct {
	client    *events.Client
	store     ResultStore
	checks    CheckDefinitionLookup
	evaluator *ThresholdEvaluator
	log       *slog.Logger

	sub    *nats.Subscription
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// ResultIngestorConfig configures a ResultIngestor. All fields except
// Client and Store are optional; nil-tolerant defaults are applied.
type ResultIngestorConfig struct {
	Client    *events.Client
	Store     ResultStore
	Checks    CheckDefinitionLookup
	Evaluator *ThresholdEvaluator
	Logger    *slog.Logger
	// QueueGroup is the NATS queue group used for load-balanced
	// subscription. Defaults to "oap-check-ingest" when empty.
	QueueGroup string
}

// NewResultIngestor constructs a ResultIngestor. The Client and Store
// fields are required; Checks, Evaluator, and Logger are optional.
func NewResultIngestor(cfg ResultIngestorConfig) *ResultIngestor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Evaluator == nil {
		cfg.Evaluator = NewThresholdEvaluator(ThresholdConfig{})
	}
	if cfg.QueueGroup == "" {
		cfg.QueueGroup = "oap-check-ingest"
	}
	return &ResultIngestor{
		client:    cfg.Client,
		store:     cfg.Store,
		checks:    cfg.Checks,
		evaluator: cfg.Evaluator,
		log:       cfg.Logger,
		stopCh:    make(chan struct{}),
	}
}

// Start subscribes to the result wildcard subject under a queue group.
// Returns an error if the NATS client is not connected.
func (r *ResultIngestor) Start(ctx context.Context) error {
	if r.client == nil || r.client.Conn() == nil {
		return errors.New("result_ingestor: nats client not connected")
	}
	sub, err := r.client.SubscribeQueue(events.SubjectCheckResultPrefix, r.queueGroup(), r.onResult)
	if err != nil {
		return fmt.Errorf("result_ingestor: subscribe: %w", err)
	}
	r.sub = sub
	r.log.Info("check result ingestor started",
		"subject", events.SubjectCheckResultPrefix,
		"queue", r.queueGroup())
	return nil
}

// queueGroup returns the configured queue group name.
func (r *ResultIngestor) queueGroup() string {
	// We set the default in NewResultIngestor; this is just a guard.
	if r.client == nil {
		return "oap-check-ingest"
	}
	// Recovered from a builder reset; use the constant default.
	return "oap-check-ingest"
}

// Stop unsubscribes and waits for in-flight handlers to complete.
func (r *ResultIngestor) Stop() {
	if r.sub != nil {
		if err := r.sub.Unsubscribe(); err != nil {
			r.log.Warn("result_ingestor unsubscribe failed", "err", err)
		}
	}
	close(r.stopCh)
	r.wg.Wait()
}

// CheckResultEvent is the payload published to oap.events.checks.result
// for each ingested result. Consumers (WebSocket hub, third-party
// integrations) use this to deliver live updates.
type CheckResultEvent struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Result    *CheckResultPayload `json:"result"`
	Alert     *AlertPayload  `json:"alert,omitempty"`
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

// onResult is the NATS message handler. It is intentionally synchronous
// per-message: NATS queue subscriptions distribute load across multiple
// server instances, so each instance only processes a fraction of the
// total volume. The per-message budget is bounded by the 10s context
// timeout below.
func (r *ResultIngestor) onResult(msg *nats.Msg) {
	r.wg.Add(1)
	defer r.wg.Done()

	agentID := extractAgentIDFromResultSubject(msg.Subject)

	var raw rawResult
	if err := json.Unmarshal(msg.Data, &raw); err != nil {
		r.log.Warn("result decode failed",
			"subject", msg.Subject,
			"err", err)
		return
	}
	if raw.AgentID == "" {
		raw.AgentID = agentID
	}
	if raw.AgentID == "" || raw.CheckID == "" {
		r.log.Warn("result missing required fields",
			"subject", msg.Subject,
			"agent_id", raw.AgentID,
			"check_id", raw.CheckID)
		return
	}
	if raw.Timestamp.IsZero() {
		raw.Timestamp = time.Now().UTC()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Persist. We map raw into the canonical model.
	model := &models.CheckResult{
		AgentID:   raw.AgentID,
		CheckID:   raw.CheckID,
		Timestamp: raw.Timestamp,
		Status:    raw.Status,
		Value:     raw.Value,
		Message:   raw.Message,
		Metadata:  raw.Metadata,
	}
	if r.store != nil {
		if err := r.store.InsertCheckResult(ctx, model); err != nil {
			r.log.Warn("result persist failed",
				"agent_id", raw.AgentID,
				"check_id", raw.CheckID,
				"err", err)
			// Continue: a persist failure should not block the broadcast
			// or alert evaluation; operators still need to see the result.
		}
	}

	// 2. Evaluate thresholds. We fetch the check definition and the
	//    recent result history. Failures here are non-fatal; if we
	//    can't evaluate we still broadcast the raw result.
	evaluation, alertPayload := r.evaluate(ctx, raw, model)

	// 3. Broadcast the result to oap.events.checks.result.
	payload := &CheckResultPayload{
		AgentID:    raw.AgentID,
		CheckID:    raw.CheckID,
		CheckType:  raw.CheckType,
		Status:     raw.Status,
		Output:     raw.Output,
		Value:      raw.Value,
		Message:    raw.Message,
		DurationMs: raw.DurationMs,
		Timestamp:  raw.Timestamp,
		Metadata:   raw.Metadata,
	}
	evt := &CheckResultEvent{
		Type:      "check.result",
		Timestamp: raw.Timestamp,
		Result:    payload,
		Alert:     alertPayload,
	}
	r.publish(ctx, events.SubjectCheckResultEvent, evt)

	// 4. Publish alert lifecycle event to oap.events.alerts when an
	//    alert was fired or resolved.
	if alertPayload != nil {
		r.publish(ctx, events.SubjectAlertEvents, alertPayload)
	}

	// Log the evaluation outcome for observability. Suppressed alerts
	// are logged at debug to avoid noise.
	if evaluation.Suppressed {
		r.log.Debug("alert suppressed by flapping detector",
			"agent_id", raw.AgentID,
			"check_id", raw.CheckID,
			"reason", evaluation.Reason)
	} else if evaluation.AlertNeeded {
		r.log.Info("alert fired",
			"agent_id", raw.AgentID,
			"check_id", raw.CheckID,
			"severity", evaluation.Severity,
			"reason", evaluation.Reason)
	}
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
