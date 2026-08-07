package alerts

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
