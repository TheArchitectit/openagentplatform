package models

import (
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
	ID            string          `json:"id"`
	AgentID       string          `json:"agent_id"`
	SiteID        string          `json:"site_id"`
	OrgID         string          `json:"org_id"`
	Hostname      string          `json:"hostname"`
	OperatingSystem string        `json:"os" db:"operating_system"`
	Arch          string          `json:"arch"`
	Platform      string          `json:"platform"`
	CPUCount      int             `json:"cpu_count"`
	TotalMemoryMB int64           `json:"total_memory_mb"`
	TotalDiskGB   int64           `json:"total_disk_gb"`
	Tags          []string        `json:"tags"`
	Metadata      map[string]any  `json:"metadata,omitempty"`
	AgentVersion  string          `json:"agent_version"`
	Version       string          `json:"version"`
	Status        string          `json:"status"`
	LastSeen      time.Time       `json:"last_seen"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Heartbeat is the payload published by agents on oap.agents.<id>.heartbeat.
type Heartbeat struct {
	AgentID    string    `json:"agent_id"`
	Timestamp  time.Time `json:"timestamp"`
	CPUPercent float64   `json:"cpu_percent"`
	MemPercent float64   `json:"mem_percent"`
	DiskPercent float64  `json:"disk_percent"`
	UptimeSecs uint64    `json:"uptime_secs"`
	Version    string    `json:"version"`
}

// CheckResult is the payload published by agents on oap.agents.<id>.results.
type CheckResult struct {
	AgentID    string         `json:"agent_id"`
	CheckID    string         `json:"check_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Status     string         `json:"status"`
	Value      float64        `json:"value"`
	Message    string         `json:"message"`
	Metadata   map[string]any `json:"metadata,omitempty"`
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
	ID            string    `json:"id"`
	CheckID       string    `json:"check_id"`
	AgentID       string    `json:"agent_id"`
	SiteID        string    `json:"site_id"`
	AssignedBy    string    `json:"assigned_by"`
	CreatedAt     time.Time `json:"created_at"`
	// Joined fields (populated by ListAssignments).
	AgentHostname string     `json:"agent_hostname,omitempty"`
	LastResult    *CheckResult `json:"last_result,omitempty"`
}

// CheckAssignmentDetail pairs an assignment with the agent's most recent
// check result for that check_id, used by the GET /assignments endpoint.
type CheckAssignmentDetail struct {
	AssignmentID string      `json:"assignment_id"`
	AgentID      string      `json:"agent_id"`
	Hostname     string      `json:"hostname,omitempty"`
	SiteID       string      `json:"site_id,omitempty"`
	AssignedAt   time.Time   `json:"assigned_at"`
	LastResult   *CheckResult `json:"last_result,omitempty"`
}

// Alert represents a single alert instance in the lifecycle state machine.
// It is created by the AlertEngine when a check failure is detected and
// transitions through pending -> open -> acknowledged/snoozed -> resolved -> closed.
type Alert struct {
	ID            string         `json:"id"`
	DedupKey      string         `json:"dedup_key"`
	CheckID       string         `json:"check_id"`
	AgentID       string         `json:"agent_id"`
	SiteID        string         `json:"site_id"`
	OrgID         string         `json:"org_id"`
	AlertRuleID   string         `json:"alert_rule_id"`
	Severity      string         `json:"severity"`      // info, warning, critical, emergency
	State         string         `json:"state"`         // pending, open, acknowledged, snoozed, resolved, closed
	Message       string         `json:"message"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	AcknowledgedBy string        `json:"acknowledged_by,omitempty"`
	SnoozedUntil  *time.Time     `json:"snoozed_until,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	ResolvedAt    *time.Time     `json:"resolved_at,omitempty"`
	ClosedAt      *time.Time     `json:"closed_at,omitempty"`
}

// AlertRule defines a rule that determines when alerts are generated and
// how they are routed. Rules can scope alerts to specific checks, agents,
// sites, and severity thresholds.
type AlertRule struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"org_id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	CheckID       string         `json:"check_id,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	SiteID        string         `json:"site_id,omitempty"`
	MinSeverity   string         `json:"min_severity"`  // info, warning, critical, emergency
	NotifyChannels []string      `json:"notify_channels,omitempty"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
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
	ID         string    `json:"id"`
	AlertID    string    `json:"alert_id"`
	Channel    string    `json:"channel"` // email, slack, webhook, etc.
	Recipient  string    `json:"recipient"`
	Status     string    `json:"status"`  // pending, sent, failed
	ErrorMsg   string    `json:"error_msg,omitempty"`
	SentAt     *time.Time `json:"sent_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
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
	ID         string    `json:"id"`
	PolicyID   string    `json:"policy_id"`
	AgentID    string    `json:"agent_id"`
	Severity   string    `json:"severity"`
	Message    string    `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
	Resolved   bool      `json:"resolved"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// PatchSeverity classifies the risk level of a patch and drives the
