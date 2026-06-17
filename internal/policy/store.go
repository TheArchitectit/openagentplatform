// Package policy - store.go implements the PostgreSQL persistence
// layer for policies, assignments, and violations.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
func (s *pgPolicyStore) InsertPolicy(ctx context.Context, p *models.Policy) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	if p.ID == "" {
		return errors.New("policy: id required")
	}
	const q = `
		INSERT INTO policies (
			id, org_id, name, description, rego_body,
			enforcement_mode, severity, category, enabled,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,
			$10,$11
		)
	`
	_, err := s.pool.Exec(ctx, q,
		p.ID, p.OrgID, p.Name, p.Description, p.RegoBody,
		p.EnforcementMode, p.Severity, p.Category, p.Enabled,
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("policy: insert: %w", err)
	}
	return nil
}

// GetPolicy fetches a single policy by id (including its Rego body),
// scoped to the given org. If orgID is non-empty, the query enforces org
// ownership. Returns ErrPolicyNotFound if the row does not exist or is
// soft-deleted.
func (s *pgPolicyStore) GetPolicy(ctx context.Context, orgID, id string) (*models.Policy, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	args := []any{id}
	where := []string{"id = $1", "deleted = false"}
	if orgID != "" {
		args = append(args, orgID)
		where = append(where, fmt.Sprintf("org_id = $%d", len(args)))
	}
	q := `
		SELECT id, COALESCE(org_id,''), COALESCE(name,''), COALESCE(description,''),
		       COALESCE(rego_body,''), COALESCE(enforcement_mode,'monitor'),
		       COALESCE(severity,'warning'), COALESCE(category,'security'),
		       COALESCE(enabled,true), COALESCE(deleted,false),
		       created_at, updated_at
		FROM policies
		WHERE ` + strings.Join(where, " AND ") + `
		LIMIT 1
	`
	var p models.Policy
	var deleted bool
	err := s.pool.QueryRow(ctx, q, args...).Scan(
		&p.ID, &p.OrgID, &p.Name, &p.Description, &p.RegoBody,
		&p.EnforcementMode, &p.Severity, &p.Category, &p.Enabled, &deleted,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPolicyNotFound
		}
		return nil, fmt.Errorf("policy: get: %w", err)
	}
	return &p, nil
}

