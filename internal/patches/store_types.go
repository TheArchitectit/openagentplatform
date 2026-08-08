package patches

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// PatchJobFilter is the filter set for ListPatchJobs. Zero-valued fields
// are ignored. TimeRange is an inclusive [from, to] window on created_at.

type PatchJobFilter struct {
	State    string
	Severity string
	OrgID    string
	AgentID  string
	From     time.Time
	To       time.Time
	Limit    int
	Offset   int
}

// Store is the full persistence interface for patch jobs, targets,
// and approval records. The workflow engine and HTTP handlers use
// this interface; pgPatchStore is the default implementation.
type Store interface {
	CreatePatchJob(ctx context.Context, job *models.PatchJob) error
	GetPatchJob(ctx context.Context, orgID, id string) (*models.PatchJob, error)
	ListPatchJobs(ctx context.Context, f PatchJobFilter) ([]models.PatchJob, int, error)
	UpdatePatchJob(ctx context.Context, job *models.PatchJob) error
	DeletePatchJob(ctx context.Context, orgID, id string) error

	InsertApprovalRecord(ctx context.Context, rec *models.ApprovalRecord) error
	GetApprovalHistory(ctx context.Context, jobID string) ([]models.ApprovalRecord, error)

	InsertPatchJobTarget(ctx context.Context, t *models.PatchJobTarget) error
	GetPatchJobTargets(ctx context.Context, jobID string) ([]models.PatchJobTarget, error)
	UpdatePatchJobTarget(ctx context.Context, t *models.PatchJobTarget) error

	GetPatchStats(ctx context.Context, orgID string) (*models.PatchStats, error)
}

// pgPatchStore is the default PostgreSQL-backed implementation of Store.
type pgPatchStore struct {
	pool *pgxpool.Pool
}

// NewPGStore constructs a Store backed by a pgx connection pool.
func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgPatchStore{pool: pool}
}

// CreatePatchJob inserts a new patch job along with its targets and
// approval records (if any). Uses a transaction so partial writes
// are not visible.