// approval rules in the workflow engine:
//
//   - "critical"   : auto-approved on creation, notification dispatched.
//   - "standard"   : requires a single approver.
//   - "major_os"   : requires two distinct approvers (four-eyes principle).
type PatchSeverity string

const (
	PatchSeverityCritical PatchSeverity = "critical"
	PatchSeverityStandard PatchSeverity = "standard"
	PatchSeverityMajorOS  PatchSeverity = "major_os"
)

// PatchJob represents a single patch deployment targeting one or more
// endpoints. The state field is driven by the ApprovalWorkflow state
// machine; the store is the source of truth for persistence.
type PatchJob struct {
	ID                      string         `json:"id"`
	OrgID                   string         `json:"org_id"`
	Title                   string         `json:"title"`
	Description             string         `json:"description"`
	Severity                PatchSeverity  `json:"severity"`
	State                   string         `json:"state"`
	CreatedBy               string         `json:"created_by"`
	ScheduledAt             *time.Time     `json:"scheduled_at,omitempty"`
	MaintenanceWindowStart  *time.Time     `json:"maintenance_window_start,omitempty"`
	MaintenanceWindowEnd    *time.Time     `json:"maintenance_window_end,omitempty"`
	ApprovalTimeout         *time.Time     `json:"approval_timeout,omitempty"`
	RequiredApprovals       int            `json:"required_approvals"`
	AutoApproveOnTimeout    bool           `json:"auto_approve_on_timeout"`
	PackageName             string         `json:"package_name"`
	PackageVersion          string         `json:"package_version,omitempty"`
	RollbackVersion         string         `json:"rollback_version,omitempty"`
	Targets                 []PatchJobTarget `json:"targets,omitempty"`
	Approvals               []ApprovalRecord `json:"approvals,omitempty"`
	FailureReason           string         `json:"failure_reason,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
	CompletedAt             *time.Time     `json:"completed_at,omitempty"`
}

// PatchJobTarget represents a single endpoint targeted by a PatchJob.
// Status is populated by the agent when the patch is dispatched.
type PatchJobTarget struct {
	ID         string    `json:"id"`
	PatchJobID string    `json:"patch_job_id"`
	AgentID    string    `json:"agent_id"`
	Hostname   string    `json:"hostname,omitempty"`
	Status     string    `json:"status"` // pending, running, success, failed
	ErrorMsg   string    `json:"error_msg,omitempty"`
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ApprovalRecord is a single approver's decision on a PatchJob. Multiple
// rows per job are possible (e.g. two-approver rule for major_os).
// Decision is one of "approved" or "rejected".
type ApprovalRecord struct {
	ID         string    `json:"id"`
	PatchJobID string    `json:"patch_job_id"`
	ApproverID string    `json:"approver_id"`
	ApproverName string  `json:"approver_name,omitempty"`
	Decision   string    `json:"decision"`
	Comment    string    `json:"comment,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// PatchStats provides aggregate statistics for the dashboard.
type PatchStats struct {
	TotalJobs       int            `json:"total_jobs"`
	ByState         map[string]int `json:"by_state"`
	BySeverity      map[string]int `json:"by_severity"`
	PendingApproval int            `json:"pending_approval"`
	RecentFailures  int            `json:"recent_failures_24h"`
	AvgApprovalTime float64        `json:"avg_approval_time_hours"`
}

// ScriptDefinition is a reusable, named script that can be enqueued for
// execution on one or more agents. Runtime is one of bash, powershell,
// python, or node. Tags are free-form strings used for filtering.
type ScriptDefinition struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Runtime        string    `json:"runtime"`
	ScriptBody     string    `json:"script_body"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Enabled        bool      `json:"enabled"`
	Tags           []string  `json:"tags,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ScriptRun records a single execution of a ScriptDefinition on a specific
// agent. Status transitions: pending -> running -> completed | failed |
// timed_out | cancelled. Stdout and Stderr are populated as the agent
// reports output. TriggeredBy is the user subject that enqueued the run;
// Scheduled is true when the run was enqueued by a schedule rather than a
// direct user action.
type ScriptRun struct {
	ID          string     `json:"id"`
	ScriptID    string     `json:"script_id"`
	AgentID     string     `json:"agent_id"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Stdout      string     `json:"stdout,omitempty"`
	Stderr      string     `json:"stderr,omitempty"`
	TriggeredBy string     `json:"triggered_by,omitempty"`
	Scheduled   bool       `json:"scheduled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type AuditEvent struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}
