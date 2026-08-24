package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"log/slog"
	"sync"
	"time"
)

// Severity levels supported by the alert engine.
const (
	SeverityInfo      = "info"
	SeverityWarning   = "warning"
	SeverityCritical  = "critical"
	SeverityEmergency = "emergency"
)

// DefaultPendingEscalationTimeout is the maximum time an alert can remain
// in the "pending" state before the engine auto-escalates it to "open".
const DefaultPendingEscalationTimeout = 5 * time.Minute

// DefaultFlapWindow is the time window over which flapping (open/resolve
// cycles) is counted for suppression.
const DefaultFlapWindow = 10 * time.Minute

// DefaultFlapThreshold is the number of open/resolve cycles within the
// flap window that triggers suppression.
const DefaultFlapThreshold = 5

// Engine is the persistence seam used by the AlertEngine. It is
// intentionally narrow -- the engine only needs to create, read, and
// update alerts and rules.

type Engine interface {
	InsertAlert(ctx context.Context, a *models.Alert) error
	GetAlert(ctx context.Context, orgID, id string) (*models.Alert, error)
	GetAlertByDedupKey(ctx context.Context, dedupKey string) (*models.Alert, error)
	UpdateAlertState(ctx context.Context, a *models.Alert) error
	InsertStateTransition(ctx context.Context, t *models.AlertStateMachine) error
	// GetNotificationChannelsForRule returns the channels associated with
	// an alert rule via rule.NotifyChannels. The engine calls this when
	// an alert state changes to "open" or "critical" to dispatch
	// notifications. May return an empty slice if no channels are
	// configured.
	GetNotificationChannelsForRule(ctx context.Context, ruleID string) ([]notify.NotificationChannel, error)
	// InsertNotificationRecord persists a notification delivery record.
	// Called after each channel dispatch attempt for auditing.
	InsertNotificationRecord(ctx context.Context, n *models.NotificationRecord) error
	// ResolveChannelIDs looks up a set of channel records by their IDs.
	// Used by the routing engine to materialize channel sets.
	ResolveChannelIDs(ctx context.Context, ids []string) ([]notify.NotificationChannel, error)
	// GetAlertRules returns all alert rules, optionally filtered by org_id.
	// Used by the engine to resolve the rule that owns an incoming
	// check-failure event when the event does not carry an explicit
	// alert_rule_id, so notifications can be dispatched to the rule's
	// configured channels.
	GetAlertRules(ctx context.Context, orgID string) ([]models.AlertRule, error)
	// GetUserPreferences is an optional preferences lookup. Returns
	// ErrPreferencesNotFound if the user has no preferences row. The
	// engine will skip preference evaluation when the store does not
	// implement this method.
	GetUserPreferences(ctx context.Context, userID, orgID string) (*UserAlertPreferences, error)
	// GetDefaultChannelIDs returns the org-level default channel IDs
	// for routing fallback. Returns nil if the store does not implement
	// this method.
	GetDefaultChannelIDs(ctx context.Context, orgID string) ([]string, error)
	// ActiveAlertSuppressionWindows returns enabled fleet-level
	// alert-suppression windows covering the given org/client/site at now.
	// Used by the notifier to suppress notifications during planned work.
	// Returns nil if the store does not implement this method.
	ActiveAlertSuppressionWindows(ctx context.Context, orgID, clientID, siteID string, now time.Time) ([]models.AlertSuppressionWindow, error)
}

