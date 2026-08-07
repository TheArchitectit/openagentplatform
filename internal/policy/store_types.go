package policy

import (
	"context"
	"time"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// PolicyFilter is the filter set for ListPolicies. Zero-valued fields
// are ignored.

type PolicyFilter struct {
	OrgID           string
	Category        string
	EnforcementMode string
	Enabled         *bool
	Search          string
	Limit           int
	Offset          int
}

// ViolationFilter filters GetPolicyViolations.
type ViolationFilter struct {
	AgentID  string
	Resolved *bool
	Limit    int
	Offset   int
}

// ComplianceSummary is the org-level aggregate returned by
// Store.ComplianceSummary. It is consumed by the
// GET /api/v1/compliance/summary endpoint.
type ComplianceSummary struct {
	OrgID             string         `json:"org_id"`
	TotalPolicies     int            `json:"total_policies"`
	EnabledPolicies   int            `json:"enabled_policies"`
	TotalAgents       int            `json:"total_agents"`
	TotalEvaluations  int            `json:"total_evaluations"`
	OpenViolations    int            `json:"open_violations"`
	ResolvedViolations int           `json:"resolved_violations"`
	CompliantPct      float64        `json:"compliant_pct"`
	ByCategory        map[string]int `json:"by_category"`
	BySeverity        map[string]int `json:"by_severity"`
	Trend24h          ComplianceTrend `json:"trend_24h"`
}

// ComplianceTrend is the 24-hour delta of new vs resolved violations.
type ComplianceTrend struct {
	NewViolations      int `json:"new_violations"`
	ResolvedViolations int `json:"resolved_violations"`
}

// Store is the persistence interface for policies, assignments, and
// violations. pgPolicyStore is the default implementation.
type Store interface {
	// Policy CRUD.
	InsertPolicy(ctx context.Context, p *models.Policy) error
	GetPolicy(ctx context.Context, orgID, id string) (*models.Policy, error)
	ListPolicies(ctx context.Context, f PolicyFilter) ([]models.Policy, int, error)
	UpdatePolicy(ctx context.Context, p *models.Policy) error
	SoftDeletePolicy(ctx context.Context, orgID, id string) error

	// Assignments.
	InsertPolicyAssignment(ctx context.Context, a *models.PolicyAssignment) error
	RemovePolicyAssignment(ctx context.Context, id string) error
	ListPolicyAssignments(ctx context.Context, policyID string) ([]models.PolicyAssignment, error)
	ListAssignmentsForAgent(ctx context.Context, agentID string) ([]models.PolicyAssignment, error)
	ListAssignmentsForSite(ctx context.Context, siteID string) ([]models.PolicyAssignment, error)

	// Violations.
	InsertPolicyViolation(ctx context.Context, v *models.PolicyViolation) error
	UpdatePolicyViolationResolved(ctx context.Context, id string, resolvedAt time.Time) error
	GetPolicyViolationByID(ctx context.Context, id string) (*models.PolicyViolation, error)
	GetPolicyViolations(ctx context.Context, policyID string, f ViolationFilter) ([]models.PolicyViolation, int, error)
	CountViolationsByPolicy(ctx context.Context, policyID string) (int, error)
	// ViolationsByAgent returns all violations for a specific agent,
	// optionally filtered by resolved status. Used by the agent detail
	// view in the compliance summary.
	ListViolationsByAgent(ctx context.Context, agentID string, resolved *bool, limit, offset int) ([]models.PolicyViolation, int, error)
	// ComplianceSummary returns aggregate compliance metrics for an org.
	// It returns the total number of evaluations considered, the number
	// that are compliant, the breakdown by category, and the trend
	// (new vs resolved in the last 24h).
	ComplianceSummary(ctx context.Context, orgID string) (ComplianceSummary, error)
	// DismissPolicyViolation marks a violation as dismissed (resolved
	// with a human-supplied reason). Returns the updated record.
	DismissPolicyViolation(ctx context.Context, id, reason, actor string) (*models.PolicyViolation, error)

	// Agent enumeration for batch evaluation.
	ListAllAgentIDs(ctx context.Context, orgID string) ([]string, error)
	ListAgentIDsForSite(ctx context.Context, siteID string) ([]string, error)
}

// pgPolicyStore is the default pgx-backed Store.
type pgPolicyStore struct {
	pool *pgxpool.Pool
}

// NewPGStore constructs a Store backed by a pgx connection pool.
func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgPolicyStore{pool: pool}
}

// --- Policies --------------------------------------------------------------

// InsertPolicy inserts a new policy. ID, timestamps, and the Rego body
// must be set by the caller.
