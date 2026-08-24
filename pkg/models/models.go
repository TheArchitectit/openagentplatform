package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	OrgID     string    `json:"org_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Site struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	CreatedAt time.Time `json:"created_at"`
}

type Agent struct {
	ID              string         `json:"id"`
	AgentID         string         `json:"agent_id"`
	SiteID          string         `json:"site_id"`
	ClientID        string         `json:"client_id,omitempty"`
	OrgID           string         `json:"org_id"`
	Hostname        string         `json:"hostname"`
	OperatingSystem string         `json:"os" db:"operating_system"`
	Arch            string         `json:"arch" db:"goarch"`
	Platform        string         `json:"platform"`
	CPUCount        int            `json:"cpu_count"`
	TotalMemoryMB   int64          `json:"total_memory_mb" db:"total_ram"`
	TotalDiskGB     int64          `json:"total_disk_gb"`
	Disks           map[string]any `json:"disks,omitempty"`
	Services        map[string]any `json:"services,omitempty"`
	WMIDetail       map[string]any `json:"wmi_detail,omitempty"`
	PublicIP        string         `json:"public_ip,omitempty"`
	BootTime        *time.Time     `json:"boot_time,omitempty"`
	LoggedInUser    string         `json:"logged_in_username,omitempty"`
	NeedsReboot     bool           `json:"needs_reboot"`
	Inventory       map[string]any `json:"inventory,omitempty"`
	MeshToken       string         `json:"mesh_token,omitempty"`
	Tags            []string       `json:"tags"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	AgentVersion    string         `json:"agent_version"`
	Version         string         `json:"version"`
	Status          string         `json:"status"`
	LastSeen        time.Time      `json:"last_seen"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       *time.Time     `json:"deleted_at,omitempty"`
}

// Heartbeat is the payload published by agents on oap.agents.<id>.heartbeat.
type Heartbeat struct {
	AgentID     string    `json:"agent_id"`
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemPercent  float64   `json:"mem_percent"`
	DiskPercent float64   `json:"disk_percent"`
	UptimeSecs  uint64    `json:"uptime_secs"`
	Version     string    `json:"version"`
}

// UnmarshalJSON decodes a Heartbeat, accepting Timestamp as either an int64
// unix-seconds value (what pkg/agent publishes: time.Now().Unix()) or an
// RFC3339 string. Without this, json.Unmarshal rejects the numeric form and
// every agent heartbeat fails to decode server-side. Magnitude auto-detection
// also accepts milli/micro/nanosecond integers so future agent versions don't
// silently land in 1970.
func (h *Heartbeat) UnmarshalJSON(data []byte) error {
	// Shadow struct: same fields but Timestamp as RawMessage so time.Time's
	// strict UnmarshalJSON never runs on a numeric value, plus no recursion
	// into this method.
	type shadow struct {
		AgentID     string          `json:"agent_id"`
		Timestamp   json.RawMessage `json:"timestamp"`
		CPUPercent  float64         `json:"cpu_percent"`
		MemPercent  float64         `json:"mem_percent"`
		DiskPercent float64         `json:"disk_percent"`
		UptimeSecs  uint64          `json:"uptime_secs"`
		Version     string          `json:"version"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	ts := time.Time{}
	if len(s.Timestamp) > 0 && string(s.Timestamp) != "null" {
		var n int64
		if err := json.Unmarshal(s.Timestamp, &n); err == nil {
			ts = unixSecondsToTime(n)
		} else {
			var str string
			if err2 := json.Unmarshal(s.Timestamp, &str); err2 == nil {
				if parsed, err3 := time.Parse(time.RFC3339, str); err3 == nil {
					ts = parsed
				}
			}
			// Neither number nor parseable string: keep zero time; the
			// heartbeat handler already substitutes time.Now() for zero.
		}
	}

	*h = Heartbeat{
		AgentID:     s.AgentID,
		Timestamp:   ts,
		CPUPercent:  s.CPUPercent,
		MemPercent:  s.MemPercent,
		DiskPercent: s.DiskPercent,
		UptimeSecs:  s.UptimeSecs,
		Version:     s.Version,
	}
	return nil
}

// unixSecondsToTime interprets an integer timestamp by magnitude:
// seconds (~1.7e9), milliseconds (~1.7e12), microseconds (~1.7e15), or
// nanoseconds (~1.7e18). Values far from any plausible epoch resolve to
// their literal second interpretation rather than erroring.
func unixSecondsToTime(n int64) time.Time {
	switch {
	case n == 0:
		return time.Time{}
	case n > 1e17: // nanoseconds
		return time.Unix(0, n)
	case n > 1e14: // microseconds
		return time.UnixMicro(n)
	case n > 1e11: // milliseconds
		return time.UnixMilli(n)
	default: // seconds
		return time.Unix(n, 0)
	}
}

