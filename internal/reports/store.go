// Package reports - store.go implements PostgreSQL persistence for
// report templates, report runs, and report schedules.
package reports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("report: not found")

// ReportTemplate defines a reusable report configuration that an org
// can customise and schedule.
type ReportTemplate struct {
	ID          string          `json:"id"`
	OrgID       string          `json:"org_id"`
	Name        string          `json:"name"`
	TemplateID  string          `json:"template_id"`  // one of the 7 built-in types
	Format      ReportFormat    `json:"format"`
	Params      json.RawMessage `json:"params"`       // template-specific parameters
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
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

// --- Templates ---

func (s *PGStore) CreateTemplate(ctx context.Context, t *ReportTemplate) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.pool.Exec(ctx,
		`INSERT INTO report_templates (id, org_id, name, template_id, format, params, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.OrgID, t.Name, t.TemplateID, string(t.Format), t.Params, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *PGStore) GetTemplate(ctx context.Context, orgID, id string) (*ReportTemplate, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, org_id, name, template_id, format, params, created_at, updated_at
		 FROM report_templates WHERE org_id=$1 AND id=$2`, orgID, id)
	return scanTemplate(row)
}

func (s *PGStore) ListTemplates(ctx context.Context, orgID string) ([]*ReportTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, org_id, name, template_id, format, params, created_at, updated_at
		 FROM report_templates WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReportTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PGStore) UpdateTemplate(ctx context.Context, orgID, id string, t *ReportTemplate) error {
	t.UpdatedAt = time.Now().UTC()
	tag, err := s.pool.Exec(ctx,
		`UPDATE report_templates SET name=$3, template_id=$4, format=$5, params=$6, updated_at=$7
		 WHERE org_id=$1 AND id=$2`,
		orgID, id, t.Name, t.TemplateID, string(t.Format), t.Params, t.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGStore) DeleteTemplate(ctx context.Context, orgID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM report_templates WHERE org_id=$1 AND id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Runs ---

func (s *PGStore) CreateRun(ctx context.Context, r *ReportRun) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO report_runs (id, org_id, template_id, title, status, format, data,
			delivery_status, delivery_target, error_message, started_at, completed_at, duration_ms)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		r.ID, r.OrgID, r.TemplateID, r.Title, r.Status, string(r.Format), r.Data,
		string(r.DeliveryStatus), r.DeliveryTarget, r.ErrorMessage, r.StartedAt, r.CompletedAt, r.DurationMs,
	)
	return err
}

func (s *PGStore) GetRun(ctx context.Context, orgID, id string) (*ReportRun, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, org_id, template_id, title, status, format, data, delivery_status,
			delivery_target, error_message, started_at, completed_at, duration_ms
		 FROM report_runs WHERE org_id=$1 AND id=$2`, orgID, id)
	return scanRun(row)
}

func (s *PGStore) ListRuns(ctx context.Context, orgID string, limit, offset int) ([]*ReportRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, org_id, template_id, title, status, format, data, delivery_status,
			delivery_target, error_message, started_at, completed_at, duration_ms
		 FROM report_runs WHERE org_id=$1 ORDER BY started_at DESC LIMIT $2 OFFSET $3`,
		orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReportRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PGStore) UpdateRunStatus(ctx context.Context, id string, status string, deliveryStatus DeliveryStatus, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE report_runs SET status=$2, delivery_status=$3, error_message=$4, completed_at=$5
		 WHERE id=$1`,
		id, status, string(deliveryStatus), errMsg, now)
	return err
}

// --- Schedules ---

func (s *PGStore) CreateSchedule(ctx context.Context, sched *ReportSchedule) error {
	if sched.ID == "" {
		sched.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	sched.CreatedAt = now
	sched.UpdatedAt = now
	_, err := s.pool.Exec(ctx,
		`INSERT INTO report_schedules (id, org_id, template_id, cron_expr, format, params,
			delivery_method, delivery_target, enabled, last_run_at, next_run_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		sched.ID, sched.OrgID, sched.TemplateID, sched.CronExpr, string(sched.Format), sched.Params,
		sched.DeliveryMethod, sched.DeliveryTarget, sched.Enabled, sched.LastRunAt, sched.NextRunAt,
		sched.CreatedAt, sched.UpdatedAt,
	)
	return err
}

func (s *PGStore) GetSchedule(ctx context.Context, orgID, id string) (*ReportSchedule, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, org_id, template_id, cron_expr, format, params, delivery_method,
			delivery_target, enabled, last_run_at, next_run_at, created_at, updated_at
		 FROM report_schedules WHERE org_id=$1 AND id=$2`, orgID, id)
	return scanSchedule(row)
}

func (s *PGStore) ListSchedules(ctx context.Context, orgID string) ([]*ReportSchedule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, org_id, template_id, cron_expr, format, params, delivery_method,
			delivery_target, enabled, last_run_at, next_run_at, created_at, updated_at
		 FROM report_schedules WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReportSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *PGStore) UpdateSchedule(ctx context.Context, orgID, id string, sched *ReportSchedule) error {
	sched.UpdatedAt = time.Now().UTC()
	tag, err := s.pool.Exec(ctx,
		`UPDATE report_schedules SET template_id=$3, cron_expr=$4, format=$5, params=$6,
			delivery_method=$7, delivery_target=$8, enabled=$9, updated_at=$10
		 WHERE org_id=$1 AND id=$2`,
		orgID, id, sched.TemplateID, sched.CronExpr, string(sched.Format), sched.Params,
		sched.DeliveryMethod, sched.DeliveryTarget, sched.Enabled, sched.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGStore) DeleteSchedule(ctx context.Context, orgID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM report_schedules WHERE org_id=$1 AND id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- scan helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTemplate(row rowScanner) (*ReportTemplate, error) {
	var t ReportTemplate
	var format string
	err := row.Scan(&t.ID, &t.OrgID, &t.Name, &t.TemplateID, &format, &t.Params, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.Format = ReportFormat(format)
	return &t, nil
}

func scanRun(row rowScanner) (*ReportRun, error) {
	var r ReportRun
	var format, deliveryStatus string
	err := row.Scan(&r.ID, &r.OrgID, &r.TemplateID, &r.Title, &r.Status, &format, &r.Data,
		&deliveryStatus, &r.DeliveryTarget, &r.ErrorMessage, &r.StartedAt, &r.CompletedAt, &r.DurationMs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.Format = ReportFormat(format)
	r.DeliveryStatus = DeliveryStatus(deliveryStatus)
	return &r, nil
}

func scanSchedule(row rowScanner) (*ReportSchedule, error) {
	var s ReportSchedule
	var format string
	err := row.Scan(&s.ID, &s.OrgID, &s.TemplateID, &s.CronExpr, &format, &s.Params,
		&s.DeliveryMethod, &s.DeliveryTarget, &s.Enabled, &s.LastRunAt, &s.NextRunAt,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Format = ReportFormat(format)
	return &s, nil
}
