package manager

import (
	"context"

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
