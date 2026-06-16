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

// pgScriptStore is the default Postgres-backed implementation of scriptStore.
// Tables assumed: script_definitions(id, org_id, name, description, runtime,
// script_body, timeout_seconds, enabled, tags jsonb, created_at, updated_at,
// deleted_at), script_runs(id, script_id, agent_id, status, started_at,
// finished_at, exit_code, stdout, stderr, triggered_by, scheduled,
// created_at, updated_at). Tolerates missing tables (returns empty/zero
// results) so the API can run before migrations are applied.
type pgScriptStore struct {
	pool *pgxpool.Pool
}

// NewPGScriptStore constructs a scriptStore backed by a pgx connection pool.
// Callers wire this into the API server via Server.SetScriptStore.
func NewPGScriptStore(pool *pgxpool.Pool) scriptStore {
	return &pgScriptStore{pool: pool}
}

// ErrScriptNotFound is returned by GetScript when the id is missing
// (and not soft-deleted).
var ErrScriptNotFound = errors.New("script not found")

// ErrScriptRunNotFound is returned by GetScriptRun when the run id is missing.
var ErrScriptRunNotFound = errors.New("script run not found")

// ScriptListFilter is the filter applied to ListScripts.
type ScriptListFilter struct {
	Runtime string
	Enabled *bool
	Tag     string
	Search  string
	Limit   int
	Offset  int
}

// ScriptRunListFilter is the filter applied to ListScriptRuns.
type ScriptRunListFilter struct {
	ScriptID string
	AgentID  string
	Status   string
	Limit    int
	Offset   int
}

// ScriptPatch carries optional fields for UpdateScript. Nil means "leave
// unchanged". A non-nil pointer means "set to this value" (even for booleans).
type ScriptPatch struct {
	Name           *string
	Description    *string
	ScriptBody     *string
	Runtime        *string
	TimeoutSeconds *int
	Enabled        *bool
	Tags           []string
}

