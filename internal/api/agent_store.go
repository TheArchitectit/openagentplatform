package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// pgAgentStore is the default implementation of agentStore backed by Postgres.
// It assumes the schema laid out in deploy/migrations has the relevant tables
// (sites, agents, check_results) but tolerates schema drift by returning a
// descriptive error rather than panicking.
type pgAgentStore struct {
	pool *pgxpool.Pool
}

// GetSiteRegistrationToken returns the registration token and org_id for a
// site by id. Returns ErrNoRows if the site does not exist.
func (p *pgAgentStore) GetSiteRegistrationToken(ctx context.Context, siteID string) (string, string, error) {
	if p.pool == nil {
		return "", "", errors.New("agent_store: nil pool")
	}
	const q = `SELECT registration_token, org_id FROM sites WHERE id = $1 LIMIT 1`
	var token, orgID string
	err := p.pool.QueryRow(ctx, q, siteID).Scan(&token, &orgID)
	if err != nil {
		return "", "", fmt.Errorf("agent_store: get site: %w", err)
	}
	return token, orgID, nil
}

// UpsertAgent inserts or updates an agent by id. The caller's agent.ID must
// be set; if it matches an existing row, the row is updated with the latest
// host info and status flipped to "online".
func (p *pgAgentStore) UpsertAgent(ctx context.Context, a *models.Agent) error {
	if p.pool == nil {
		return errors.New("agent_store: nil pool")
	}
	const q = `
		INSERT INTO agents (
			id, site_id, org_id, hostname, os, arch, platform,
			cpu_count, total_memory_mb, total_disk_gb, agent_version,
			status, last_seen, tags, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,
			$12,$13,$14,COALESCE($15, NOW()), $16
		)
		ON CONFLICT (id) DO UPDATE SET
			hostname        = EXCLUDED.hostname,
			os              = EXCLUDED.os,
			arch            = EXCLUDED.arch,
			platform        = EXCLUDED.platform,
			cpu_count       = EXCLUDED.cpu_count,
			total_memory_mb = EXCLUDED.total_memory_mb,
			total_disk_gb   = EXCLUDED.total_disk_gb,
			agent_version   = EXCLUDED.agent_version,
			status          = EXCLUDED.status,
			last_seen       = EXCLUDED.last_seen,
			tags            = EXCLUDED.tags,
			updated_at      = EXCLUDED.updated_at
	`
	_, err := p.pool.Exec(ctx, q,
		a.ID, a.SiteID, a.OrgID, a.Hostname, a.OperatingSystem, a.Arch, a.Platform,
		a.CPUCount, a.TotalMemoryMB, a.TotalDiskGB, a.AgentVersion,
		a.Status, a.LastSeen, a.Tags, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("agent_store: upsert agent: %w", err)
	}
	return nil
}

// GetAgent returns one agent by id.
func (p *pgAgentStore) GetAgent(ctx context.Context, id string) (*models.Agent, error) {
	if p.pool == nil {
		return nil, errors.New("agent_store: nil pool")
	}
	const q = `
		SELECT id, site_id, COALESCE(org_id,''), hostname, COALESCE(os,''), COALESCE(arch,''),
		       COALESCE(platform,''), COALESCE(cpu_count,0), COALESCE(total_memory_mb,0),
		       COALESCE(total_disk_gb,0), COALESCE(agent_version,''), COALESCE(status,'offline'),
		       COALESCE(last_seen, 'epoch'::timestamptz), tags, created_at, updated_at
		FROM agents
		WHERE id = $1
		LIMIT 1
	`
	a := &models.Agent{}
	err := p.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.SiteID, &a.OrgID, &a.Hostname, &a.OperatingSystem, &a.Arch, &a.Platform,
		&a.CPUCount, &a.TotalMemoryMB, &a.TotalDiskGB, &a.AgentVersion, &a.Status,
		&a.LastSeen, &a.Tags, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAgentNotFound
		}
		return nil, fmt.Errorf("agent_store: get agent: %w", err)
	}
	return a, nil
}

