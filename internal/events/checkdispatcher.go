package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// CheckStore is the persistence seam for the check dispatcher. The default
// implementation is *pgAgentStore in internal/api.
type CheckStore interface {
	InsertCheckResult(ctx context.Context, r *models.CheckResult) error
}

// AlertSink is the downstream notification seam. Implementations are
// expected to be non-blocking; if they block they must use a bounded
// queue. The default implementation is nil-safe: when the sink is nil,
// alerts are logged but not delivered elsewhere.
type AlertSink interface {
	Evaluate(ctx context.Context, result *models.CheckResult) error
}

// CheckDispatcher owns the subscription for check results and the publisher
// for check assignments.
type CheckDispatcher struct {
	client *Client
	store  CheckStore
	sink   AlertSink
	log    *slog.Logger

	resultSub  *nats.Subscription
	assignSub  *nats.Subscription
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewCheckDispatcher constructs a dispatcher. store and sink may be nil; the
// dispatcher tolerates that (logs warnings, does not panic).
func NewCheckDispatcher(client *Client, store CheckStore, sink AlertSink, log *slog.Logger) *CheckDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &CheckDispatcher{
		client: client,
		store:  store,
		sink:   sink,
		log:    log,
		stopCh: make(chan struct{}),
	}
}

// Start subscribes to the check-results wildcard and listens for
// assignment-change events on a queue subscription.
func (d *CheckDispatcher) Start(ctx context.Context) error {
	if d.client == nil || d.client.conn == nil {
		return errors.New("checkdispatcher: nats client not connected")
	}
	sub, err := d.client.SubscribeQueue(SubjectCheckResultsPrefix, "oap-check-evaluator", d.onResult)
	if err != nil {
		return fmt.Errorf("checkdispatcher: subscribe results: %w", err)
	}
	d.resultSub = sub
	d.log.Info("check result handler started", "subject", SubjectCheckResultsPrefix)
	return nil
}

// Stop unsubscribes and waits for goroutines.
func (d *CheckDispatcher) Stop() {
	if d.resultSub != nil {
		if err := d.resultSub.Unsubscribe(); err != nil {
			d.log.Warn("check unsubscribe failed", "err", err)
		}
	}
	if d.assignSub != nil {
		if err := d.assignSub.Unsubscribe(); err != nil {
			d.log.Warn("assign unsubscribe failed", "err", err)
		}
	}
	close(d.stopCh)
	d.wg.Wait()
}

// AssignCheck publishes a check assignment to a specific agent. The agent
// is expected to subscribe to oap.agents.<agent_id>.checks.
func (d *CheckDispatcher) AssignCheck(ctx context.Context, agentID string, assignment any) error {
	if d.client == nil {
		return errors.New("checkdispatcher: nats client not connected")
	}
	payload, err := json.Marshal(assignment)
	if err != nil {
		return fmt.Errorf("checkdispatcher: marshal assignment: %w", err)
	}
	subject := CheckAssignmentSubject(agentID)
	return d.client.Publish(ctx, subject, payload)
}

// CheckAssignmentSubject returns the per-agent subject for check assignments.
func CheckAssignmentSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.checks", agentID)
}

// onResult is the nats.MsgHandler for incoming check results. It parses,
// persists, and evaluates alerts.
func (d *CheckDispatcher) onResult(msg *nats.Msg) {
	agentID := agentIDFromSubject(msg.Subject)
	if agentID == "" {
		// Result subject is oap.agents.<id>.results
		parts := splitDots(msg.Subject)
		if len(parts) >= 4 {
			agentID = joinDots(parts[2 : len(parts)-1])
		}
	}
	if agentID == "" {
		d.log.Warn("check result on unknown subject", "subject", msg.Subject)
		return
	}

	var payload models.CheckResult
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		d.log.Warn("check result decode failed", "agent_id", agentID, "err", err)
		return
	}
	if payload.AgentID != "" {
		agentID = payload.AgentID
	}
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if d.store != nil {
		if err := d.store.InsertCheckResult(ctx, &payload); err != nil {
			d.log.Warn("check result persist failed", "agent_id", agentID, "err", err)
		}
	}

	if d.sink != nil {
		if err := d.sink.Evaluate(ctx, &payload); err != nil {
			d.log.Warn("alert evaluation failed", "agent_id", agentID, "err", err)
		}
	} else {
		// Best-effort default: log non-OK results so operators see them
		// even without an alert sink wired up.
		if payload.Status != "ok" && payload.Status != "OK" {
			d.log.Warn("check result non-ok",
				"agent_id", agentID,
				"check_id", payload.CheckID,
				"status", payload.Status,
				"message", payload.Message,
			)
		}
	}
}

// splitDots and joinDots are tiny helpers so the result handler doesn't pull
// in the strings package separately.
func splitDots(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func joinDots(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "."
		}
		out += p
	}
	return out
}
