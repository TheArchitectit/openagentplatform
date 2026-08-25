package patches

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	// Per-KB WinUpdate state (RMM-03). These methods are independent
	// of patch jobs: they are fed by agent scan/install/reboot reports.
	IngestKBScan(ctx context.Context, orgID, agentID, kb, severity string) (string, error)
	IngestKBInstall(ctx context.Context, orgID, agentID, kb string, success, rebootRequired bool, errMsg string) (string, error)
	IngestKBRebootDone(ctx context.Context, orgID, agentID string, kbs []string) error
	TransitionKB(ctx context.Context, orgID, agentID, kb, event string) (string, error)
	GetKBStatesByAgent(ctx context.Context, orgID, agentID string) ([]models.WinUpdateKBState, error)

	// CVE enrichment (RMM-05).
	UpsertCVEEnrichment(ctx context.Context, cve *models.CVEEnrichment) error
	GetCVEEnrichment(ctx context.Context, cveID string) (*models.CVEEnrichment, error)
	ListCVEEnrichments(ctx context.Context, limit int) ([]models.CVEEnrichment, error)
	PatchCatalogUpdateCVEIDs(ctx context.Context, orgID, kb string, cveIDs []string) error
	PatchCatalogUpdateCVSS(ctx context.Context, orgID, kb string, cvssScore *float64) error
	LookupCVEsByKB(ctx context.Context, orgID, kb string) ([]models.CVEEnrichment, error)
	LookupKBsByCVE(ctx context.Context, orgID, cveID string) ([]CVEKBMatch, error)
}

// CVEKBMatch represents a CVE→KB correlation result.
type CVEKBMatch struct {
	KB        string   `json:"kb"`
	Title     string   `json:"title,omitempty"`
	Severity  string   `json:"severity,omitempty"`
	CVEIDs    []string `json:"cve_ids"`
	CvssScore *float64 `json:"cvss_score,omitempty"`
}

// patchPoolConn is the minimal pgx surface used by pgPatchStore. It is
// satisfied by *pgxpool.Pool in production and by pgxmock pools in tests.
type patchPoolConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// pgPatchStore is the default PostgreSQL-backed implementation of Store.
type pgPatchStore struct {
	pool patchPoolConn
}

// NewPGStore constructs a Store backed by a pgx connection pool.
func NewPGStore(pool patchPoolConn) Store {
	return &pgPatchStore{pool: pool}
}

// CreatePatchJob inserts a new patch job along with its targets and
// approval records (if any). Uses a transaction so partial writes
// are not visible.
