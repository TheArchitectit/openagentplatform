package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

func (s *pgStore) InsertTask(ctx context.Context, t *models.Task) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if t.ID == "" {
		return errors.New("a2a: task ID required")
	}

	messagesJSON, err := jsonOrNull(t.Message)
	if err != nil {
		return fmt.Errorf("a2a: marshal message: %w", err)
	}
	// If the task has multiple messages, we store them as a JSON array.
	// For backward compat, if Message is zero-valued, we store an empty array.
	if t.Message.ID == "" && len(t.Message.Parts) == 0 {
		messagesJSON = []byte(`[]`)
	}

	metaJSON, err := jsonOrNull(t.Metadata)
	if err != nil {
		return fmt.Errorf("a2a: marshal metadata: %w", err)
	}

	const q = `
		INSERT INTO a2a_tasks (
			id, session_id, status, messages, metadata, agent_card_url,
			version, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9
		)
	`
	_, err = s.pool.Exec(ctx, q,
		t.ID, t.ContextID, t.Status, messagesJSON, metaJSON, t.AgentID,
		t.Version, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("a2a: insert task: %w", err)
	}
	return nil
}

// GetTask fetches a single task by id, including its artifacts.
// Returns ErrTaskNotFound if the id does not exist.
func (s *pgStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	const q = `
		SELECT id, COALESCE(session_id,''), COALESCE(status,'pending'),
		       messages, metadata, COALESCE(agent_card_url,''),
		       version, created_at, updated_at
		FROM a2a_tasks
		WHERE id = $1
		LIMIT 1
	`
	var t models.Task
	var messagesRaw []byte
	var metaRaw []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&t.ID, &t.ContextID, &t.Status,
		&messagesRaw, &metaRaw, &t.AgentID,
		&t.Version, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("a2a: get task: %w", err)
	}

	// Deserialize messages (JSONB array)
	if len(messagesRaw) > 0 {
		var msgs []models.Message
		if err := json.Unmarshal(messagesRaw, &msgs); err == nil && len(msgs) > 0 {
			// Store the latest message in the Task.Message field
			t.Message = msgs[len(msgs)-1]
		}
	}

	// Deserialize metadata
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &t.Metadata)
	}

	// Load artifacts
	artifacts, err := s.ListArtifacts(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("a2a: load artifacts: %w", err)
	}
	t.Artifacts = artifacts

	return &t, nil
}

// ListTasks returns a filtered list of tasks plus the total matching count.
// Filters are applied additively. Results are ordered by created_at DESC.
func (s *pgStore) ListTasks(ctx context.Context, f TaskFilter) ([]models.Task, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("a2a: nil pool")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	args := make([]any, 0, 4)
	where := make([]string, 0, 3)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.SessionID != "" {
		add("session_id = $%d", f.SessionID)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.AgentCardURL != "" {
		add("agent_card_url = $%d", f.AgentCardURL)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAnd(where)
	}

	// Count
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM a2a_tasks "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("a2a: count tasks: %w", err)
	}

	// Fetch
	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(session_id,''), COALESCE(status,'pending'),
		       metadata, COALESCE(agent_card_url,''),
		       version, created_at, updated_at
		FROM a2a_tasks
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("a2a: list tasks: %w", err)
	}
	defer rows.Close()

	out := make([]models.Task, 0, f.Limit)
	for rows.Next() {
		var t models.Task
		var metaRaw []byte
		if err := rows.Scan(
			&t.ID, &t.ContextID, &t.Status,
			&metaRaw, &t.AgentID,
			&t.Version, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("a2a: scan task: %w", err)
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &t.Metadata)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("a2a: rows err: %w", err)
	}
	return out, total, nil
}

// UpdateTaskStatus updates the status and increments the version
// atomically. Uses optimistic concurrency: the UPDATE only succeeds if
// the current version matches the expected version. Returns
// ErrVersionMismatch if the version does not match (concurrent
// modification).
func (s *pgStore) UpdateTaskStatus(ctx context.Context, id string, status string, version int) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	const q = `
		UPDATE a2a_tasks SET
			status     = $2,
			version    = version + 1,
			updated_at = NOW()
		WHERE id = $1 AND version = $3
	`
	tag, err := s.pool.Exec(ctx, q, id, status, version)
	if err != nil {
		return fmt.Errorf("a2a: update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Could be not-found or version-mismatch; check which
		var exists bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM a2a_tasks WHERE id = $1)", id).Scan(&exists); err != nil {
			return fmt.Errorf("a2a: check exists: %w", err)
		}
		if !exists {
			return ErrTaskNotFound
		}
		return ErrVersionMismatch
	}
	return nil
}

// UpdateTask updates the full mutable state of a task (metadata,
// session_id, agent_card_url). Uses optimistic concurrency via the
// version column. The caller must set the expected version on t.Version
// before calling. On success, version is incremented by the database.
func (s *pgStore) UpdateTask(ctx context.Context, t *models.Task) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if t.ID == "" {
		return errors.New("a2a: task ID required")
	}

	metaJSON, err := jsonOrNull(t.Metadata)
	if err != nil {
		return fmt.Errorf("a2a: marshal metadata: %w", err)
	}

	const q = `
		UPDATE a2a_tasks SET
			session_id     = $2,
			metadata       = $3,
			agent_card_url = $4,
			version        = version + 1,
			updated_at     = NOW()
		WHERE id = $1 AND version = $5
	`
	tag, err := s.pool.Exec(ctx, q, t.ID, t.ContextID, metaJSON, t.AgentID, t.Version)
	if err != nil {
		return fmt.Errorf("a2a: update task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionMismatch
	}
	return nil
}

// DeleteTask removes a task by id. Artifacts are removed via CASCADE.
func (s *pgStore) DeleteTask(ctx context.Context, id string) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	const q = `DELETE FROM a2a_tasks WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("a2a: delete task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// ============================================================
// Message operations
// ============================================================

// AddMessage appends a message to the task's messages JSONB array and
// increments the version. Uses optimistic concurrency.