// Publisher is the subset of the NATS client interface used by the engine.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// Subscriber is the subset of the NATS client interface used by the engine.
type Subscriber interface {
	SubscribeQueue(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// AlertEngine subscribes to oap.events.alerts and drives the state machine
// for every alert lifecycle event.
type AlertEngine struct {
	client      Subscriber
	store       Engine
	publisher   Publisher
	sm          *StateMachine
	log         *slog.Logger
	notifierReg *notify.NotifierRegistry
	// router evaluates routing rules to determine which channels
	// receive a given alert. May be nil; when nil, the engine falls
	// back to the rule's own notify_channels.
	router *Router
	// now is the clock source. Defaults to time.Now. Overridable for
	// tests.
	now func() time.Time

	sub                  *nats.Subscription
	stopCh               chan struct{}
	wg                   sync.WaitGroup
	pendingEscalation    time.Duration
	flapWindow           time.Duration
	flapThreshold        int
	escalationTickerDone chan struct{}
	flapMu               sync.Mutex
	flapHistory          map[string][]time.Time
	cfg                  Config
}

// Config configures the AlertEngine. All fields are optional except
// Client and Store.
type Config struct {
	Client            Subscriber
	Store             Engine
	Publisher         Publisher
	Logger            *slog.Logger
	StateMachine      *StateMachine
	PendingEscalation time.Duration
	FlapWindow        time.Duration
	FlapThreshold     int
	QueueGroup        string
	// NotifierRegistry is used to look up the appropriate notifier for
	// each channel type. If nil, notifications are not dispatched
	// (alerts still transition normally).
	NotifierRegistry *notify.NotifierRegistry
	// Router, when set, is consulted before dispatching to determine
	// the final channel set. The router's output overrides the rule's
	// own notify_channels. If nil, rule-level channels are used.
	Router *Router
	// SilenceEvaluator, when set, is started/stopped alongside the engine
	// and periodically fires offline-sla alerts for rules carrying the
	// offline_silence_seconds condition. If nil, offline-sla evaluation is
	// disabled.
	SilenceEvaluator *SilenceEvaluator
	// Now overrides the clock source. Defaults to time.Now.
	Now func() time.Time
}

// New constructs an AlertEngine. Client and Store are required.
func New(cfg Config) *AlertEngine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.StateMachine == nil {
		cfg.StateMachine = NewStateMachine()
	}
	if cfg.PendingEscalation <= 0 {
		cfg.PendingEscalation = DefaultPendingEscalationTimeout
	}
	if cfg.FlapWindow <= 0 {
		cfg.FlapWindow = DefaultFlapWindow
	}
	if cfg.FlapThreshold <= 0 {
		cfg.FlapThreshold = DefaultFlapThreshold
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &AlertEngine{
		client:            cfg.Client,
		store:             cfg.Store,
		publisher:         cfg.Publisher,
		sm:                cfg.StateMachine,
		log:               cfg.Logger,
		notifierReg:       cfg.NotifierRegistry,
		router:            cfg.Router,
		now:               cfg.Now,
		stopCh:            make(chan struct{}),
		flapHistory:       make(map[string][]time.Time),
		cfg:               cfg,
		pendingEscalation: cfg.PendingEscalation,
		flapWindow:        cfg.FlapWindow,
		flapThreshold:     cfg.FlapThreshold,
	}
}

// Start subscribes to the alert events subject and starts the escalation
// ticker. Returns an error if subscription fails.
func (e *AlertEngine) Start(ctx context.Context) error {
	if e.client == nil {
		return errors.New("alert_engine: nil subscriber")
	}
	queue := "oap-alert-engine"
	sub, err := e.client.SubscribeQueue(events.SubjectAlertEvents, queue, e.onAlertEvent)
	if err != nil {
		return fmt.Errorf("alert_engine: subscribe: %w", err)
	}
	e.sub = sub
	e.log.Info("alert engine started",
		"subject", events.SubjectAlertEvents,
		"queue", queue)

	// Start the escalation ticker.
	e.escalationTickerDone = make(chan struct{})
	go e.runEscalationLoop()

	if e.cfg.SilenceEvaluator != nil {
		e.cfg.SilenceEvaluator.Start(ctx)
	}
	return nil
}

// Stop unsubscribes, stops the escalation ticker, and stops the silence
// evaluator if one was configured.
func (e *AlertEngine) Stop() {
	if e.sub != nil {
		if err := e.sub.Unsubscribe(); err != nil {
			e.log.Warn("alert engine unsubscribe failed", "err", err)
		}
	}
	if cfg := e.cfg.SilenceEvaluator; cfg != nil {
		cfg.Stop()
	}
	close(e.stopCh)
	if e.escalationTickerDone != nil {
		<-e.escalationTickerDone
	}
	e.wg.Wait()
}

// AlertEvent is the JSON payload published on oap.events.alerts by the
// check ingest pipeline. The engine reads these to create and escalate
// alerts.
type AlertEvent struct {
	Type          string `json:"type"` // "alert.fired" or "alert.resolved"
	AgentID       string `json:"agent_id"`
	AgentHostname string `json:"agent_hostname,omitempty"`
	SiteID        string `json:"site_id,omitempty"`
	// ClientID is the tenant-scoped client that owns the agent. Empty
	// when the agent is not associated with a client.
	ClientID  string    `json:"client_id,omitempty"`
	CheckID   string    `json:"check_id"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	// AlertType classifies the source of the event. "check_failure"
	// is the default for check-based alerts; "policy_violation" is
	// set by the PolicyEngine's ViolationManager and is treated
	// specially in the state machine so that policy findings are
	// never confused with monitoring checks.
	AlertType string `json:"alert_type,omitempty"`
}

// DedupKey builds the canonical dedup key for a (check, agent, rule) triple.
// Used to prevent duplicate alerts for the same failure.
func DedupKey(checkID, agentID, alertRuleID string) string {
	return checkID + "\x00" + agentID + "\x00" + alertRuleID
}

// onAlertEvent is the NATS message handler. It parses the payload and
// dispatches to the appropriate state-machine event.
func (e *AlertEngine) onAlertEvent(msg *nats.Msg) {
	e.wg.Add(1)
	defer e.wg.Done()

	var evt AlertEvent
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		e.log.Warn("alert event decode failed", "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch evt.Type {
	case "alert.fired":
		e.handleCheckFailure(ctx, &evt)
	case "alert.resolved":
		e.handleCheckRecovery(ctx, &evt)
	default:
		e.log.Warn("unknown alert event type", "type", evt.Type)
	}
}

// handleCheckFailure creates a new pending alert or escalates an existing
// one. Dedup prevents creating multiple alerts for the same (check,
// agent, rule) triple.
