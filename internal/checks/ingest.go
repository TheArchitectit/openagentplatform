package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	queueGrp  string

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
		queueGrp:  cfg.QueueGroup,
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
	return r.queueGrp
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