// InsertScript creates a new script definition. Returns the persisted row
// (with DB-populated timestamps).
func (p *pgScriptStore) InsertScript(ctx context.Context, s *models.ScriptDefinition) error {
	if p.pool == nil {
		return errors.New("script_store: nil pool")
	}
	tagsJSON, err := json.Marshal(s.Tags)
	if err != nil {
		return fmt.Errorf("script_store: marshal tags: %w", err)
	}
	const q = `
		INSERT INTO script_definitions (
			id, org_id, name, description, runtime, script_body,
			timeout_seconds, enabled, tags, created_at, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2,''), ''), $3, $4, $5, $6,
			$7, $8, $9, COALESCE($10, NOW()), COALESCE($10, NOW())
		)
		RETURNING created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q,
		s.ID, s.OrgID, s.Name, s.Description, s.Runtime, s.ScriptBody,
		s.TimeoutSeconds, s.Enabled, tagsJSON, s.CreatedAt,
	)
	if err := row.Scan(&s.CreatedAt, &s.UpdatedAt); err != nil {
		return fmt.Errorf("script_store: insert: %w", err)
	}
	return nil
}

// GetScript returns one script definition by id, or ErrScriptNotFound.
// Soft-deleted rows (deleted_at IS NOT NULL) are treated as not found.
func (p *pgScriptStore) GetScript(ctx context.Context, id string) (*models.ScriptDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("script_store: nil pool")
	}
	const q = `
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       runtime, COALESCE(script_body,''),
		       COALESCE(timeout_seconds, 30), COALESCE(enabled, true),
		       COALESCE(tags, '[]'::jsonb),
		       created_at, updated_at
		FROM script_definitions
		WHERE id = $1 AND deleted_at IS NULL
		LIMIT 1
	`
	s := &models.ScriptDefinition{}
	var tagsRaw []byte
	err := p.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.OrgID, &s.Name, &s.Description,
		&s.Runtime, &s.ScriptBody,
		&s.TimeoutSeconds, &s.Enabled, &tagsRaw,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScriptNotFound
		}
		return nil, fmt.Errorf("script_store: get: %w", err)
	}
	if len(tagsRaw) > 0 {
		if err := json.Unmarshal(tagsRaw, &s.Tags); err != nil {
			return nil, fmt.Errorf("script_store: unmarshal tags: %w", err)
		}
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return s, nil
}

// ListScripts returns a filtered, paginated slice plus the total count.
// Soft-deleted rows are excluded.
func (p *pgScriptStore) ListScripts(ctx context.Context, f ScriptListFilter) ([]models.ScriptDefinition, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("script_store: nil pool")
	}
	args := make([]any, 0, 6)
	where := make([]string, 0, 5)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	where = append(where, "deleted_at IS NULL")
	if f.Runtime != "" {
		add("runtime = $%d", f.Runtime)
	}
	if f.Enabled != nil {
		add("enabled = $%d", *f.Enabled)
	}
	if f.Tag != "" {
		args = append(args, f.Tag)
		where = append(where, fmt.Sprintf("tags @> $%d", len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d)", len(args)))
	}
	whereSQL := "WHERE " + joinAnd(where)

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM script_definitions "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("script_store: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       runtime, COALESCE(script_body,''),
		       COALESCE(timeout_seconds, 30), COALESCE(enabled, true),
		       COALESCE(tags, '[]'::jsonb),
		       created_at, updated_at
		FROM script_definitions
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("script_store: list: %w", err)
	}
	defer rows.Close()

	out := make([]models.ScriptDefinition, 0, f.Limit)
	for rows.Next() {
		var s models.ScriptDefinition
		var tagsRaw []byte
		if err := rows.Scan(
			&s.ID, &s.OrgID, &s.Name, &s.Description,
			&s.Runtime, &s.ScriptBody,
			&s.TimeoutSeconds, &s.Enabled, &tagsRaw,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("script_store: scan: %w", err)
		}
		if len(tagsRaw) > 0 {
			if err := json.Unmarshal(tagsRaw, &s.Tags); err != nil {
				return nil, 0, fmt.Errorf("script_store: unmarshal tags: %w", err)
			}
		}
		if s.Tags == nil {
			s.Tags = []string{}
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("script_store: rows err: %w", err)
	}
	return out, total, nil
}

// UpdateScript applies a partial update to a script definition. Only the
// non-nil fields in the patch are persisted. The updated_at column is
// always bumped to NOW(). A non-nil empty Tags slice is treated as "clear
// all tags" (stored as []).
func (p *pgScriptStore) UpdateScript(ctx context.Context, id string, patch ScriptPatch) (*models.ScriptDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("script_store: nil pool")
	}
	args := make([]any, 0, 6)
	sets := make([]string, 0, 6)
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
	if patch.ScriptBody != nil {
		add("script_body", *patch.ScriptBody)
	}
	if patch.Runtime != nil {
		add("runtime", *patch.Runtime)
	}
	if patch.TimeoutSeconds != nil {
		add("timeout_seconds", *patch.TimeoutSeconds)
	}
	if patch.Enabled != nil {
		add("enabled", *patch.Enabled)
	}
	if patch.Tags != nil {
		tagsJSON, err := json.Marshal(patch.Tags)
		if err != nil {
			return nil, fmt.Errorf("script_store: marshal tags: %w", err)
		}
		add("tags", tagsJSON)
	}
	if len(sets) == 0 {
		// Nothing to update — return the current row.
		return p.GetScript(ctx, id)
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE script_definitions SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id
	`, joinAnd(sets), len(args))

	var newID string
	if err := p.pool.QueryRow(ctx, q, args...).Scan(&newID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScriptNotFound
		}
		return nil, fmt.Errorf("script_store: update: %w", err)
	}
	return p.GetScript(ctx, id)
}

// DeleteScript soft-deletes a script definition by setting deleted_at.
func (p *pgScriptStore) DeleteScript(ctx context.Context, id string) error {
	if p.pool == nil {
		return errors.New("script_store: nil pool")
	}
	const q = `
		UPDATE script_definitions
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id
	`
	var returnedID string
	if err := p.pool.QueryRow(ctx, q, id).Scan(&returnedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrScriptNotFound
		}
		return fmt.Errorf("script_store: delete: %w", err)
	}
	return nil
}

