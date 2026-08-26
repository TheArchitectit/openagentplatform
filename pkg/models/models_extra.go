package models

import (
	"encoding/json"
	"time"
)

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
	ID                     string           `json:"id"`
	OrgID                  string           `json:"org_id"`
	Title                  string           `json:"title"`
	Description            string           `json:"description"`
	Severity               PatchSeverity    `json:"severity"`
	State                  string           `json:"state"`
	CreatedBy              string           `json:"created_by"`
	ScheduledAt            *time.Time       `json:"scheduled_at,omitempty"`
	MaintenanceWindowStart *time.Time       `json:"maintenance_window_start,omitempty"`
	MaintenanceWindowEnd   *time.Time       `json:"maintenance_window_end,omitempty"`
	ApprovalTimeout        *time.Time       `json:"approval_timeout,omitempty"`
	RequiredApprovals      int              `json:"required_approvals"`
	AutoApproveOnTimeout   bool             `json:"auto_approve_on_timeout"`
	PackageName            string           `json:"package_name"`
	PackageVersion         string           `json:"package_version,omitempty"`
	RollbackVersion        string           `json:"rollback_version,omitempty"`
	Targets                []PatchJobTarget `json:"targets,omitempty"`
	Approvals              []ApprovalRecord `json:"approvals,omitempty"`
	FailureReason          string           `json:"failure_reason,omitempty"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
	CompletedAt            *time.Time       `json:"completed_at,omitempty"`
}

// WinUpdateKBState records the per-KB lifecycle state of a single
// Windows Update article on a single agent. The state field is driven
// by the WinUpdate state machine in internal/patches/winupdate_states.go;
// the store is the source of truth for persistence. This table is
// independent of patch_job_targets because it is populated by agent
// scan/install reports rather than by deployment jobs.
type WinUpdateKBState struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	AgentID   string    `json:"agent_id"`
	KB        string    `json:"kb"`
	State     string    `json:"state"`
	Result    string    `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PatchJobTarget represents a single endpoint targeted by a PatchJob.
// Status is populated by the agent when the patch is dispatched.
type PatchJobTarget struct {
	ID         string     `json:"id"`
	PatchJobID string     `json:"patch_job_id"`
	AgentID    string     `json:"agent_id"`
	Hostname   string     `json:"hostname,omitempty"`
	Status     string     `json:"status"` // pending, running, success, failed
	ErrorMsg   string     `json:"error_msg,omitempty"`
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ApprovalRecord is a single approver's decision on a PatchJob. Multiple
// rows per job are possible (e.g. two-approver rule for major_os).
// Decision is one of "approved" or "rejected".
type ApprovalRecord struct {
	ID           string    `json:"id"`
	PatchJobID   string    `json:"patch_job_id"`
	ApproverID   string    `json:"approver_id"`
	ApproverName string    `json:"approver_name,omitempty"`
	Decision     string    `json:"decision"`
	Comment      string    `json:"comment,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
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

// CVEEnrichment holds NVD-sourced metadata for a CVE record. It is
// populated by the NVD ingest service and used for CVSS scores and
// descriptions. The patch_catalog.cve_ids JSONB column stores the
// CVE IDs associated with each KB; this table provides the detail.
type CVEEnrichment struct {
	ID             string           `json:"id"`
	CVEID          string           `json:"cve_id"`
	Source         string           `json:"source"`
	CvssV3Score    *float64         `json:"cvss_v3_score,omitempty"`
	CvssV3Severity string           `json:"cvss_v3_severity,omitempty"`
	Description    string           `json:"description,omitempty"`
	PublishedDate  *time.Time       `json:"published_date,omitempty"`
	LastModified   *time.Time       `json:"last_modified,omitempty"`
	RawData        json.RawMessage  `json:"raw_data,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// ScriptDefinition is a reusable, named script that can be enqueued for
// execution on one or more agents. Runtime is one of bash, powershell,
// python, or node. Tags are free-form strings used for filtering.
type ScriptDefinition struct {
	ID                 string         `json:"id"`
	OrgID              string         `json:"org_id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Body               string         `json:"body" db:"body"`
	Runtime            string         `json:"runtime"`
	Arguments          []any          `json:"arguments,omitempty"`
	EnvVars            map[string]any `json:"env_vars,omitempty"`
	TimeoutSeconds     int            `json:"timeout_seconds"`
	SupportedPlatforms []string       `json:"supported_platforms,omitempty"`
	Category           string         `json:"category,omitempty"`
	IsTemplate         bool           `json:"is_template"`
	CreatedBy          string         `json:"created_by,omitempty"`
	Enabled            bool           `json:"enabled"`
	Tags               []string       `json:"tags,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
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
	ID        string         `json:"id"`
	ActorID   string         `json:"actor_id"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// AutomatedTaskAction is the discriminated action an automated task performs.
const (
	TaskActionPatchDeploy TaskAction = "patch_deploy"
	TaskActionReboot      TaskAction = "reboot"
	TaskActionScriptRun   TaskAction = "script_run"
	TaskActionCheckEnable TaskAction = "check_enable"
)

// TaskAction is the discriminated action type.
type TaskAction string

// AutomatedTask is a cron-scheduled action bound to a Policy. It is persisted as
// an element of the Policy.automated_tasks JSONB array
// (py/alembic/versions/0005_policies.py). The cron_expr is validated against the
// internal/reports parseSimpleCron parser (+ @hourly/@daily/@weekly/@monthly
// aliases) and rejected on parse failure (fail-closed).
type AutomatedTask struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Enabled   bool            `json:"enabled"`
	CronExpr  string          `json:"cron_expr"`
	Action    TaskAction      `json:"action"`
	Params    json.RawMessage `json:"params"`
	Timezone  string          `json:"timezone"`
	NextRunAt *time.Time      `json:"next_run_at,omitempty"`
	LastRunAt *time.Time      `json:"last_run_at,omitempty"`
	LastStatus string         `json:"last_status,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
