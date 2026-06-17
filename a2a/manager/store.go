// Package manager - store.go implements the PostgreSQL persistence layer
// for A2A tasks, their messages, and artifacts. All queries use
// parameterized statements via pgx to prevent SQL injection.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// SQL schema (DDL)
// ============================================================

// Schema returns the DDL statements for the a2a_tasks and a2a_artifacts
// tables. Callers should execute these during database initialization.
// The schema uses UUID primary keys for tasks and TEXT composite keys
// (id, task_id) for artifacts.
const Schema = `
CREATE TABLE IF NOT EXISTS a2a_tasks (
	id            UUID         PRIMARY KEY,
	session_id    TEXT         NOT NULL DEFAULT '',
	status        TEXT         NOT NULL DEFAULT 'pending',
	messages      JSONB        NOT NULL DEFAULT '[]'::jsonb,
	metadata      JSONB        NOT NULL DEFAULT '{}'::jsonb,
	agent_card_url TEXT        NOT NULL DEFAULT '',
	version       INTEGER      NOT NULL DEFAULT 1,
	created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_session_id ON a2a_tasks (session_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_status     ON a2a_tasks (status);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_agent_card ON a2a_tasks (agent_card_url);

CREATE TABLE IF NOT EXISTS a2a_artifacts (
	id         TEXT         NOT NULL,
	task_id    UUID         NOT NULL REFERENCES a2a_tasks(id) ON DELETE CASCADE,
	parts      JSONB        NOT NULL DEFAULT '[]'::jsonb,
	metadata   JSONB        NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	PRIMARY KEY (id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_a2a_artifacts_task_id ON a2a_artifacts (task_id);
`

// ============================================================
// Store interface
// ============================================================

// TaskFilter is the filter set for ListTasks. Zero-valued fields are ignored.
type TaskFilter struct {
	SessionID    string
	Status       string
	AgentCardURL string
	Limit        int
	Offset       int
}

// Store is the full persistence interface for A2A tasks and artifacts.
type Store interface {
	// Task CRUD
	InsertTask(ctx context.Context, t *models.Task) error
	GetTask(ctx context.Context, id string) (*models.Task, error)
	ListTasks(ctx context.Context, f TaskFilter) ([]models.Task, int, error)
	UpdateTaskStatus(ctx context.Context, id string, status string, version int) error
	UpdateTask(ctx context.Context, t *models.Task) error
	DeleteTask(ctx context.Context, id string) error

	// Message operations
	AddMessage(ctx context.Context, taskID string, msg models.Message, version int) error
	GetMessages(ctx context.Context, taskID string) ([]models.Message, error)

	// Artifact operations
	InsertArtifact(ctx context.Context, a *models.Artifact) error
	GetArtifact(ctx context.Context, id string, taskID string) (*models.Artifact, error)
	ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error)
	DeleteArtifact(ctx context.Context, id string, taskID string) error
}

// ============================================================
// PostgreSQL implementation
// ============================================================

// pgStore is the default PostgreSQL-backed implementation of Store.
type pgStore struct {
	pool *pgxpool.Pool
}

// NewPGStore constructs a Store backed by a pgx connection pool.
func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

// InsertTask inserts a new task. The task's ID, timestamps, and version
// must be set by the caller. Returns an error if the ID already exists.
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
func (s *pgStore) AddMessage(ctx context.Context, taskID string, msg models.Message, version int) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if taskID == "" {
		return errors.New("a2a: task ID required")
	}
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("a2a: invalid message: %w", err)
	}

	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("a2a: marshal message: %w", err)
	}

	const q = `
		UPDATE a2a_tasks SET
			messages   = messages || $2::jsonb,
			version    = version + 1,
			updated_at = NOW()
		WHERE id = $1 AND version = $3
	`
	tag, err := s.pool.Exec(ctx, q, taskID, msgJSON, version)
	if err != nil {
		return fmt.Errorf("a2a: add message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionMismatch
	}
	return nil
}

// GetMessages returns all messages for a task, ordered by insertion
// order (JSONB array order).
func (s *pgStore) GetMessages(ctx context.Context, taskID string) ([]models.Message, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	if taskID == "" {
		return nil, errors.New("a2a: task ID required")
	}
	const q = `SELECT messages FROM a2a_tasks WHERE id = $1 LIMIT 1`
	var raw []byte
	err := s.pool.QueryRow(ctx, q, taskID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("a2a: get messages: %w", err)
	}
	if len(raw) == 0 {
		return []models.Message{}, nil
	}
	var msgs []models.Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, fmt.Errorf("a2a: unmarshal messages: %w", err)
	}
	return msgs, nil
}

