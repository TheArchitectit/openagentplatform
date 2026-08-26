// Package scheduled - store.go implements PostgreSQL persistence for
// automated tasks. CRUD methods are in store_tasks.go; the row scanners
// are in store_scan.go.
package scheduled

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("scheduled: not found")

// Store defines the persistence interface for automated tasks. Both the
// scheduler (for loading due tasks and persisting next_run_at) and the API
// (for CRUD) depend on this interface.
type Store interface {
	CreateTask(ctx context.Context, task *TaskRecord) error
	GetTask(ctx context.Context, orgID, id string) (*TaskRecord, error)
	UpdateTask(ctx context.Context, task *TaskRecord) error
	DeleteTask(ctx context.Context, orgID, id string) error
	ListTasks(ctx context.Context, orgID string) ([]*TaskRecord, error)
	ListDueTasks(ctx context.Context, now time.Time) ([]*TaskRecord, error)
	MarkRun(ctx context.Context, id string, runAt time.Time, status string, nextRun *time.Time) error
}

// PGStore implements Store against PostgreSQL.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore builds a PGStore backed by pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// TaskRecord is the persisted representation of an automated task.
type TaskRecord struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"org_id"`
	Name       string    `json:"name"`
	Enabled    bool      `json:"enabled"`
	CronExpr   string    `json:"cron_expr"`
	Action     string    `json:"action"`
	Params     []byte    `json:"params,omitempty"`
	Timezone   string    `json:"timezone"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastStatus string    `json:"last_status,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
