package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// CountAssignments returns the number of active assignments for a check.
func (p *pgCheckStore) CountAssignments(ctx context.Context, checkID string) (int, error) {
	if p.pool == nil {
		return 0, errors.New("check_store: nil pool")
	}
	const q = `SELECT COUNT(*) FROM check_assignments WHERE check_id = $1`
	var n int
	if err := p.pool.QueryRow(ctx, q, checkID).Scan(&n); err != nil {
		return 0, fmt.Errorf("check_store: count assignments: %w", err)
	}
	return n, nil
}

// AssignCheck inserts one assignment row. If agent_id is empty but site_id
// is set, the caller should normally expand to per-agent rows first via
// AssignCheckToSite; this method stores the row as-is.
func (p *pgCheckStore) AssignCheck(ctx context.Context, a *models.CheckAssignment) error {
	if p.pool == nil {
		return errors.New("check_store: nil pool")
	}
	const q = `
		INSERT INTO check_assignments (id, check_id, agent_id, site_id, assigned_by, created_at)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4,''), ''), $5, COALESCE($6, NOW()))
		ON CONFLICT (check_id, agent_id) DO NOTHING
	`
	_, err := p.pool.Exec(ctx, q, a.ID, a.CheckID, a.AgentID, a.SiteID, a.AssignedBy, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("check_store: assign: %w", err)
	}
	return nil
}

// AssignCheckToSite fans out a check to every agent in the given site,
// creating one assignment row per agent. Existing assignments are not
// duplicated (ON CONFLICT DO NOTHING). Returns the number of assignments
// created (excluding any that already existed).
func (p *pgCheckStore) AssignCheckToSite(ctx context.Context, checkID, siteID, assignedBy string) (int, error) {
	if p.pool == nil {
		return 0, errors.New("check_store: nil pool")
	}
	const q = `
		INSERT INTO check_assignments (id, check_id, agent_id, site_id, assigned_by, created_at)
		SELECT gen_random_uuid(), $1, a.id, a.site_id, $3, NOW()
		FROM agents a
		WHERE a.site_id = $2
		ON CONFLICT (check_id, agent_id) DO NOTHING
	`
	tag, err := p.pool.Exec(ctx, q, checkID, siteID, assignedBy)
	if err != nil {
		return 0, fmt.Errorf("check_store: assign site: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RemoveAssignment deletes a single (check_id, agent_id) assignment row.
// Returns ErrAssignmentNotFound if no row was deleted.
func (p *pgCheckStore) RemoveAssignment(ctx context.Context, checkID, agentID string) error {
	if p.pool == nil {
		return errors.New("check_store: nil pool")
	}
	const q = `DELETE FROM check_assignments WHERE check_id = $1 AND agent_id = $2`
	tag, err := p.pool.Exec(ctx, q, checkID, agentID)
	if err != nil {
		return fmt.Errorf("check_store: remove assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAssignmentNotFound
	}
	return nil
}

// ListAssignments returns all assignments for a check, each joined with
// the agent's hostname and the agent's most recent result for this check.
func (p *pgCheckStore) ListAssignments(ctx context.Context, checkID string) ([]models.CheckAssignmentDetail, error) {
	if p.pool == nil {
		return nil, errors.New("check_store: nil pool")
	}
	const q = `
		SELECT ca.id, ca.agent_id, COALESCE(ag.hostname, ''), COALESCE(ca.site_id, ''),
		       ca.created_at
		FROM check_assignments ca
		LEFT JOIN agents ag ON ag.id = ca.agent_id
		WHERE ca.check_id = $1
		ORDER BY ca.created_at DESC
	`
	rows, err := p.pool.Query(ctx, q, checkID)
	if err != nil {
		return nil, fmt.Errorf("check_store: list assignments: %w", err)
	}
	defer rows.Close()
	out := make([]models.CheckAssignmentDetail, 0, 8)
	for rows.Next() {
		var d models.CheckAssignmentDetail
		if err := rows.Scan(&d.AssignmentID, &d.AgentID, &d.Hostname, &d.SiteID, &d.AssignedAt); err != nil {
			return nil, fmt.Errorf("check_store: scan assignment: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("check_store: assignments rows err: %w", err)
	}

	// Enrich with the most recent result per assignment. This is a
	// second pass so the LATERAL join / DISTINCT ON trick doesn't have
	// to be expressed inline.
	for i := range out {
		r, err := p.latestResult(ctx, out[i].AgentID, checkID)
		if err != nil {
			// Best-effort: ignore lookup failures here; the handler logs.
			continue
		}
		out[i].LastResult = r
	}
	return out, nil
}

// latestResult returns the most recent check_results row for (agent_id, check_id),
// or nil if none exists.
func (p *pgCheckStore) latestResult(ctx context.Context, agentID, checkID string) (*models.CheckResult, error) {
	const q = `
		SELECT agent_id, check_id, timestamp, status, value, message
		FROM check_results
		WHERE agent_id = $1 AND check_id = $2
		ORDER BY timestamp DESC
		LIMIT 1
	`
	r := &models.CheckResult{}
	err := p.pool.QueryRow(ctx, q, agentID, checkID).Scan(
		&r.AgentID, &r.CheckID, &r.Timestamp, &r.Status, &r.Value, &r.Message,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// GetAssignmentsForAgent returns all check_ids assigned to a given agent.
// Used by the agent command stream to know which checks to execute.
func (p *pgCheckStore) GetAssignmentsForAgent(ctx context.Context, agentID string) ([]string, error) {
	if p.pool == nil {
		return nil, errors.New("check_store: nil pool")
	}
	const q = `SELECT check_id FROM check_assignments WHERE agent_id = $1`
	rows, err := p.pool.Query(ctx, q, agentID)
	if err != nil {
		return nil, fmt.Errorf("check_store: assignments for agent: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ErrAssignmentNotFound is returned by RemoveAssignment when no row matched.
var ErrAssignmentNotFound = errors.New("assignment not found")
