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

// CheckStore is retained only for API compatibility with
// NewCheckDispatcher's signature; the dispatcher no longer persists
// results (the ingestor owns persistence — see W2).
type CheckStore interface {
	InsertCheckResult(ctx context.Context, r *models.CheckResult) error
}

// AlertSink is retained only for API compatibility with
// NewCheckDispatcher's signature; alert evaluation happens in the
// ingestor pipeline.
type AlertSink interface {
	Evaluate(ctx context.Context, result *models.CheckResult) error
}

// CheckDispatcher owns the publisher for check assignments. Result
// ingestion (persist, threshold evaluation, alerting, broadcast) is owned
// by internal/checks.ResultIngestor — see remediation plan W2: the
// dispatcher previously ALSO subscribed to oap.agents.*.results under a
// different queue group and inserted every result a second time.
type CheckDispatcher struct {
	client *Client
	log    *slog.Logger

	assignSub  *nats.Subscription
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewCheckDispatcher constructs a dispatcher. The client may be nil; the
// dispatcher tolerates that (logs warnings, does not panic). The store and
// sink parameters are retained for API compatibility and ignored.
func NewCheckDispatcher(client *Client, _ CheckStore, _ AlertSink, log *slog.Logger) *CheckDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &CheckDispatcher{
		client: client,
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
	d.log.Info("check assignment dispatcher started (results handled by result ingestor)")
	return nil
}

// Stop unsubscribes and waits for goroutines.
func (d *CheckDispatcher) Stop() {
	if d.assignSub != nil {
		if err := d.assignSub.Unsubscribe(); err != nil {
			d.log.Warn("assign unsubscribe failed", "err", err)
		}
	}
	close(d.stopCh)
	d.wg.Wait()
}

// CheckAssignment is the canonical payload published to agents. It carries
// everything the agent needs to start running the check: the check_id, the
// type, the validated config, the run interval, and the timeout. Agents
// subscribed to oap.agents.<agent_id>.checks receive this struct and
// either start a new goroutine for the check or update an existing one.
type CheckAssignment struct {
	Type           string         `json:"type"`
	CheckID        string         `json:"check_id"`
	Name           string         `json:"name,omitempty"`
	Config         map[string]any `json:"config"`
	IntervalSecs   int            `json:"interval_seconds"`
	TimeoutSecs    int            `json:"timeout_seconds"`
	Timestamp      time.Time      `json:"timestamp"`
	OrgID          string         `json:"org_id,omitempty"`
}

// AssignCheck publishes a check assignment to a specific agent. The agent
// is expected to subscribe to oap.agents.<agent_id>.checks. The supplied
// assignment value is JSON-encoded as-is; callers wanting the canonical
// payload shape should use AssignCheckWithDefinition.
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

// AssignCheckWithDefinition builds the canonical CheckAssignment payload
// from a check definition and publishes it to the given agent. This is
// the preferred entry point for API handlers creating assignments.
func (d *CheckDispatcher) AssignCheckWithDefinition(ctx context.Context, agentID, orgID string, def *models.CheckDefinition) error {
	if def == nil {
		return errors.New("checkdispatcher: nil check definition")
	}
	if def.Config == nil {
		def.Config = map[string]any{}
	}
	assign := CheckAssignment{
		Type:         "RunCheck",
		CheckID:      def.ID,
		Name:         def.Name,
		Config:       def.Config,
		IntervalSecs: def.IntervalSeconds,
		TimeoutSecs:  def.TimeoutSeconds,
		Timestamp:    time.Now().UTC(),
		OrgID:        orgID,
	}
	return d.AssignCheck(ctx, agentID, assign)
}

// CheckAssignmentSubject returns the per-agent subject for check assignments.
func CheckAssignmentSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.checks", agentID)
}
