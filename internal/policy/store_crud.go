package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
