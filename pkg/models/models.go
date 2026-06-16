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

type Check struct {
	ID         string         `json:"id"`
	AgentID    string         `json:"agent_id"`
	Type       string         `json:"type"`
	Target     string         `json:"target"`
	Config     map[string]any `json:"config"`
	Schedule   string         `json:"schedule"`
	Enabled    bool           `json:"enabled"`
	CreatedAt  time.Time      `json:"created_at"`
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
