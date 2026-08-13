package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
