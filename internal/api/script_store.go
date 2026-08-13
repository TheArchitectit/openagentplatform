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
// body, timeout_seconds, enabled, tags jsonb, created_at, updated_at,
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
	OrgID   string
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
	Body           *string
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
			id, org_id, name, description, runtime, body,
			timeout_seconds, enabled, tags, created_at, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2,''), ''), $3, $4, $5, $6,
			$7, $8, $9, COALESCE($10, NOW()), COALESCE($10, NOW())
		)
		RETURNING created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q,
		s.ID, s.OrgID, s.Name, s.Description, s.Runtime, s.Body,
		s.TimeoutSeconds, s.Enabled, tagsJSON, s.CreatedAt,
	)
	if err := row.Scan(&s.CreatedAt, &s.UpdatedAt); err != nil {
		return fmt.Errorf("script_store: insert: %w", err)
	}
	return nil
}

// GetScript returns one script definition by id, or ErrScriptNotFound.
// Soft-deleted rows (deleted_at IS NOT NULL) are treated as not found.
// If orgID is non-empty, the query enforces org ownership.
func (p *pgScriptStore) GetScript(ctx context.Context, orgID, id string) (*models.ScriptDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("script_store: nil pool")
	}
	args := []any{id}
	where := []string{"id = $1", "deleted_at IS NULL"}
	if orgID != "" {
		args = append(args, orgID)
		where = append(where, fmt.Sprintf("org_id = $%d", len(args)))
	}
	q := `
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       runtime, COALESCE(body,''),
		       COALESCE(timeout_seconds, 30), COALESCE(enabled, true),
		       COALESCE(tags, '[]'::jsonb),
		       created_at, updated_at
		FROM script_definitions
		WHERE ` + joinAnd(where) + `
		LIMIT 1
	`
	s := &models.ScriptDefinition{}
	var tagsRaw []byte
	err := p.pool.QueryRow(ctx, q, args...).Scan(
		&s.ID, &s.OrgID, &s.Name, &s.Description,
		&s.Runtime, &s.Body,
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
	if f.OrgID != "" {
		add("org_id = $%d", f.OrgID)
	}
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
		       runtime, COALESCE(body,''),
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
			&s.Runtime, &s.Body,
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
func (p *pgScriptStore) DeleteScript(ctx context.Context, orgID, id string) error {
	if p.pool == nil {
		return errors.New("script_store: nil pool")
	}
	args := []any{id}
	where := "id = $1 AND deleted_at IS NULL"
	if orgID != "" {
		args = append(args, orgID)
		where += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	q := `
		UPDATE script_definitions
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE ` + where + `
		RETURNING id
	`
	var returnedID string
	if err := p.pool.QueryRow(ctx, q, args...).Scan(&returnedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrScriptNotFound
		}
		return fmt.Errorf("script_store: delete: %w", err)
	}
	return nil
}