// ============================================================
// Artifact operations
// ============================================================

// InsertArtifact inserts a new artifact for a task. The artifact's
// ID, task_id, and created_at must be set by the caller.
func (s *pgStore) InsertArtifact(ctx context.Context, a *models.Artifact) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if a.ID == "" {
		return errors.New("a2a: artifact ID required")
	}
	if a.TaskID == "" {
		return errors.New("a2a: artifact task_id required")
	}

	partsJSON, err := json.Marshal(a.Parts)
	if err != nil {
		return fmt.Errorf("a2a: marshal parts: %w", err)
	}
	// The model Artifact does not have a top-level metadata field;
	// artifacts carry metadata via their parts or description. We store
	// a minimal JSON with name and description.
	metaMap := map[string]string{
		"name":        a.Name,
		"description": a.Description,
		"mime_type":   a.MimeType,
	}
	metaJSON, err := json.Marshal(metaMap)
	if err != nil {
		return fmt.Errorf("a2a: marshal artifact metadata: %w", err)
	}

	const q = `
		INSERT INTO a2a_artifacts (id, task_id, parts, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = s.pool.Exec(ctx, q, a.ID, a.TaskID, partsJSON, metaJSON, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("a2a: insert artifact: %w", err)
	}
	return nil
}

// GetArtifact fetches a single artifact by (id, task_id).
// Returns ErrArtifactNotFound if not found.
func (s *pgStore) GetArtifact(ctx context.Context, id string, taskID string) (*models.Artifact, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	const q = `
		SELECT id, task_id, parts, metadata, created_at
		FROM a2a_artifacts
		WHERE id = $1 AND task_id = $2
		LIMIT 1
	`
	var a models.Artifact
	var partsRaw []byte
	var metaRaw []byte
	err := s.pool.QueryRow(ctx, q, id, taskID).Scan(
		&a.ID, &a.TaskID, &partsRaw, &metaRaw, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("a2a: get artifact: %w", err)
	}
	if len(partsRaw) > 0 {
		_ = json.Unmarshal(partsRaw, &a.Parts)
	}
	if len(metaRaw) > 0 {
		var m map[string]string
		_ = json.Unmarshal(metaRaw, &m)
		a.Name = m["name"]
		a.Description = m["description"]
		a.MimeType = m["mime_type"]
	}
	return &a, nil
}

// ListArtifacts returns all artifacts for a task, ordered by created_at ASC.
func (s *pgStore) ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	if taskID == "" {
		return nil, errors.New("a2a: task ID required")
	}
	const q = `
		SELECT id, task_id, parts, metadata, created_at
		FROM a2a_artifacts
		WHERE task_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("a2a: list artifacts: %w", err)
	}
	defer rows.Close()
	out := make([]models.Artifact, 0, 4)
	for rows.Next() {
		var a models.Artifact
		var partsRaw []byte
		var metaRaw []byte
		if err := rows.Scan(&a.ID, &a.TaskID, &partsRaw, &metaRaw, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("a2a: scan artifact: %w", err)
		}
		if len(partsRaw) > 0 {
			_ = json.Unmarshal(partsRaw, &a.Parts)
		}
		if len(metaRaw) > 0 {
			var m map[string]string
			_ = json.Unmarshal(metaRaw, &m)
			a.Name = m["name"]
			a.Description = m["description"]
			a.MimeType = m["mime_type"]
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteArtifact removes an artifact by (id, task_id).
func (s *pgStore) DeleteArtifact(ctx context.Context, id string, taskID string) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	const q = `DELETE FROM a2a_artifacts WHERE id = $1 AND task_id = $2`
	tag, err := s.pool.Exec(ctx, q, id, taskID)
	if err != nil {
		return fmt.Errorf("a2a: delete artifact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrArtifactNotFound
	}
	return nil
}

// ============================================================
// Errors
// ============================================================

// ErrTaskNotFound is returned when a task id does not exist.
var ErrTaskNotFound = errors.New("a2a: task not found")

// ErrArtifactNotFound is returned when an artifact (id, task_id) does not exist.
var ErrArtifactNotFound = errors.New("a2a: artifact not found")

// ErrVersionMismatch is returned when an optimistic concurrency check
// fails (the stored version does not match the expected version).
var ErrVersionMismatch = errors.New("a2a: version mismatch (concurrent modification)")

// ============================================================
// Helpers
// ============================================================

// jsonOrNull marshals v to JSON, or returns nil if v is empty/nil.
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	// For maps, check if empty
	if m, ok := v.(map[string]string); ok && len(m) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(v)
}

// joinAnd joins SQL fragments with " AND ".
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