// String renders a Heartbeat compactly for logs.
func (h Heartbeat) String() string {
	return fmt.Sprintf("Heartbeat{agent=%s ts=%s cpu=%.1f mem=%.1f disk=%.1f v=%s}",
		h.AgentID, h.Timestamp.Format(time.RFC3339), h.CPUPercent, h.MemPercent, h.DiskPercent, h.Version)
}

// CheckResult is the payload published by agents on oap.agents.<id>.results.
type CheckResult struct {
	AgentID   string         `json:"agent_id"`
	CheckID   string         `json:"check_id"`
	Timestamp time.Time      `json:"timestamp"`
	Status    string         `json:"status"`
	Value     float64        `json:"value"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// CheckDefinition is a reusable, named check definition (e.g. "ping Google DNS").
// Config holds type-specific parameters (host, url, threshold, etc.) as JSONB
// and is validated at API time against the check_type's schema.
type CheckDefinition struct {
	ID              string         `json:"id"`
	OrgID           string         `json:"org_id"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	CheckType       string         `json:"check_type"`
	Config          map[string]any `json:"config"`
	IntervalSeconds int            `json:"interval_seconds"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
	Enabled         bool           `json:"enabled"`
	FailThreshold   float64        `json:"fail_threshold,omitempty"`
	WarnThreshold   float64        `json:"warn_threshold,omitempty"`
	ErrorThreshold  float64        `json:"error_threshold,omitempty"`
	AlertSeverity   string         `json:"alert_severity,omitempty"`
	IsTemplate      bool           `json:"is_template"`
	LastStatus      string         `json:"last_status,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// CheckAssignment links a CheckDefinition to an Agent (or a site_id for
// fan-out to all agents in a site). site_id is used only for assignments
// created from /assign when the request supplies a site instead of an agent.
type CheckAssignment struct {
	ID         string    `json:"id"`
	CheckID    string    `json:"check_id"`
	AgentID    string    `json:"agent_id"`
	SiteID     string    `json:"site_id"`
	AssignedBy string    `json:"assigned_by"`
	CreatedAt  time.Time `json:"created_at"`
	// Joined fields (populated by ListAssignments).
	AgentHostname string       `json:"agent_hostname,omitempty"`
	LastResult    *CheckResult `json:"last_result,omitempty"`
}

// CheckAssignmentDetail pairs an assignment with the agent's most recent
// check result for that check_id, used by the GET /assignments endpoint.
type CheckAssignmentDetail struct {
	AssignmentID string       `json:"assignment_id"`
	AgentID      string       `json:"agent_id"`
	Hostname     string       `json:"hostname,omitempty"`
	SiteID       string       `json:"site_id,omitempty"`
	AssignedAt   time.Time    `json:"assigned_at"`
	LastResult   *CheckResult `json:"last_result,omitempty"`
}

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
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	CheckID        string    `json:"check_id,omitempty"`
	AgentID        string    `json:"agent_id,omitempty"`
	SiteID         string    `json:"site_id,omitempty"`
	MinSeverity    string    `json:"min_severity"` // info, warning, critical, emergency
	NotifyChannels []string  `json:"notify_channels,omitempty"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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

// Policy is a Rego-based compliance policy. The Rego body is compiled
// at create/update time and cached. Policies are evaluated against
// agent state to produce violations.
type Policy struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"org_id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	RegoBody        string    `json:"rego_body"`
	EnforcementMode string    `json:"enforcement_mode"` // enforce, monitor, disabled
	Severity        string    `json:"severity"`         // info, warning, critical
	Category        string    `json:"category"`         // security, compliance, operational
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PolicyAssignment links a policy to a target (agent or site). An
// assignment is a many-to-many row; site-scoped policies evaluate
// against all agents in that site.
type PolicyAssignment struct {
	ID        string    `json:"id"`
	PolicyID  string    `json:"policy_id"`
	AgentID   string    `json:"agent_id,omitempty"`
	SiteID    string    `json:"site_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// PolicyViolation records a single failed evaluation of a policy
// against a specific agent. Persisted for compliance reporting and
// audit.
type PolicyViolation struct {
	ID         string         `json:"id"`
	PolicyID   string         `json:"policy_id"`
	AgentID    string         `json:"agent_id"`
	Severity   string         `json:"severity"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
	Resolved   bool           `json:"resolved"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}
