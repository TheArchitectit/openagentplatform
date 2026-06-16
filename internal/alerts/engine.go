// Package alerts - engine.go implements the AlertEngine that subscribes
// to NATS alert events and drives the alert state machine.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
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
	GetAlert(ctx context.Context, id string) (*models.Alert, error)
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
	// GetUserPreferences is an optional preferences lookup. Returns
	// ErrPreferencesNotFound if the user has no preferences row. The
	// engine will skip preference evaluation when the store does not
	// implement this method.
	GetUserPreferences(ctx context.Context, userID, orgID string) (*UserAlertPreferences, error)
	// GetDefaultChannelIDs returns the org-level default channel IDs
	// for routing fallback. Returns nil if the store does not implement
	// this method.
	GetDefaultChannelIDs(ctx context.Context, orgID string) ([]string, error)
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
	client        Subscriber
	store         Engine
	publisher     Publisher
	sm            *StateMachine
	log           *slog.Logger
	notifierReg   *notify.NotifierRegistry
	// router evaluates routing rules to determine which channels
	// receive a given alert. May be nil; when nil, the engine falls
	// back to the rule's own notify_channels.
	router        *Router
	// now is the clock source. Defaults to time.Now. Overridable for
	// tests.
	now           func() time.Time

	sub                  *nats.Subscription
	stopCh               chan struct{}
	wg                   sync.WaitGroup
	pendingEscalation    time.Duration
	flapWindow           time.Duration
	flapThreshold        int
	escalationTickerDone chan struct{}
	flapMu               sync.Mutex
	flapHistory          map[string][]time.Time
}

// Config configures the AlertEngine. All fields are optional except
// Client and Store.
type Config struct {
	Client             Subscriber
	Store              Engine
	Publisher          Publisher
	Logger             *slog.Logger
	StateMachine       *StateMachine
	PendingEscalation  time.Duration
	FlapWindow         time.Duration
	FlapThreshold      int
	QueueGroup         string
	// NotifierRegistry is used to look up the appropriate notifier for
	// each channel type. If nil, notifications are not dispatched
	// (alerts still transition normally).
	NotifierRegistry *notify.NotifierRegistry
	// Router, when set, is consulted before dispatching to determine
	// the final channel set. The router's output overrides the rule's
	// own notify_channels. If nil, rule-level channels are used.
	Router *Router
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
		client:      cfg.Client,
		store:       cfg.Store,
		publisher:   cfg.Publisher,
		sm:          cfg.StateMachine,
		log:         cfg.Logger,
		notifierReg: cfg.NotifierRegistry,
		router:      cfg.Router,
		now:         cfg.Now,
		stopCh:      make(chan struct{}),
		flapHistory: make(map[string][]time.Time),
		pendingEscalation: cfg.PendingEscalation,
		flapWindow:       cfg.FlapWindow,
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
	return nil
}