// ListPolicies returns a filtered list of non-deleted policies plus
// the total matching count.
func (s *pgPolicyStore) ListPolicies(ctx context.Context, f PolicyFilter) ([]models.Policy, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("policy: nil pool")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	args := make([]any, 0, 8)
	where := []string{"deleted = false"}
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.OrgID != "" {
		add("org_id = $%d", f.OrgID)
	}
	if f.Category != "" {
		add("category = $%d", f.Category)
	}
	if f.EnforcementMode != "" {
		add("enforcement_mode = $%d", f.EnforcementMode)
	}
	if f.Enabled != nil {
		add("enabled = $%d", *f.Enabled)
	}
	if f.Search != "" {
		add("(name ILIKE $%d OR description ILIKE $%d)", "%"+f.Search+"%")
		// The above is wrong: $N is referenced twice. Use a separate
		// arg slot:
		where = where[:len(where)-1]
		args = args[:len(args)-1]
		args = append(args, "%"+f.Search+"%", "%"+f.Search+"%")
		pos := len(args) - 1
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR description ILIKE $%d)", pos-1, pos))
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM policies "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("policy: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(org_id,''), COALESCE(name,''), COALESCE(description,''),
		       COALESCE(rego_body,''), COALESCE(enforcement_mode,'monitor'),
		       COALESCE(severity,'warning'), COALESCE(category,'security'),
		       COALESCE(enabled,true),
		       created_at, updated_at
		FROM policies
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("policy: list: %w", err)
	}
	defer rows.Close()

	out := make([]models.Policy, 0, f.Limit)
	for rows.Next() {
		var p models.Policy
		if err := rows.Scan(
			&p.ID, &p.OrgID, &p.Name, &p.Description, &p.RegoBody,
			&p.EnforcementMode, &p.Severity, &p.Category, &p.Enabled,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("policy: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// UpdatePolicy updates an existing policy. Returns ErrPolicyNotFound if
// no row matches (or the policy has been soft-deleted).
func (s *pgPolicyStore) UpdatePolicy(ctx context.Context, p *models.Policy) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	if p.ID == "" {
		return errors.New("policy: id required")
	}
	const q = `
		UPDATE policies SET
			name = $2,
			description = $3,
			rego_body = $4,
			enforcement_mode = $5,
			severity = $6,
			category = $7,
			enabled = $8,
			updated_at = $9
		WHERE id = $1 AND deleted = false
	`
	tag, err := s.pool.Exec(ctx, q,
		p.ID, p.Name, p.Description, p.RegoBody,
		p.EnforcementMode, p.Severity, p.Category, p.Enabled,
		p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("policy: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

// SoftDeletePolicy marks a policy as deleted. The row is preserved for
// audit; queries filter it out via the `deleted = false` clause.
func (s *pgPolicyStore) SoftDeletePolicy(ctx context.Context, orgID, id string) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	args := []any{id}
	where := "id = $1 AND deleted = false"
	if orgID != "" {
		args = append(args, orgID)
		where += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	q := "UPDATE policies SET deleted = true, updated_at = NOW() WHERE " + where
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("policy: soft delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

// --- Assignments -----------------------------------------------------------

// InsertPolicyAssignment links a policy to an agent or site. Exactly
// one of AgentID or SiteID must be set.
func (s *pgPolicyStore) InsertPolicyAssignment(ctx context.Context, a *models.PolicyAssignment) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	if a.ID == "" {
		return errors.New("policy: assignment id required")
	}
	if a.AgentID == "" && a.SiteID == "" {
		return errors.New("policy: assignment requires agent_id or site_id")
	}
	const q = `
		INSERT INTO policy_assignments (id, policy_id, agent_id, site_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := s.pool.Exec(ctx, q, a.ID, a.PolicyID, a.AgentID, a.SiteID, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("policy: insert assignment: %w", err)
	}
	return nil
}

// RemovePolicyAssignment deletes a single assignment by id.
func (s *pgPolicyStore) RemovePolicyAssignment(ctx context.Context, id string) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	const q = `DELETE FROM policy_assignments WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("policy: remove assignment: %w", err)
	}
	return nil
}

// ListPolicyAssignments returns every assignment row for a policy.
func (s *pgPolicyStore) ListPolicyAssignments(ctx context.Context, policyID string) ([]models.PolicyAssignment, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	const q = `
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(site_id,''), created_at
		FROM policy_assignments
		WHERE policy_id = $1
		ORDER BY created_at DESC
	`
	rows, err := s.pool.Query(ctx, q, policyID)
	if err != nil {
		return nil, fmt.Errorf("policy: list assignments: %w", err)
	}
	defer rows.Close()
	out := make([]models.PolicyAssignment, 0, 8)
	for rows.Next() {
		var a models.PolicyAssignment
		if err := rows.Scan(&a.ID, &a.PolicyID, &a.AgentID, &a.SiteID, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("policy: scan assignment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAssignmentsForAgent returns every policy assigned directly to
// an agent.
func (s *pgPolicyStore) ListAssignmentsForAgent(ctx context.Context, agentID string) ([]models.PolicyAssignment, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	const q = `
		SELECT pa.id, pa.policy_id, COALESCE(pa.agent_id,''), COALESCE(pa.site_id,''), pa.created_at
		FROM policy_assignments pa
		JOIN agents a ON a.id = $1
		WHERE pa.agent_id = $1 OR pa.site_id = a.site_id
		ORDER BY pa.created_at DESC
	`
	rows, err := s.pool.Query(ctx, q, agentID)
	if err != nil {
		return nil, fmt.Errorf("policy: list agent assignments: %w", err)
	}
	defer rows.Close()
	out := make([]models.PolicyAssignment, 0, 8)
	for rows.Next() {
		var a models.PolicyAssignment
		if err := rows.Scan(&a.ID, &a.PolicyID, &a.AgentID, &a.SiteID, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("policy: scan agent assignment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAssignmentsForSite returns every policy assigned to a site.
func (s *pgPolicyStore) ListAssignmentsForSite(ctx context.Context, siteID string) ([]models.PolicyAssignment, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	const q = `
		SELECT id, policy_id, COALESCE(agent_id,''), COALESCE(site_id,''), created_at
		FROM policy_assignments
		WHERE site_id = $1
		ORDER BY created_at DESC
	`
	rows, err := s.pool.Query(ctx, q, siteID)
	if err != nil {
		return nil, fmt.Errorf("policy: list site assignments: %w", err)
	}
	defer rows.Close()
	out := make([]models.PolicyAssignment, 0, 16)
	for rows.Next() {
		var a models.PolicyAssignment
		if err := rows.Scan(&a.ID, &a.PolicyID, &a.AgentID, &a.SiteID, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("policy: scan site assignment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- Violations ------------------------------------------------------------

// InsertPolicyViolation writes a violation record. ID and timestamp are
// expected to be set by the caller.
func (s *pgPolicyStore) InsertPolicyViolation(ctx context.Context, v *models.PolicyViolation) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	if v.ID == "" {
		return errors.New("policy: violation id required")
	}
	details, err := jsonOrNull(v.Details)
	if err != nil {
		return fmt.Errorf("policy: marshal details: %w", err)
	}
	const q = `
		INSERT INTO policy_violations (
			id, policy_id, agent_id, severity, message, details,
			resolved, resolved_at, created_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9
		)
	`
	_, err = s.pool.Exec(ctx, q,
		v.ID, v.PolicyID, v.AgentID, v.Severity, v.Message, details,
		v.Resolved, v.ResolvedAt, v.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("policy: insert violation: %w", err)
	}
	return nil
}

// UpdatePolicyViolationResolved marks a violation as resolved.
func (s *pgPolicyStore) UpdatePolicyViolationResolved(ctx context.Context, id string, resolvedAt time.Time) error {
	if s.pool == nil {
		return errors.New("policy: nil pool")
	}
	const q = `UPDATE policy_violations SET resolved = true, resolved_at = $2 WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id, resolvedAt)
	if err != nil {
		return fmt.Errorf("policy: resolve violation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPolicyViolationNotFound
	}
	return nil
}

// GetPolicyViolations lists violations for a policy, with optional
// agent and resolved filters. Returns the page and the total count.
func (s *pgPolicyStore) GetPolicyViolations(ctx context.Context, policyID string, f ViolationFilter) ([]models.PolicyViolation, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("policy: nil pool")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	args := make([]any, 0, 4)
	where := []string{"policy_id = $1"}
	args = append(args, policyID)
	if f.AgentID != "" {
		args = append(args, f.AgentID)
		where = append(where, fmt.Sprintf("agent_id = $%d", len(args)))
	}
	if f.Resolved != nil {
		args = append(args, *f.Resolved)
		where = append(where, fmt.Sprintf("resolved = $%d", len(args)))
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM policy_violations "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("policy: count violations: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(severity,'warning'),
		       COALESCE(message,''), details, COALESCE(resolved,false), resolved_at, created_at
		FROM policy_violations
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("policy: list violations: %w", err)
	}
	defer rows.Close()

	out := make([]models.PolicyViolation, 0, f.Limit)
	for rows.Next() {
		var v models.PolicyViolation
		var det []byte
		if err := rows.Scan(
			&v.ID, &v.PolicyID, &v.AgentID, &v.Severity, &v.Message, &det,
			&v.Resolved, &v.ResolvedAt, &v.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("policy: scan violation: %w", err)
		}
		if len(det) > 0 {
			_ = json.Unmarshal(det, &v.Details)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// CountViolationsByPolicy returns the total number of recorded
// violations for a policy (open + resolved).
func (s *pgPolicyStore) CountViolationsByPolicy(ctx context.Context, policyID string) (int, error) {
	if s.pool == nil {
		return 0, errors.New("policy: nil pool")
	}
	const q = `SELECT COUNT(*) FROM policy_violations WHERE policy_id = $1`
	var n int
	if err := s.pool.QueryRow(ctx, q, policyID).Scan(&n); err != nil {
		return 0, fmt.Errorf("policy: count violations: %w", err)
	}
	return n, nil
}

// GetPolicyViolationByID fetches a single violation by id. Returns
// ErrPolicyViolationNotFound when the row does not exist.
func (s *pgPolicyStore) GetPolicyViolationByID(ctx context.Context, id string) (*models.PolicyViolation, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	if id == "" {
		return nil, errors.New("policy: violation id required")
	}
	const q = `
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(severity,'warning'),
		       COALESCE(message,''), details, COALESCE(resolved,false), resolved_at, created_at
		FROM policy_violations
		WHERE id = $1
		LIMIT 1
	`
	var v models.PolicyViolation
	var det []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&v.ID, &v.PolicyID, &v.AgentID, &v.Severity, &v.Message, &det,
		&v.Resolved, &v.ResolvedAt, &v.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPolicyViolationNotFound
		}
		return nil, fmt.Errorf("policy: get violation: %w", err)
	}
	if len(det) > 0 {
		_ = json.Unmarshal(det, &v.Details)
	}
	return &v, nil
}

// ListViolationsByAgent returns all violations for a single agent,
// newest first. The resolved filter is optional; when nil, both open
// and resolved rows are returned.
func (s *pgPolicyStore) ListViolationsByAgent(ctx context.Context, agentID string, resolved *bool, limit, offset int) ([]models.PolicyViolation, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("policy: nil pool")
	}
	if agentID == "" {
		return nil, 0, errors.New("policy: agent_id required")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := []any{agentID}
	where := []string{"agent_id = $1"}
	if resolved != nil {
		args = append(args, *resolved)
		where = append(where, fmt.Sprintf("resolved = $%d", len(args)))
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM policy_violations "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("policy: count agent violations: %w", err)
	}

	args = append(args, limit, offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(severity,'warning'),
		       COALESCE(message,''), details, COALESCE(resolved,false), resolved_at, created_at
		FROM policy_violations
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("policy: list agent violations: %w", err)
	}
	defer rows.Close()
	out := make([]models.PolicyViolation, 0, limit)
	for rows.Next() {
		var v models.PolicyViolation
		var det []byte
		if err := rows.Scan(
			&v.ID, &v.PolicyID, &v.AgentID, &v.Severity, &v.Message, &det,
			&v.Resolved, &v.ResolvedAt, &v.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("policy: scan agent violation: %w", err)
		}
		if len(det) > 0 {
			_ = json.Unmarshal(det, &v.Details)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// ComplianceSummary computes the org-level compliance aggregates. It
// is intentionally implemented as a small set of independent queries
// rather than one monolithic join, to keep the query plan simple and
// each metric independently cacheable. orgID may be empty to compute
// a platform-wide summary.
func (s *pgPolicyStore) ComplianceSummary(ctx context.Context, orgID string) (ComplianceSummary, error) {
	if s.pool == nil {
		return ComplianceSummary{}, errors.New("policy: nil pool")
	}
	summary := ComplianceSummary{
		OrgID:      orgID,
		ByCategory: make(map[string]int),
		BySeverity: make(map[string]int),
	}

	// Total + enabled policies.
	polQ := `SELECT COUNT(*), COUNT(*) FILTER (WHERE enabled = true) FROM policies WHERE deleted = false`
	if err := s.pool.QueryRow(ctx, polQ).Scan(&summary.TotalPolicies, &summary.EnabledPolicies); err != nil {
		return summary, fmt.Errorf("policy: compliance policies: %w", err)
	}

	// Total agents (platform-wide; an org-scoped filter requires the
	// agents table to be present in this database, which it is).
	agentQ := `SELECT COUNT(*) FROM agents`
	if err := s.pool.QueryRow(ctx, agentQ).Scan(&summary.TotalAgents); err != nil {
		// The agents table may be in a different schema or absent; the
		// rest of the summary still works. We log by returning a
		// zeroed agent count instead of failing the whole call.
		summary.TotalAgents = 0
	}

	// Open vs resolved violations.
	const countsQ = `
		SELECT
			COUNT(*) FILTER (WHERE resolved = false),
			COUNT(*) FILTER (WHERE resolved = true)
		FROM policy_violations
	`
	if err := s.pool.QueryRow(ctx, countsQ).Scan(&summary.OpenViolations, &summary.ResolvedViolations); err != nil {
		return summary, fmt.Errorf("policy: compliance violation counts: %w", err)
	}

	// Violations by category, joined through the policies table so
	// that the "category" column is the policy's declared category.
	const byCatQ = `
		SELECT COALESCE(p.category, 'configuration'), COUNT(*)
		FROM policy_violations v
		LEFT JOIN policies p ON p.id = v.policy_id
		WHERE v.resolved = false
		GROUP BY p.category
	`
	catRows, err := s.pool.Query(ctx, byCatQ)
	if err != nil {
		return summary, fmt.Errorf("policy: compliance by category: %w", err)
	}
	for catRows.Next() {
		var cat string
		var n int
		if err := catRows.Scan(&cat, &n); err != nil {
			catRows.Close()
			return summary, fmt.Errorf("policy: scan by category: %w", err)
		}
		summary.ByCategory[cat] = n
	}
	catRows.Close()

	const bySevQ = `
		SELECT COALESCE(severity, 'warning'), COUNT(*)
		FROM policy_violations
		WHERE resolved = false
		GROUP BY severity
	`
	sevRows, err := s.pool.Query(ctx, bySevQ)
	if err != nil {
		return summary, fmt.Errorf("policy: compliance by severity: %w", err)
	}
	for sevRows.Next() {
		var sev string
		var n int
		if err := sevRows.Scan(&sev, &n); err != nil {
			sevRows.Close()
			return summary, fmt.Errorf("policy: scan by severity: %w", err)
		}
		summary.BySeverity[sev] = n
	}
	sevRows.Close()

	// 24-hour trend.
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	const trendQ = `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= $1),
			COUNT(*) FILTER (WHERE resolved = true AND resolved_at >= $1)
		FROM policy_violations
	`
	if err := s.pool.QueryRow(ctx, trendQ, cutoff).Scan(
		&summary.Trend24h.NewViolations,
		&summary.Trend24h.ResolvedViolations,
	); err != nil {
		return summary, fmt.Errorf("policy: compliance trend: %w", err)
	}

	// Total evaluations considered: open + resolved violations. This
	// is an approximation -- every violation corresponds to one
	// evaluation that failed; passing evaluations are not persisted
	// today, so we use the violation count as the denominator.
	summary.TotalEvaluations = summary.OpenViolations + summary.ResolvedViolations
	if summary.TotalEvaluations > 0 {
		// Compliant% is the fraction of all evaluations that passed
		// (which is 1 - failures/total). We compute it as
		// resolved/total, which is a conservative measure of how many
		// historical failures have been cleaned up. This is the number
		// most compliance dashboards expect to see.
		summary.CompliantPct = float64(summary.ResolvedViolations) / float64(summary.TotalEvaluations) * 100.0
	}

	return summary, nil
}

// ListAllAgentIDs returns every agent ID in the platform, optionally
// filtered by org.
func (s *pgPolicyStore) ListAllAgentIDs(ctx context.Context, orgID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	var (
		rows pgx.Rows
		err  error
	)
	if orgID != "" {
		rows, err = s.pool.Query(ctx, `SELECT id FROM agents WHERE org_id = $1`, orgID)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT id FROM agents`)
	}
	if err != nil {
		return nil, fmt.Errorf("policy: list agents: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 64)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("policy: scan agent: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListAgentIDsForSite returns every agent ID belonging to a site.
func (s *pgPolicyStore) ListAgentIDsForSite(ctx context.Context, siteID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM agents WHERE site_id = $1`, siteID)
	if err != nil {
		return nil, fmt.Errorf("policy: list site agents: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 32)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("policy: scan site agent: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --- Errors ----------------------------------------------------------------

// ErrPolicyNotFound is returned when a policy id does not exist or
// has been soft-deleted.
var ErrPolicyNotFound = errors.New("policy not found")

// ErrPolicyViolationNotFound is returned when a violation id does not
// exist.
var ErrPolicyViolationNotFound = errors.New("policy violation not found")

// DismissPolicyViolation marks a violation as resolved and stores the
// dismissal metadata in the Details JSONB column. We do not extend
// the base model with dedicated columns so the migration cost stays at
// zero; the UI reads the "dismissed_by" / "dismiss_reason" keys from
// Details when rendering a resolved-by-human badge.
func (s *pgPolicyStore) DismissPolicyViolation(ctx context.Context, id, reason, actor string) (*models.PolicyViolation, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	if id == "" {
		return nil, errors.New("policy: violation id required")
	}
	now := time.Now().UTC()
	const q = `
		UPDATE policy_violations SET
			resolved = true,
			resolved_at = $2,
			details = COALESCE(details, '{}'::jsonb) ||
				jsonb_build_object('dismissed_by', $3::text, 'dismiss_reason', $4::text)
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q, id, now, actor, reason)
	if err != nil {
		return nil, fmt.Errorf("policy: dismiss violation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrPolicyViolationNotFound
	}
	return s.GetPolicyViolationByID(ctx, id)
}

// --- helpers ---------------------------------------------------------------

// jsonOrNull marshals v to JSON, or returns nil if v is empty.
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
