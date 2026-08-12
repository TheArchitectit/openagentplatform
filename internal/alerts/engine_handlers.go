package alerts

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
	alertType := evt.AlertType
	if alertType == "" {
		alertType = "check_failure"
	}
	meta := map[string]any{
		"alert_type":     alertType,
		"agent_hostname": evt.AgentHostname,
		"site_id":        evt.SiteID,
	}
	alert := &models.Alert{
		ID:          uuid.New().String(),
		DedupKey:    dedupKey,
		CheckID:     evt.CheckID,
		AgentID:     evt.AgentID,
		SiteID:      evt.SiteID,
		Severity:    normalizeSeverity(evt.Severity),
		State:       StatePending,
		Message:     evt.Message,
		Metadata:    meta,
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
		Reason:    alertType,
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
		"severity", alert.Severity,
		"alert_type", alertType)

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
func (e *AlertEngine) Acknowledge(ctx context.Context, orgID, alertID, actor string) error {
	return e.transitionByID(ctx, orgID, alertID, EventAcknowledge, actor, "", 0)
}

// Snooze transitions an alert to snoozed with the given duration.
func (e *AlertEngine) Snooze(ctx context.Context, orgID, alertID, actor string, duration time.Duration) error {
	return e.transitionByID(ctx, orgID, alertID, EventSnooze, actor, "", duration)
}

// Resolve transitions an alert to resolved.
func (e *AlertEngine) Resolve(ctx context.Context, orgID, alertID, actor string) error {
	return e.transitionByID(ctx, orgID, alertID, EventCheckRecovery, actor, "", 0)
}

// Close transitions an alert to closed.
func (e *AlertEngine) Close(ctx context.Context, orgID, alertID, actor string) error {
	return e.transitionByID(ctx, orgID, alertID, EventClose, actor, "", 0)
}

// transitionByID is the shared internal helper for user-driven transitions.
func (e *AlertEngine) transitionByID(ctx context.Context, orgID, alertID, event, actor, reason string, duration time.Duration) error {
	alert, err := e.store.GetAlert(ctx, orgID, alertID)
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
