package models

import "time"

// Alert represents a single alert instance in the lifecycle state machine.
// It is created by the AlertEngine when a check failure is detected and
// transitions through pending -> open -> acknowledged/snoozed -> resolved -> closed.
type Alert struct {
	ID       string `json:"id"`
	DedupKey string `json:"dedup_key"`
	CheckID  string `json:"check_id"`
	AgentID  string `json:"agent_id"`
	SiteID   string `json:"site_id"`
	OrgID    string `json:"org_id"`
	// ClientID is the tenant-scoped client that owns the alert. It is
	// nullable: agents that report without a client (e.g. legacy probes)
	// leave it empty, and client-scoped suppression windows never match
	// such alerts.
	ClientID       string         `json:"client_id,omitempty"`
	AlertRuleID    string         `json:"alert_rule_id"`
	Severity       string         `json:"severity"` // info, warning, critical, emergency
	State          string         `json:"state"`    // pending, open, acknowledged, snoozed, resolved, closed
	Message        string         `json:"message"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	AcknowledgedBy string         `json:"acknowledged_by,omitempty"`
	SnoozedUntil   *time.Time     `json:"snoozed_until,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	ResolvedAt     *time.Time     `json:"resolved_at,omitempty"`
	ClosedAt       *time.Time     `json:"closed_at,omitempty"`
}

// AlertRule defines a rule that determines when alerts are generated and
// how they are routed. Rules can scope alerts to specific checks, agents,
// sites, and severity thresholds.
type AlertRule struct {
	ID             string   `json:"id"`
	OrgID          string   `json:"org_id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	CheckID        string   `json:"check_id,omitempty"`
	AgentID        string   `json:"agent_id,omitempty"`
	SiteID         string   `json:"site_id,omitempty"`
	MinSeverity    string   `json:"min_severity"` // info, warning, critical, emergency
	NotifyChannels []string `json:"notify_channels,omitempty"`
	Enabled        bool     `json:"enabled"`
	// OfflineSilenceSeconds, when set, is an additive SLA condition: the rule
	// fires when an agent has been silent (last_seen older than) this many
	// seconds. It is evaluated against the agent's stored last_seen, not the
	// 120s liveness threshold. Nil means the condition is absent and the rule
	// behaves exactly as before.
	OfflineSilenceSeconds *int      `json:"offline_silence_seconds,omitempty"`
	// HypervisorClusterID scopes EVE alerts to a specific cluster.
	HypervisorClusterID string `json:"hypervisor_cluster_id,omitempty"`
	// HypervisorEventTypes filters which EVE events trigger this rule.
	HypervisorEventTypes []string `json:"hypervisor_event_types,omitempty"`
	// StoragePoolAlertPct is the utilization threshold for storage warnings.
	StoragePoolAlertPct *int `json:"storage_pool_alert_pct,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AlertSuppressionWindow is a fleet-level window during which alert
// *notifications* are suppressed. It is distinct from per-user quiet hours
// (preferences.go) and from patch-deploy windows (internal/patches). Scope is
// hierarchical: an org-scoped window suppresses for the whole org; a client-
// or site-scoped window suppresses only for that client/site. The time
// semantics mirror internal/patches.MaintenanceWindow (start/end, optional
// recurrence by weekday) but this is a separate type and does not import that
// package.
type AlertSuppressionWindow struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	// Scope: at most one of ClientID / SiteID should be set. An empty
	// ClientID+SiteID means the window is org-wide.
	ClientID  string    `json:"client_id,omitempty"`
	SiteID    string    `json:"site_id,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Recurring bool      `json:"recurring"`
	// Weekdays, when non-empty, restricts a recurring window to those days.
	Weekdays []time.Weekday `json:"weekdays,omitempty"`
	// Timezone is the IANA location name (e.g. "America/New_York") in which
	// a recurring window's weekday and time-of-day are evaluated. Non-recurring
	// windows compare instants directly and ignore it. Defaults to "UTC".
	Timezone  string    `json:"timezone,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsActiveAt reports whether the window covers the given instant. For
// non-recurring windows this is a simple start <= now < end check. For
// recurring windows the weekday must also match; the time-of-day is derived
// from the Start/End timestamps on the given day.
func (w *AlertSuppressionWindow) IsActiveAt(now time.Time) bool {
	if w == nil || !w.Enabled {
		return false
	}
	if w.Recurring {
		// Evaluate weekday and time-of-day in the window's timezone.
		loc := time.UTC
		if w.Timezone != "" {
			if l, err := time.LoadLocation(w.Timezone); err == nil {
				loc = l
			}
		}
		localNow := now.In(loc)
		dayOK := len(w.Weekdays) == 0
		for _, d := range w.Weekdays {
			if d == localNow.Weekday() {
				dayOK = true
				break
			}
		}
		if !dayOK {
			return false
		}
		// Compare time-of-day against the recurring window's start/end.
		startMin := w.Start.Hour()*60 + w.Start.Minute()
		endMin := w.End.Hour()*60 + w.End.Minute()
		nowMin := localNow.Hour()*60 + localNow.Minute()
		if startMin <= endMin {
			if nowMin < startMin || nowMin >= endMin {
				return false
			}
		} else {
			// Overnight window.
			if nowMin < startMin && nowMin >= endMin {
				return false
			}
		}
		return true
	}
	return !now.Before(w.Start) && now.Before(w.End)
}

// AlertStateMachine records a single state transition in an alert's
// lifecycle. It is written to the alert_state_history table for audit.
type AlertStateMachine struct {
	ID        string    `json:"id"`
	AlertID   string    `json:"alert_id"`
	FromState string    `json:"from_state"`
	ToState   string    `json:"to_state"`
	Event     string    `json:"event"`
	Actor     string    `json:"actor"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// NotificationRecord tracks a notification sent for an alert, including
// the channel used and delivery status.
type NotificationRecord struct {
	ID        string     `json:"id"`
	AlertID   string     `json:"alert_id"`
	Channel   string     `json:"channel"` // email, slack, webhook, etc.
	Recipient string     `json:"recipient"`
	Status    string     `json:"status"` // pending, sent, failed
	ErrorMsg  string     `json:"error_msg,omitempty"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
