package scheduled

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	createTaskSQL = `INSERT INTO automated_tasks
		(id, org_id, name, enabled, cron_expr, action, params, timezone,
		 next_run_at, last_run_at, last_status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`

	getTaskSQL = `SELECT id, org_id, name, enabled, cron_expr, action, params, timezone,
		next_run_at, last_run_at, last_status, created_at, updated_at
		FROM automated_tasks WHERE org_id=$1 AND id=$2`

	updateTaskSQL = `UPDATE automated_tasks SET name=$3, enabled=$4, cron_expr=$5,
		action=$6, params=$7, timezone=$8, updated_at=$9
		WHERE org_id=$1 AND id=$2`

	deleteTaskSQL   = `DELETE FROM automated_tasks WHERE org_id=$1 AND id=$2`
	listTasksSQL    = `SELECT id, org_id, name, enabled, cron_expr, action, params, timezone,
		next_run_at, last_run_at, last_status, created_at, updated_at
		FROM automated_tasks WHERE org_id=$1 ORDER BY created_at DESC`
	listDueSQL = `SELECT id, org_id, name, enabled, cron_expr, action, params, timezone,
		next_run_at, last_run_at, last_status, created_at, updated_at
		FROM automated_tasks WHERE enabled AND next_run_at <= $1`
	markRunSQL = `UPDATE automated_tasks SET last_run_at=$2, last_status=$3,
		next_run_at=$4, updated_at=$5 WHERE id=$1`
)

// CreateTask inserts a new automated task. It validates the cron expression
// (fail-closed) and computes the initial next_run_at.
func (s *PGStore) CreateTask(ctx context.Context, task *TaskRecord) error {
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.Timezone == "" {
		task.Timezone = "UTC"
	}
	if err := validateCron(task.CronExpr); err != nil {
		return fmt.Errorf("scheduled: invalid cron_expr %q: %w", task.CronExpr, err)
	}
	now := time.Now().UTC()
	next, err := computeNextRun(task.CronExpr, now)
	if err != nil {
		return fmt.Errorf("scheduled: compute next_run_at: %w", err)
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	task.NextRunAt = next
	_, err = s.pool.Exec(ctx, createTaskSQL,
		task.ID, task.OrgID, task.Name, task.Enabled, task.CronExpr, task.Action,
		task.Params, task.Timezone, task.NextRunAt, task.LastRunAt, task.LastStatus,
		task.CreatedAt, task.UpdatedAt)
	return err
}

// GetTask returns a single task by org + id.
func (s *PGStore) GetTask(ctx context.Context, orgID, id string) (*TaskRecord, error) {
	row := s.pool.QueryRow(ctx, getTaskSQL, orgID, id)
	return scanTask(row)
}

// UpdateTask patches a task's mutable fields and recomputes next_run_at if
// the cron_expr changed.
func (s *PGStore) UpdateTask(ctx context.Context, task *TaskRecord) error {
	if err := validateCron(task.CronExpr); err != nil {
		return fmt.Errorf("scheduled: invalid cron_expr %q: %w", task.CronExpr, err)
	}
	now := time.Now().UTC()
	task.UpdatedAt = now
	if task.NextRunAt == nil || true {
		next, err := computeNextRun(task.CronExpr, now)
		if err != nil {
			return fmt.Errorf("scheduled: compute next_run_at: %w", err)
		}
		task.NextRunAt = next
	}
	_, err := s.pool.Exec(ctx, updateTaskSQL,
		task.OrgID, task.ID, task.Name, task.Enabled, task.CronExpr, task.Action,
		task.Params, task.Timezone, task.UpdatedAt)
	return err
}

// DeleteTask removes a task.
func (s *PGStore) DeleteTask(ctx context.Context, orgID, id string) error {
	tag, err := s.pool.Exec(ctx, deleteTaskSQL, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTasks returns all tasks for an org.
func (s *PGStore) ListTasks(ctx context.Context, orgID string) ([]*TaskRecord, error) {
	rows, err := s.pool.Query(ctx, listTasksSQL, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TaskRecord
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListDueTasks returns enabled tasks whose next_run_at is at or before now.
func (s *PGStore) ListDueTasks(ctx context.Context, now time.Time) ([]*TaskRecord, error) {
	rows, err := s.pool.Query(ctx, listDueSQL, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TaskRecord
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkRun records a completed (or failed) run and advances next_run_at.
func (s *PGStore) MarkRun(ctx context.Context, id string, runAt time.Time, status string, nextRun *time.Time) error {
	_, err := s.pool.Exec(ctx, markRunSQL, id, runAt, status, nextRun, time.Now().UTC())
	return err
}

// scanTask decodes a row into a TaskRecord. It accepts any type that exposes
// Scan (sql.Row, pgx.Row, pgx.Rows).
func scanTask(row interface{ Scan(...any) error }) (*TaskRecord, error) {
	t := &TaskRecord{}
	var params []byte
	err := row.Scan(&t.ID, &t.OrgID, &t.Name, &t.Enabled, &t.CronExpr, &t.Action,
		&params, &t.Timezone, &t.NextRunAt, &t.LastRunAt, &t.LastStatus, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.Params = params
	return t, nil
}