// ListAgents returns a filtered, paginated list of agents plus the total
// matching row count (ignoring limit/offset).
func (p *pgAgentStore) ListAgents(ctx context.Context, f AgentListFilter) ([]models.Agent, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("agent_store: nil pool")
	}
	// Build the WHERE clause dynamically. We use ordinal placeholders.
	args := make([]any, 0, 5)
	where := make([]string, 0, 4)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.SiteID != "" {
		add("site_id = $%d", f.SiteID)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where = append(where, fmt.Sprintf("(hostname ILIKE $%d)", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAnd(where)
	}

	// Count.
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM agents "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("agent_store: count agents: %w", err)
	}

	// Page.
	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, site_id, COALESCE(org_id,''), hostname, COALESCE(os,''), COALESCE(arch,''),
		       COALESCE(platform,''), COALESCE(cpu_count,0), COALESCE(total_memory_mb,0),
		       COALESCE(total_disk_gb,0), COALESCE(agent_version,''), COALESCE(status,'offline'),
		       COALESCE(last_seen, 'epoch'::timestamptz), tags, created_at, updated_at
		FROM agents
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("agent_store: list agents: %w", err)
	}
	defer rows.Close()

	out := make([]models.Agent, 0, f.Limit)
	for rows.Next() {
		var a models.Agent
		if err := rows.Scan(
			&a.ID, &a.SiteID, &a.OrgID, &a.Hostname, &a.OperatingSystem, &a.Arch, &a.Platform,
			&a.CPUCount, &a.TotalMemoryMB, &a.TotalDiskGB, &a.AgentVersion, &a.Status,
			&a.LastSeen, &a.Tags, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("agent_store: scan agent: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("agent_store: rows err: %w", err)
	}
	return out, total, nil
}

// ListCheckResultsByAgent returns the most recent N check results for an
// agent. Returns an empty slice if the table does not yet exist (best-effort
// for the MVP).
func (p *pgAgentStore) ListCheckResultsByAgent(ctx context.Context, agentID string, limit int) ([]models.CheckResult, error) {
	if p.pool == nil {
		return nil, errors.New("agent_store: nil pool")
	}
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	const q = `
		SELECT agent_id, check_id, COALESCE(timestamp, 'epoch'::timestamptz),
		       COALESCE(status,''), COALESCE(value, 0), COALESCE(message,''), metadata
		FROM check_results
		WHERE agent_id = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`
	rows, err := p.pool.Query(ctx, q, agentID, limit)
	if err != nil {
		// Tolerate missing table; the migration may not be applied yet.
		return []models.CheckResult{}, nil
	}
	defer rows.Close()
	out := make([]models.CheckResult, 0, limit)
	for rows.Next() {
		var r models.CheckResult
		if err := rows.Scan(
			&r.AgentID, &r.CheckID, &r.Timestamp, &r.Status, &r.Value, &r.Message, &r.Metadata,
		); err != nil {
			return nil, fmt.Errorf("agent_store: scan check result: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// ListCheckResultsByAgentPaged is a paginated variant of
// ListCheckResultsByAgent used by the public REST endpoint
// GET /api/v1/agents/{id}/check-results. It returns results ordered from
// newest to oldest. The default limit is 50 and the maximum is 500 to
// bound memory usage. Returns an empty slice if the table is missing
// (best-effort for the MVP).
func (p *pgAgentStore) ListCheckResultsByAgentPaged(ctx context.Context, agentID string, limit, offset int) ([]models.CheckResult, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("agent_store: nil pool")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	const countQ = `SELECT COUNT(*) FROM check_results WHERE agent_id = $1`
	var total int
	if err := p.pool.QueryRow(ctx, countQ, agentID).Scan(&total); err != nil {
		// Tolerate missing table.
		return []models.CheckResult{}, 0, nil
	}
	const q = `
		SELECT agent_id, check_id, COALESCE(timestamp, 'epoch'::timestamptz),
		       COALESCE(status,''), COALESCE(value, 0), COALESCE(message,''), metadata
		FROM check_results
		WHERE agent_id = $1
		ORDER BY timestamp DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := p.pool.Query(ctx, q, agentID, limit, offset)
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
			return nil, 0, fmt.Errorf("agent_store: scan check result: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("agent_store: check result rows err: %w", err)
	}
	return out, total, nil
}

// ListRecentResults returns the most recent N check results for the
// given (agent_id, check_id) pair, ordered from oldest to newest. It
// is used by the threshold evaluator to count consecutive failures.
// limit is clamped to [1, 200] with a default of 20.
func (p *pgAgentStore) ListRecentResults(ctx context.Context, agentID, checkID string, limit int) ([]models.CheckResult, error) {
	if p.pool == nil {
		return nil, errors.New("agent_store: nil pool")
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	const q = `
		SELECT agent_id, check_id, COALESCE(timestamp, 'epoch'::timestamptz),
		       COALESCE(status,''), COALESCE(value, 0), COALESCE(message,''), metadata
		FROM check_results
		WHERE agent_id = $1 AND check_id = $2
		ORDER BY timestamp DESC
		LIMIT $3
	`
	rows, err := p.pool.Query(ctx, q, agentID, checkID, limit)
	if err != nil {
		// Tolerate missing table; treat as empty history.
		return []models.CheckResult{}, nil
	}
	defer rows.Close()
	out := make([]models.CheckResult, 0, limit)
	for rows.Next() {
		var r models.CheckResult
		if err := rows.Scan(
			&r.AgentID, &r.CheckID, &r.Timestamp, &r.Status, &r.Value, &r.Message, &r.Metadata,
		); err != nil {
			return nil, fmt.Errorf("agent_store: scan recent result: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agent_store: recent result rows err: %w", err)
	}
	// Reverse so the slice is ordered oldest -> newest, matching what
	// the threshold evaluator expects.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ListCheckResultsPaged returns a filtered, paginated slice of check
// results across all agents. Used by the platform-wide endpoint
// GET /api/v1/check-results. Filters: agent_id, check_id, status, and
// a free-text search on the message column. The default limit is 50
// and the maximum is 500.
func (p *pgAgentStore) ListCheckResultsPaged(ctx context.Context, agentID, checkID, status, search string, limit, offset int) ([]models.CheckResult, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("agent_store: nil pool")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := make([]any, 0, 5)
	where := make([]string, 0, 4)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
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

// UpdateAgentHeartbeat is used by the events package to update an agent's
// status, last_seen, and metrics from a heartbeat payload.
func (p *pgAgentStore) UpdateAgentHeartbeat(ctx context.Context, agentID string, status string, lastSeen any, cpu, mem, disk float64) error {
	if p.pool == nil {
		return errors.New("agent_store: nil pool")
	}
	const q = `
		UPDATE agents
		SET status = $2,
		    last_seen = $3,
		    last_cpu_percent = $4,
		    last_mem_percent = $5,
		    last_disk_percent = $6,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := p.pool.Exec(ctx, q, agentID, status, lastSeen, cpu, mem, disk)
	if err != nil {
		return fmt.Errorf("agent_store: update heartbeat: %w", err)
	}
	return nil
}

// MarkStaleAgentsOffline flips any agent whose last_seen is older than
// `threshold` to status='offline'. Returns the list of agent IDs that
// changed state so callers can emit AgentOffline events.
func (p *pgAgentStore) MarkStaleAgentsOffline(ctx context.Context, threshold any) ([]string, error) {
	if p.pool == nil {
		return nil, errors.New("agent_store: nil pool")
	}
	const q = `
		UPDATE agents
		SET status = 'offline', updated_at = NOW()
		WHERE status = 'online'
		  AND last_seen < $1
		RETURNING id
	`
	rows, err := p.pool.Query(ctx, q, threshold)
	if err != nil {
		return nil, fmt.Errorf("agent_store: mark stale: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// InsertCheckResult persists a single check result.
func (p *pgAgentStore) InsertCheckResult(ctx context.Context, r *models.CheckResult) error {
	if p.pool == nil {
		return errors.New("agent_store: nil pool")
	}
	const q = `
		INSERT INTO check_results (agent_id, check_id, timestamp, status, value, message, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := p.pool.Exec(ctx, q, r.AgentID, r.CheckID, r.Timestamp, r.Status, r.Value, r.Message, r.Metadata)
	if err != nil {
		return fmt.Errorf("agent_store: insert check result: %w", err)
	}
	return nil
}

// ErrAgentNotFound is returned by GetAgent when the requested id is missing.
var ErrAgentNotFound = errors.New("agent not found")

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
