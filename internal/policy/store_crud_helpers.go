package policy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