// Stop unsubscribes and stops the escalation ticker.
func (e *AlertEngine) Stop() {
	if e.sub != nil {
		if err := e.sub.Unsubscribe(); err != nil {
			e.log.Warn("alert engine unsubscribe failed", "err", err)
		}
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
	Type      string    `json:"type"` // "alert.fired" or "alert.resolved"
	AgentID   string    `json:"agent_id"`
	CheckID   string    `json:"check_id"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
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
func (e *AlertEngine) handleCheckFailure(ctx context.Context, evt *AlertEvent) {
	dedupKey := DedupKey(evt.CheckID, evt.AgentID, "")

	// Suppress if the check is flapping.
	if e.isFlapping(dedupKey) {
		e.log.Debug("alert suppressed by flapping detector",
			"check_id", evt.CheckID,
			"agent_id", evt.AgentID)
		return
	}

	// Look for an existing active alert.
	existing, err := e.store.GetAlertByDedupKey(ctx, dedupKey)
	if err != nil && !errors.Is(err, ErrAlertNotFound) {
		e.log.Warn("dedup lookup failed", "err", err)
	}

	now := e.sm.now()

	if existing != nil {
		// Escalate existing alert via check_failure event.
		// From acknowledged/snoozed/resolved, a new failure re-opens.
		// From open or pending, it stays (no transition needed).
		if existing.State == StateOpen || existing.State == StatePending {
			// Already firing; record flap cycle.
			e.recordFlap(dedupKey)
			return
		}
		rec, err := e.sm.Transition(ctx, TransitionInput{
			Alert:  existing,
			Event:  EventCheckFailure,
			Actor:  "system",
			Reason: "re-failure detected",
		})
		if err != nil {
			e.log.Warn("escalate transition failed",
				"alert_id", existing.ID, "err", err)
			return
		}
		if err := e.store.UpdateAlertState(ctx, existing); err != nil {
			e.log.Warn("update alert state failed", "err", err)
			return
		}
		if err := e.store.InsertStateTransition(ctx, rec); err != nil {
			e.log.Warn("insert state transition failed", "err", err)
		}
		e.recordFlap(dedupKey)
		e.log.Info("alert escalated",
			"alert_id", existing.ID,
			"from", rec.FromState, "to", rec.ToState)
		// Dispatch notifications for the re-opened alert.
		e.dispatchNotifications(ctx, existing)
		return
	}

	// Create a new pending alert.
	alert := &models.Alert{
		ID:          uuid.New().String(),
		DedupKey:    dedupKey,
		CheckID:     evt.CheckID,
		AgentID:     evt.AgentID,
		Severity:    normalizeSeverity(evt.Severity),
		State:       StatePending,
		Message:     evt.Message,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := e.store.InsertAlert(ctx, alert); err != nil {
		e.log.Warn("insert alert failed", "err", err)
		return
	}
	rec := &models.AlertStateMachine{
		AlertID:   alert.ID,
		FromState: "",
		ToState:   StatePending,
		Event:     EventCheckFailure,
		Actor:     "system",
		CreatedAt: now,
	}
	if err := e.store.InsertStateTransition(ctx, rec); err != nil {
		e.log.Warn("insert initial state transition failed", "err", err)
	}
	e.recordFlap(dedupKey)
	e.log.Info("alert created",
		"alert_id", alert.ID,
		"check_id", evt.CheckID,
		"agent_id", evt.AgentID,
		"severity", alert.Severity)

	// Dispatch notifications for the new alert. Newly-fired alerts
	// are considered "open" for notification purposes even though the
	// state machine starts them in "pending"; users want to be paged
	// as soon as a check is failing.
	e.dispatchNotifications(ctx, alert)
}

// handleCheckRecovery auto-resolves any active alert for the given
// (check, agent) pair.
func (e *AlertEngine) handleCheckRecovery(ctx context.Context, evt *AlertEvent) {
	dedupKey := DedupKey(evt.CheckID, evt.AgentID, "")
	existing, err := e.store.GetAlertByDedupKey(ctx, dedupKey)
	if err != nil {
		if errors.Is(err, ErrAlertNotFound) {
			return
		}
		e.log.Warn("recovery dedup lookup failed", "err", err)
		return
	}

	// No-op if already resolved or closed.
	if existing.State == StateResolved || existing.State == StateClosed {
		return
	}

	rec, err := e.sm.Transition(ctx, TransitionInput{
		Alert:  existing,
		Event:  EventCheckRecovery,
		Actor:  "system",
		Reason: "check recovered",
	})
	if err != nil {
		e.log.Warn("recovery transition failed", "err", err)
		return
	}
	if err := e.store.UpdateAlertState(ctx, existing); err != nil {
		e.log.Warn("update alert on recovery failed", "err", err)
		return
	}
	if err := e.store.InsertStateTransition(ctx, rec); err != nil {
		e.log.Warn("insert recovery state transition failed", "err", err)
	}
	e.log.Info("alert resolved",
		"alert_id", existing.ID,
		"from", rec.FromState, "to", rec.ToState)
}

// Acknowledge transitions an alert from pending/open to acknowledged.
func (e *AlertEngine) Acknowledge(ctx context.Context, alertID, actor string) error {
	return e.transitionByID(ctx, alertID, EventAcknowledge, actor, "", 0)
}

// Snooze transitions an alert to snoozed with the given duration.
func (e *AlertEngine) Snooze(ctx context.Context, alertID, actor string, duration time.Duration) error {
	return e.transitionByID(ctx, alertID, EventSnooze, actor, "", duration)
}

// Resolve transitions an alert to resolved.
func (e *AlertEngine) Resolve(ctx context.Context, alertID, actor string) error {
	return e.transitionByID(ctx, alertID, EventCheckRecovery, actor, "", 0)
}

// Close transitions an alert to closed.
func (e *AlertEngine) Close(ctx context.Context, alertID, actor string) error {
	return e.transitionByID(ctx, alertID, EventClose, actor, "", 0)
}

// transitionByID is the shared internal helper for user-driven transitions.
func (e *AlertEngine) transitionByID(ctx context.Context, alertID, event, actor, reason string, duration time.Duration) error {
	alert, err := e.store.GetAlert(ctx, alertID)
	if err != nil {
		return err
	}
	rec, err := e.sm.Transition(ctx, TransitionInput{
		Alert:    alert,
		Event:    event,
		Actor:    actor,
		Reason:   reason,
		Duration: duration,
	})
	if err != nil {
		return err
	}
	if err := e.store.UpdateAlertState(ctx, alert); err != nil {
		return err
	}
	return e.store.InsertStateTransition(ctx, rec)
}

// runEscalationLoop periodically scans for pending alerts that have
// exceeded the escalation timeout and auto-escalates them to open.
func (e *AlertEngine) runEscalationLoop() {
	defer close(e.escalationTickerDone)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.escalateStalePending()
		}
	}
}

// escalateStalePending finds pending alerts older than the escalation
// timeout and transitions them to open.
func (e *AlertEngine) escalateStalePending() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cutoff := e.sm.now().Add(-e.pendingEscalation)
	f := AlertFilter{
		State:  StatePending,
		From:   time.Time{}, // no lower bound
		To:     cutoff,
		Limit:  200,
	}
	// We want alerts where created_at <= cutoff. Use To as the upper bound.
	f.To = cutoff

	alerts, _, err := e.listAlerts(ctx, f)
	if err != nil {
		e.log.Warn("escalation list failed", "err", err)
		return
	}
	for _, a := range alerts {
		rec, err := e.sm.Transition(ctx, TransitionInput{
			Alert:  &a,
			Event:  EventEscalate,
			Actor:  "escalator",
			Reason: fmt.Sprintf("pending > %s", e.pendingEscalation),
		})
		if err != nil {
			e.log.Warn("escalate transition failed", "alert_id", a.ID, "err", err)
			continue
		}
		if err := e.store.UpdateAlertState(ctx, &a); err != nil {
			e.log.Warn("escalate update failed", "alert_id", a.ID, "err", err)
			continue
		}
		if err := e.store.InsertStateTransition(ctx, rec); err != nil {
			e.log.Warn("escalate history insert failed", "alert_id", a.ID, "err", err)
		}
		e.log.Info("alert auto-escalated",
			"alert_id", a.ID, "from", rec.FromState, "to", rec.ToState)
		// Dispatch notifications for the newly-open alert.
		e.dispatchNotifications(ctx, &a)
	}
}

// dispatchNotifications looks up the channels configured for the
// alert's rule and fans out the alert to each one concurrently. Called
// when an alert state changes to "pending", "open", or re-opens from
// acknowledged/snoozed/resolved. A nil notifier registry or a rule with
// no channels is a no-op. Channel lookup failures are logged and
// silently dropped -- notification delivery must never block alert
// processing.
//
// Integration points:
//   - If a Router is configured, it is consulted to compute the final
//     channel set from routing rules. The router's output replaces the
//     rule-level notify_channels.
//   - If the store implements GetUserPreferences, each channel's owner
//     has their preferences evaluated. Channels whose owner's
//     preferences suppress the alert are removed from the dispatch set.
func (e *AlertEngine) dispatchNotifications(ctx context.Context, alert *models.Alert) {
	if e.notifierReg == nil {
		return
	}
	if alert == nil {
		return
	}

	// Find the alert rule that owns this alert. We use AlertRuleID when
	// present; otherwise we fall back to looking up rules by check_id.
	var ruleID string
	if alert.AlertRuleID != "" {
		ruleID = alert.AlertRuleID
	} else {
		// Without a rule ID we cannot reliably determine channel
		// configuration. Log and skip.
		e.log.Debug("no alert_rule_id on alert; skipping notification dispatch",
			"alert_id", alert.ID)
		return
	}

	channels, err := e.resolveChannels(ctx, alert, ruleID)
	if err != nil {
		e.log.Warn("notification channel lookup failed",
			"alert_id", alert.ID, "rule_id", ruleID, "err", err)
		return
	}
	if len(channels) == 0 {
		return
	}

	results := notify.Dispatch(ctx, e.notifierReg, alert, channels, e.log)
	for _, r := range results {
		status := "sent"
		errMsg := ""
		if r.Err != nil {
			status = "failed"
			errMsg = r.Err.Error()
		}
		rec := &models.NotificationRecord{
			ID:        uuid.New().String(),
			AlertID:   alert.ID,
			Channel:   r.ChannelType,
			Recipient: r.ChannelID,
			Status:    status,
			ErrorMsg:  errMsg,
			CreatedAt: e.sm.now(),
		}
		if status == "sent" {
			now := e.sm.now()
			rec.SentAt = &now
		}
		if r.Err == nil {
			e.log.Info("notification channel delivered",
				"alert_id", alert.ID,
				"channel_id", r.ChannelID,
				"channel_type", r.ChannelType)
		} else {
			e.log.Warn("notification channel failed",
				"alert_id", alert.ID,
				"channel_id", r.ChannelID,
				"channel_type", r.ChannelType,
				"err", r.Err)
		}
		if err := e.store.InsertNotificationRecord(ctx, rec); err != nil {
			e.log.Warn("insert notification record failed",
				"alert_id", alert.ID, "err", err)
		}
	}
}

// resolveChannels returns the final set of channels that should
// receive the alert. If a router is configured, it is consulted
// first; otherwise the rule-level channels are used. User preferences
// (quiet hours, severity threshold, mute, channel toggles) are then
// applied to filter out suppressed channels.
func (e *AlertEngine) resolveChannels(ctx context.Context, alert *models.Alert, ruleID string) ([]notify.NotificationChannel, error) {
	var channels []notify.NotificationChannel

	if e.router != nil {
		rc := RoutingContext{
			OrgID:    alert.OrgID,
			AgentID:  alert.AgentID,
			SiteID:   alert.SiteID,
			CheckID:  alert.CheckID,
			Severity: alert.Severity,
		}
		result, err := e.router.Route(ctx, alert.OrgID, rc)
		if err != nil {
			e.log.Warn("routing evaluation failed; falling back to rule channels",
				"alert_id", alert.ID, "err", err)
		} else if len(result.ChannelIDs) > 0 || result.UsedDefault {
			channels, err = e.store.ResolveChannelIDs(ctx, result.ChannelIDs)
			if err != nil {
				return nil, fmt.Errorf("resolve channel ids: %w", err)
			}
		}
	}

	// Fall back to the rule's own notify_channels when routing
	// produced no set.
	if len(channels) == 0 {
		var err error
		channels, err = e.store.GetNotificationChannelsForRule(ctx, ruleID)
		if err != nil {
			return nil, err
		}
	}

	// Apply user preferences. Channels whose owner has suppressed the
	// alert (quiet hours, severity, mute, channel toggle) are removed.
	return e.applyPreferences(ctx, alert, channels), nil
}

// applyPreferences filters channels by evaluating the owning user's
// preferences. Org-wide channels (UserID == "") are always passed
// through. Channels that survive the filter are returned in their
// original order.
func (e *AlertEngine) applyPreferences(ctx context.Context, alert *models.Alert, channels []notify.NotificationChannel) []notify.NotificationChannel {
	if len(channels) == 0 {
		return channels
	}
	now := e.engineNow()

	out := make([]notify.NotificationChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.UserID == "" {
			out = append(out, ch)
			continue
		}
		prefs, err := e.store.GetUserPreferences(ctx, ch.UserID, ch.OrgID)
		if err != nil {
			// If we cannot load preferences, be permissive and deliver.
			out = append(out, ch)
			continue
		}
		// Check channel-type toggle first; cheapest filter.
		if !prefs.IsChannelEnabled(ch.Type) {
			e.log.Debug("notification suppressed by channel toggle",
				"alert_id", alert.ID,
				"channel_id", ch.ID,
				"user_id", ch.UserID)
			continue
		}
		res := Evaluate(prefs, alert.Severity, now)
		if !res.ShouldNotify {
			e.log.Debug("notification suppressed by preferences",
				"alert_id", alert.ID,
				"channel_id", ch.ID,
				"user_id", ch.UserID,
				"reason", res.Reason,
				"suppressed_by", res.SuppressedBy)
			continue
		}
		out = append(out, ch)
	}
	return out
}

// engineNow returns the engine's clock, falling back to the state
// machine clock for backwards compatibility.
func (e *AlertEngine) engineNow() time.Time {
	if e.now != nil {
		return e.now()
	}
	return e.sm.now()
}

// listAlerts is a thin adapter that uses the store interface to list
// pending alerts. It exists so the engine does not depend on the concrete
// pgAlertStore type.
func (e *AlertEngine) listAlerts(ctx context.Context, f AlertFilter) ([]models.Alert, int, error) {
	// We use the concrete store if available; otherwise we cast.
	type lister interface {
		ListAlerts(ctx context.Context, f AlertFilter) ([]models.Alert, int, error)
	}
	if l, ok := e.store.(lister); ok {
		return l.ListAlerts(ctx, f)
	}
	return nil, 0, errors.New("alert_engine: store does not support ListAlerts")
}

// --- Flap detection -------------------------------------------------------

// recordFlap logs a flap event for the given dedup key.
func (e *AlertEngine) recordFlap(dedupKey string) {
	e.flapMu.Lock()
	defer e.flapMu.Unlock()
	now := e.sm.now()
	e.flapHistory[dedupKey] = append(e.flapHistory[dedupKey], now)
}

// isFlapping returns true if the dedup key has more than flapThreshold
// events within the flap window.
func (e *AlertEngine) isFlapping(dedupKey string) bool {
	e.flapMu.Lock()
	defer e.flapMu.Unlock()
	now := e.sm.now()
	cutoff := now.Add(-e.flapWindow)
	events := e.flapHistory[dedupKey]

	// Prune old entries.
	pruned := events[:0]
	for _, t := range events {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	e.flapHistory[dedupKey] = pruned
	return len(pruned) >= e.flapThreshold
}

// normalizeSeverity maps a raw severity string to one of the four
// canonical levels. Unknown values default to "warning".
func normalizeSeverity(s string) string {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityCritical, SeverityEmergency:
		return s
	case "warn", "crit":
		// Map legacy/short names to the full taxonomy.
		if s == "warn" {
			return SeverityWarning
		}
		return SeverityCritical
	default:
		return SeverityWarning
	}
}
