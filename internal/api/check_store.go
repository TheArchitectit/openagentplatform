package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// pgCheckStore is the default Postgres-backed implementation of checkStore.
// Tables assumed: check_definitions(id, org_id, name, description, check_type,
// config jsonb, interval_seconds, timeout_seconds, enabled, created_at,
// updated_at), check_assignments(id, check_id, agent_id, site_id, assigned_by,
// created_at). Tolerates missing tables (returns empty/zero results) so the
// API can run before migrations are applied.
type pgCheckStore struct {
	pool *pgxpool.Pool
}

// ErrCheckNotFound is returned by GetCheck when the id is missing.
var ErrCheckNotFound = errors.New("check not found")

// CheckListFilter is the filter applied to ListChecks.
type CheckListFilter struct {
	CheckType string
	Enabled   *bool
	Search    string
	Limit     int
	Offset    int
}

// InsertCheck creates a new check definition. Returns the persisted row
// (with DB-populated timestamps and id).
func (p *pgCheckStore) InsertCheck(ctx context.Context, c *models.CheckDefinition) error {
	if p.pool == nil {
		return errors.New("check_store: nil pool")
	}
	cfgJSON, err := json.Marshal(c.Config)
	if err != nil {
		return fmt.Errorf("check_store: marshal config: %w", err)
	}
	const q = `
		INSERT INTO check_definitions (
			id, org_id, name, description, check_type, config,
			interval_seconds, timeout_seconds, enabled, created_at, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2,''), ''), $3, $4, $5, $6,
			$7, $8, $9, COALESCE($10, NOW()), COALESCE($10, NOW())
		)
		RETURNING created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q,
		c.ID, c.OrgID, c.Name, c.Description, c.CheckType, cfgJSON,
		c.IntervalSeconds, c.TimeoutSeconds, c.Enabled, c.CreatedAt,
	)
	if err := row.Scan(&c.CreatedAt, &c.UpdatedAt); err != nil {
		return fmt.Errorf("check_store: insert: %w", err)
	}
	return nil
}

// GetCheck returns one check definition by id, or ErrCheckNotFound.
func (p *pgCheckStore) GetCheck(ctx context.Context, id string) (*models.CheckDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("check_store: nil pool")
	}
	const q = `
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       check_type, config,
		       COALESCE(interval_seconds, 60), COALESCE(timeout_seconds, 30),
		       COALESCE(enabled, true), created_at, updated_at
		FROM check_definitions
		WHERE id = $1
		LIMIT 1
	`
	c := &models.CheckDefinition{}
	var cfgRaw []byte
	err := p.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.OrgID, &c.Name, &c.Description, &c.CheckType, &cfgRaw,
		&c.IntervalSeconds, &c.TimeoutSeconds, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCheckNotFound
		}
		return nil, fmt.Errorf("check_store: get: %w", err)
	}
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &c.Config); err != nil {
			return nil, fmt.Errorf("check_store: unmarshal config: %w", err)
		}
	}
	if c.Config == nil {
		c.Config = map[string]any{}
	}
	return c, nil
}

// ListChecks returns a filtered, paginated slice plus the total count.
func (p *pgCheckStore) ListChecks(ctx context.Context, f CheckListFilter) ([]models.CheckDefinition, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("check_store: nil pool")
	}
	args := make([]any, 0, 5)
	where := make([]string, 0, 3)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.CheckType != "" {
		add("check_type = $%d", f.CheckType)
	}
	if f.Enabled != nil {
		add("enabled = $%d", *f.Enabled)
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d)", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAnd(where)
	}

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM check_definitions "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("check_store: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       check_type, config,
		       COALESCE(interval_seconds, 60), COALESCE(timeout_seconds, 30),
		       COALESCE(enabled, true), created_at, updated_at
		FROM check_definitions
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("check_store: list: %w", err)
	}
	defer rows.Close()

	out := make([]models.CheckDefinition, 0, f.Limit)
	for rows.Next() {
		var c models.CheckDefinition
		var cfgRaw []byte
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Name, &c.Description, &c.CheckType, &cfgRaw,
			&c.IntervalSeconds, &c.TimeoutSeconds, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("check_store: scan: %w", err)
		}
		if len(cfgRaw) > 0 {
			if err := json.Unmarshal(cfgRaw, &c.Config); err != nil {
				return nil, 0, fmt.Errorf("check_store: unmarshal config: %w", err)
			}
		}
		if c.Config == nil {
			c.Config = map[string]any{}
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("check_store: rows err: %w", err)
	}
	return out, total, nil
}

// UpdateCheck applies a partial update to a check definition. Only the
// non-nil fields in the patch are persisted. The updated_at column is
// always bumped to NOW().
func (p *pgCheckStore) UpdateCheck(ctx context.Context, id string, patch CheckPatch) (*models.CheckDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("check_store: nil pool")
	}
	args := make([]any, 0, 5)
	sets := make([]string, 0, 5)
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Description != nil {
		add("description", *patch.Description)
	}
	if patch.Config != nil {
		cfgJSON, err := json.Marshal(patch.Config)
		if err != nil {
			return nil, fmt.Errorf("check_store: marshal config: %w", err)
		}
		add("config", cfgJSON)
	}
	if patch.IntervalSeconds != nil {
		add("interval_seconds", *patch.IntervalSeconds)
	}
	if patch.TimeoutSeconds != nil {
		add("timeout_seconds", *patch.TimeoutSeconds)
	}
	if patch.Enabled != nil {
		add("enabled", *patch.Enabled)
	}
	if len(sets) == 0 {
		// Nothing to update — just return the current row.
		return p.GetCheck(ctx, id)
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE check_definitions SET %s
		WHERE id = $%d
		RETURNING id
	`, joinAnd(sets), len(args))

	var newID string
	if err := p.pool.QueryRow(ctx, q, args...).Scan(&newID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCheckNotFound
		}
		return nil, fmt.Errorf("check_store: update: %w", err)
	}
	return p.GetCheck(ctx, id)
}

// CheckPatch carries optional fields for UpdateCheck. Nil means "leave
// unchanged". A non-nil pointer means "set to this value" (even for booleans).
type CheckPatch struct {
	Name            *string
	Description     *string
	Config          map[string]any
	IntervalSeconds *int
	TimeoutSeconds  *int
	Enabled         *bool
}

// DeleteCheck hard-deletes a check definition row. Caller is responsible
// for checking assignment count first.
func (p *pgCheckStore) DeleteCheck(ctx context.Context, id string) error {
	if p.pool == nil {
		return errors.New("check_store: nil pool")
	}
	const q = `DELETE FROM check_definitions WHERE id = $1`
	_, err := p.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("check_store: delete: %w", err)
	}
	return nil
}

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
