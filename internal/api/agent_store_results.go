package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// ListCheckResultsPaged returns a filtered, paginated slice of check
// results across all agents. Used by the platform-wide endpoint
// GET /api/v1/check-results. Filters: orgID (tenant scope — when
// non-empty, only results from agents in that org are returned),
// agent_id, check_id, status, and a free-text search on the message
// column. The default limit is 50 and the maximum is 500.
func (p *pgAgentStore) ListCheckResultsPaged(ctx context.Context, orgID, agentID, checkID, status, search string, limit, offset int) ([]models.CheckResult, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("agent_store: nil pool")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := make([]any, 0, 6)
	where := make([]string, 0, 5)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if orgID != "" {
		// Tenant scope via the agents table. check_results has no
		// org_id column; ownership is derived from the agent that
		// produced the result.
		add(`EXISTS (SELECT 1 FROM agents WHERE agents.id = check_results.agent_id AND agents.org_id = $%d)`, orgID)
	}
	if agentID != "" {
		add("agent_id = $%d", agentID)
	}
	if checkID != "" {
		add("check_id = $%d", checkID)
	}
	if status != "" {
		add("status = $%d", status)
	}
	if search != "" {
		args = append(args, "%"+search+"%")
		where = append(where, fmt.Sprintf("(message ILIKE $%d)", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAnd(where)
	}

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM check_results "+whereSQL, args...).Scan(&total); err != nil {
		return []models.CheckResult{}, 0, nil
	}

	args = append(args, limit, offset)
	q := fmt.Sprintf(`
		SELECT agent_id, check_id, COALESCE(timestamp, 'epoch'::timestamptz),
		       COALESCE(status,''), COALESCE(value, 0), COALESCE(message,''), metadata
		FROM check_results
		%s
		ORDER BY timestamp DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return []models.CheckResult{}, 0, nil
	}
	defer rows.Close()
	out := make([]models.CheckResult, 0, limit)
	for rows.Next() {
		var r models.CheckResult
		if err := rows.Scan(
			&r.AgentID, &r.CheckID, &r.Timestamp, &r.Status, &r.Value, &r.Message, &r.Metadata,
		); err != nil {
			return nil, 0, fmt.Errorf("agent_store: scan paged result: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("agent_store: paged result rows err: %w", err)
	}
	return out, total, nil
}