// InsertScriptRun creates a new script run record. Returns the persisted
// row (with DB-populated timestamps).
func (p *pgScriptStore) InsertScriptRun(ctx context.Context, run *models.ScriptRun) error {
	if p.pool == nil {
		return errors.New("script_store: nil pool")
	}
	const q = `
		INSERT INTO script_runs (
			id, script_id, agent_id, status, started_at, finished_at,
			exit_code, stdout, stderr, triggered_by, scheduled,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, COALESCE(NULLIF($10,''), ''), $11,
			COALESCE($12, NOW()), COALESCE($12, NOW())
		)
		RETURNING created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q,
		run.ID, run.ScriptID, run.AgentID, run.Status, run.StartedAt, run.FinishedAt,
		run.ExitCode, run.Stdout, run.Stderr, run.TriggeredBy, run.Scheduled,
		run.CreatedAt,
	)
	if err := row.Scan(&run.CreatedAt, &run.UpdatedAt); err != nil {
		return fmt.Errorf("script_store: insert run: %w", err)
	}
	return nil
}

// GetScriptRun returns one script run by id.
func (p *pgScriptStore) GetScriptRun(ctx context.Context, id string) (*models.ScriptRun, error) {
	if p.pool == nil {
		return nil, errors.New("script_store: nil pool")
	}
	const q = `
		SELECT id, script_id, COALESCE(agent_id,''),
		       COALESCE(status,'pending'),
		       COALESCE(started_at, 'epoch'::timestamptz),
		       finished_at,
		       exit_code,
		       COALESCE(stdout,''), COALESCE(stderr,''),
		       COALESCE(triggered_by,''), COALESCE(scheduled, false),
		       created_at, updated_at
		FROM script_runs
		WHERE id = $1
		LIMIT 1
	`
	run := &models.ScriptRun{}
	err := p.pool.QueryRow(ctx, q, id).Scan(
		&run.ID, &run.ScriptID, &run.AgentID,
		&run.Status, &run.StartedAt, &run.FinishedAt,
		&run.ExitCode, &run.Stdout, &run.Stderr,
		&run.TriggeredBy, &run.Scheduled,
		&run.CreatedAt, &run.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScriptRunNotFound
		}
		return nil, fmt.Errorf("script_store: get run: %w", err)
	}
	return run, nil
}

// ListScriptRuns returns a filtered, paginated slice of script runs plus
// the total count. Pass ScriptID and/or AgentID and/or Status to filter.
func (p *pgScriptStore) ListScriptRuns(ctx context.Context, f ScriptRunListFilter) ([]models.ScriptRun, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("script_store: nil pool")
	}
	args := make([]any, 0, 4)
	where := make([]string, 0, 3)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.ScriptID != "" {
		add("script_id = $%d", f.ScriptID)
	}
	if f.AgentID != "" {
		add("agent_id = $%d", f.AgentID)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAnd(where)
	}

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM script_runs "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("script_store: count runs: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, script_id, COALESCE(agent_id,''),
		       COALESCE(status,'pending'),
		       COALESCE(started_at, 'epoch'::timestamptz),
		       finished_at,
		       exit_code,
		       COALESCE(stdout,''), COALESCE(stderr,''),
		       COALESCE(triggered_by,''), COALESCE(scheduled, false),
		       created_at, updated_at
		FROM script_runs
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("script_store: list runs: %w", err)
	}
	defer rows.Close()

	out := make([]models.ScriptRun, 0, f.Limit)
	for rows.Next() {
		var run models.ScriptRun
		if err := rows.Scan(
			&run.ID, &run.ScriptID, &run.AgentID,
			&run.Status, &run.StartedAt, &run.FinishedAt,
			&run.ExitCode, &run.Stdout, &run.Stderr,
			&run.TriggeredBy, &run.Scheduled,
			&run.CreatedAt, &run.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("script_store: scan run: %w", err)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("script_store: rows err: %w", err)
	}
	return out, total, nil
}

// UpdateScriptRunOutput updates the output fields of a script run
// (status, finished_at, exit_code, stdout, stderr).
func (p *pgScriptStore) UpdateScriptRunOutput(ctx context.Context, run *models.ScriptRun) error {
	if p.pool == nil {
		return errors.New("script_store: nil pool")
	}
	const q = `
		UPDATE script_runs
		SET status = $2,
		    finished_at = $3,
		    exit_code = $4,
		    stdout = $5,
		    stderr = $6,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`
	if err := p.pool.QueryRow(ctx, q,
		run.ID, run.Status, run.FinishedAt,
		run.ExitCode, run.Stdout, run.Stderr,
	).Scan(&run.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrScriptRunNotFound
		}
		return fmt.Errorf("script_store: update run output: %w", err)
	}
	return nil
}
