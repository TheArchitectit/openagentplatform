package models

import "time"

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
	ID           string    `json:"id"`
	SiteID       string    `json:"site_id"`
	OrgID        string    `json:"org_id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	Platform     string    `json:"platform"`
	CPUCount     int       `json:"cpu_count"`
	TotalMemoryMB int64    `json:"total_memory_mb"`
	TotalDiskGB  int64     `json:"total_disk_gb"`
	AgentVersion string    `json:"agent_version"`
	Version      string    `json:"version"`
	Status       string    `json:"status"`
	LastSeen     time.Time `json:"last_seen"`
	Tags         []string  `json:"tags"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

type Alert struct {
	ID         string    `json:"id"`
	CheckID    string    `json:"check_id"`
	Severity   string    `json:"severity"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type Policy struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type Patch struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	PolicyID   string    `json:"policy_id"`
	Status     string    `json:"status"`
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Script struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	Runtime   string    `json:"runtime"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditEvent struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}
