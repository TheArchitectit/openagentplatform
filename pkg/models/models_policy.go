package models

import "time"

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
