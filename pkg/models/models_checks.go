package models

import "time"

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
