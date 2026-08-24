package models

import "time"

// Alert represents a single alert instance in the lifecycle state machine.
// It is created by the AlertEngine when a check failure is detected and
// transitions through pending -> open -> acknowledged/snoozed -> resolved -> closed.
type Alert struct {
	ID             string         `json:"id"`
	DedupKey       string         `json:"dedup_key"`
	CheckID        string         `json:"check_id"`
	AgentID        string         `json:"agent_id"`
	SiteID         string         `json:"site_id"`
	OrgID          string         `json:"org_id"`
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
	ID                   string    `json:"id"`
	OrgID                string    `json:"org_id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	CheckID              string    `json:"check_id,omitempty"`
	AgentID              string    `json:"agent_id,omitempty"`
	SiteID               string    `json:"site_id,omitempty"`
	MinSeverity          string    `json:"min_severity"` // info, warning, critical, emergency
	NotifyChannels       []string  `json:"notify_channels,omitempty"`
	Enabled              bool      `json:"enabled"`
	// OfflineSilenceSeconds, when set, is an additive SLA condition: the rule
	// fires when an agent has been silent (last_seen older than) this many
	// seconds. It is evaluated against the agent's stored last_seen, not the
	// 120s liveness threshold. Nil means the condition is absent and the rule
	// behaves exactly as before.
	OfflineSilenceSeconds *int      `json:"offline_silence_seconds,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
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
