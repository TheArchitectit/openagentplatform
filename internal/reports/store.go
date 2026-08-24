// Package reports - store.go implements PostgreSQL persistence for
// report templates, report runs, and report schedules. The PGStore CRUD
// methods are split across store_templates.go, store_runs.go, and
// store_schedules.go; the row scanners live in store_scan.go.
package reports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("report: not found")

// ReportTemplate defines a reusable report configuration that an org
// can customise and schedule.
type ReportTemplate struct {
	ID         string          `json:"id"`
	OrgID      string          `json:"org_id"`
	Name       string          `json:"name"`
	TemplateID string          `json:"template_id"` // one of the 7 built-in types
	Format     ReportFormat    `json:"format"`
	Params     json.RawMessage `json:"params"` // template-specific parameters
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ReportRun is a single execution of a report (manual or scheduled).
type ReportRun struct {
	ID             string          `json:"id"`
	OrgID          string          `json:"org_id"`
	TemplateID     string          `json:"template_id"`
	Title          string          `json:"title"`
	Status         string          `json:"status"` // "running", "completed", "failed"
	Format         ReportFormat    `json:"format"`
	Data           json.RawMessage `json:"data,omitempty"`
	DeliveryStatus DeliveryStatus  `json:"delivery_status"`
	DeliveryTarget string          `json:"delivery_target,omitempty"` // email addr, webhook URL, or empty
	ErrorMessage   string          `json:"error_message,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	DurationMs     int64           `json:"duration_ms"`
}

// ReportSchedule defines a cron-based recurring report.
type ReportSchedule struct {
	ID             string          `json:"id"`
	OrgID          string          `json:"org_id"`
	TemplateID     string          `json:"template_id"`
	CronExpr       string          `json:"cron_expr"`
	Format         ReportFormat    `json:"format"`
	Params         json.RawMessage `json:"params"`
	DeliveryMethod string          `json:"delivery_method"` // "email", "webhook", "download"
	DeliveryTarget string          `json:"delivery_target,omitempty"`
	Enabled        bool            `json:"enabled"`
	LastRunAt      *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time      `json:"next_run_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Store is the persistence interface for report data.
type Store interface {
	// Templates
	CreateTemplate(ctx context.Context, t *ReportTemplate) error
	GetTemplate(ctx context.Context, orgID, id string) (*ReportTemplate, error)
	ListTemplates(ctx context.Context, orgID string) ([]*ReportTemplate, error)
	UpdateTemplate(ctx context.Context, orgID, id string, t *ReportTemplate) error
	DeleteTemplate(ctx context.Context, orgID, id string) error

	// Runs
	CreateRun(ctx context.Context, r *ReportRun) error
	GetRun(ctx context.Context, orgID, id string) (*ReportRun, error)
	ListRuns(ctx context.Context, orgID string, limit, offset int) ([]*ReportRun, error)
	UpdateRunStatus(ctx context.Context, id string, status string, deliveryStatus DeliveryStatus, errMsg string) error

	// Schedules
	CreateSchedule(ctx context.Context, s *ReportSchedule) error
	GetSchedule(ctx context.Context, orgID, id string) (*ReportSchedule, error)
	ListSchedules(ctx context.Context, orgID string) ([]*ReportSchedule, error)
	// ListDueSchedules returns enabled schedules whose NextRunAt is in
	// the past, across all orgs. Used by the Scheduler tick.
	ListDueSchedules(ctx context.Context, now time.Time) ([]*ReportSchedule, error)
	UpdateSchedule(ctx context.Context, orgID, id string, s *ReportSchedule) error
	DeleteSchedule(ctx context.Context, orgID, id string) error
}

// PGStore is the PostgreSQL-backed implementation of Store.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore returns a new PGStore.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// EnsureSchema creates the required tables if they do not exist.
func (s *PGStore) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS report_templates (
			id          TEXT PRIMARY KEY,
			org_id      TEXT NOT NULL,
			name        TEXT NOT NULL,
			template_id TEXT NOT NULL,
			format      TEXT NOT NULL DEFAULT 'json',
			params      JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_report_templates_org ON report_templates(org_id)`,
		`CREATE TABLE IF NOT EXISTS report_runs (
			id              TEXT PRIMARY KEY,
			org_id          TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			title           TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'running',
			format          TEXT NOT NULL DEFAULT 'json',
			data            JSONB,
			delivery_status TEXT NOT NULL DEFAULT 'pending',
			delivery_target TEXT NOT NULL DEFAULT '',
			error_message   TEXT NOT NULL DEFAULT '',
			started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at    TIMESTAMPTZ,
			duration_ms     BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_report_runs_org ON report_runs(org_id, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS report_schedules (
			id              TEXT PRIMARY KEY,
			org_id          TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			cron_expr       TEXT NOT NULL,
			format          TEXT NOT NULL DEFAULT 'json',
			params          JSONB NOT NULL DEFAULT '{}'::jsonb,
			delivery_method TEXT NOT NULL DEFAULT 'download',
			delivery_target TEXT NOT NULL DEFAULT '',
			enabled         BOOLEAN NOT NULL DEFAULT TRUE,
			last_run_at     TIMESTAMPTZ,
			next_run_at     TIMESTAMPTZ,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_report_schedules_org ON report_schedules(org_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}
