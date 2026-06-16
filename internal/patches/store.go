// Package patches - store.go implements the PostgreSQL persistence
// layer for patch jobs, targets, and approval records.
package patches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
	GetPatchJob(ctx context.Context, id string) (*models.PatchJob, error)
	ListPatchJobs(ctx context.Context, f PatchJobFilter) ([]models.PatchJob, int, error)
	UpdatePatchJob(ctx context.Context, job *models.PatchJob) error
	DeletePatchJob(ctx context.Context, id string) error

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
func (s *pgPatchStore) CreatePatchJob(ctx context.Context, job *models.PatchJob) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if job.ID == "" {
		return errors.New("patches: job ID required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("patches: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO patch_jobs (
			id, org_id, title, description, severity, state, created_by,
			scheduled_at, maintenance_window_start, maintenance_window_end,
			approval_timeout, required_approvals, auto_approve_on_timeout,
			package_name, package_version, rollback_version,
			failure_reason, created_at, updated_at, completed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,
			$11,$12,$13,
			$14,$15,$16,
			$17,$18,$19,$20
		)
	`
	_, err = tx.Exec(ctx, q,
		job.ID, job.OrgID, job.Title, job.Description, job.Severity, job.State, job.CreatedBy,
		job.ScheduledAt, job.MaintenanceWindowStart, job.MaintenanceWindowEnd,
		job.ApprovalTimeout, job.RequiredApprovals, job.AutoApproveOnTimeout,
		job.PackageName, job.PackageVersion, job.RollbackVersion,
		job.FailureReason, job.CreatedAt, job.UpdatedAt, job.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("patches: insert job: %w", err)
	}

	for i := range job.Targets {
		t := &job.Targets[i]
		if t.ID == "" {
			t.ID = fmt.Sprintf("%s-t%d", job.ID, i)
		}
		t.PatchJobID = job.ID
		if err := s.insertTargetTx(ctx, tx, t); err != nil {
			return err
		}
	}

	for i := range job.Approvals {
		a := &job.Approvals[i]
		if a.ID == "" {
			a.ID = fmt.Sprintf("%s-a%d", job.ID, i)
		}
		a.PatchJobID = job.ID
		if err := s.insertApprovalTx(ctx, tx, a); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetPatchJob fetches a single patch job by id, including its
// targets and approval records. Returns ErrPatchJobNotFound when
// the id does not exist.
func (s *pgPatchStore) GetPatchJob(ctx context.Context, id string) (*models.PatchJob, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	const q = `
		SELECT id, COALESCE(org_id,''), COALESCE(title,''), COALESCE(description,''),
		       COALESCE(severity,'standard'), COALESCE(state,'pending_approval'),
		       COALESCE(created_by,''), scheduled_at, maintenance_window_start, maintenance_window_end,
		       approval_timeout, COALESCE(required_approvals,0), COALESCE(auto_approve_on_timeout,false),
		       COALESCE(package_name,''), COALESCE(package_version,''), COALESCE(rollback_version,''),
		       COALESCE(failure_reason,''), created_at, updated_at, completed_at
		FROM patch_jobs
		WHERE id = $1
		LIMIT 1
	`
	job := &models.PatchJob{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&job.ID, &job.OrgID, &job.Title, &job.Description,
		&job.Severity, &job.State,
		&job.CreatedBy, &job.ScheduledAt, &job.MaintenanceWindowStart, &job.MaintenanceWindowEnd,
		&job.ApprovalTimeout, &job.RequiredApprovals, &job.AutoApproveOnTimeout,
		&job.PackageName, &job.PackageVersion, &job.RollbackVersion,
		&job.FailureReason, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPatchJobNotFound
		}
		return nil, fmt.Errorf("patches: get job: %w", err)
	}

	targets, err := s.GetPatchJobTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	job.Targets = targets

	approvals, err := s.GetApprovalHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	job.Approvals = approvals

	return job, nil
}

// ListPatchJobs returns a filtered list of patch jobs plus the total
// matching count. Filters are applied additively. Results are ordered
// by created_at DESC.
func (s *pgPatchStore) ListPatchJobs(ctx context.Context, f PatchJobFilter) ([]models.PatchJob, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("patches: nil pool")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	args := make([]any, 0, 8)
	where := make([]string, 0, 6)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.State != "" {
		add("state = $%d", f.State)
	}
	if f.Severity != "" {
		add("severity = $%d", f.Severity)
	}
	if f.OrgID != "" {
		add("org_id = $%d", f.OrgID)
	}
	if !f.From.IsZero() {
		add("created_at >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("created_at <= $%d", f.To)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAndPatches(where)
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM patch_jobs "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("patches: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(org_id,''), COALESCE(title,''), COALESCE(description,''),
		       COALESCE(severity,'standard'), COALESCE(state,'pending_approval'),
		       COALESCE(created_by,''), scheduled_at, maintenance_window_start, maintenance_window_end,
		       approval_timeout, COALESCE(required_approvals,0), COALESCE(auto_approve_on_timeout,false),
		       COALESCE(package_name,''), COALESCE(package_version,''), COALESCE(rollback_version,''),
		       COALESCE(failure_reason,''), created_at, updated_at, completed_at
		FROM patch_jobs
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("patches: list: %w", err)
	}
	defer rows.Close()

	out := make([]models.PatchJob, 0, f.Limit)
	for rows.Next() {
		var job models.PatchJob
		if err := rows.Scan(
			&job.ID, &job.OrgID, &job.Title, &job.Description,
			&job.Severity, &job.State,
			&job.CreatedBy, &job.ScheduledAt, &job.MaintenanceWindowStart, &job.MaintenanceWindowEnd,
			&job.ApprovalTimeout, &job.RequiredApprovals, &job.AutoApproveOnTimeout,
			&job.PackageName, &job.PackageVersion, &job.RollbackVersion,
			&job.FailureReason, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("patches: scan: %w", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("patches: rows err: %w", err)
	}
	return out, total, nil
}

// UpdatePatchJob updates the mutable columns of a patch job. Returns
// ErrPatchJobNotFound if no row matches.
func (s *pgPatchStore) UpdatePatchJob(ctx context.Context, job *models.PatchJob) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if job.ID == "" {
		return errors.New("patches: job ID required")
	}
	const q = `
		UPDATE patch_jobs SET
			title = $2,
			description = $3,
			severity = $4,
			state = $5,
			scheduled_at = $6,
			maintenance_window_start = $7,
			maintenance_window_end = $8,
			approval_timeout = $9,
			required_approvals = $10,
			auto_approve_on_timeout = $11,
			package_name = $12,
			package_version = $13,
			rollback_version = $14,
			failure_reason = $15,
			updated_at = $16,
			completed_at = $17
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q,
		job.ID, job.Title, job.Description, job.Severity, job.State,
		job.ScheduledAt, job.MaintenanceWindowStart, job.MaintenanceWindowEnd,
		job.ApprovalTimeout, job.RequiredApprovals, job.AutoApproveOnTimeout,
		job.PackageName, job.PackageVersion, job.RollbackVersion,
		job.FailureReason, job.UpdatedAt, job.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("patches: update job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPatchJobNotFound
	}
	return nil
}

// DeletePatchJob removes a patch job by id. Returns ErrPatchJobNotFound
// if no row matches.
func (s *pgPatchStore) DeletePatchJob(ctx context.Context, id string) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	const q = `DELETE FROM patch_jobs WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("patches: delete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPatchJobNotFound
	}
	return nil
}

// InsertApprovalRecord persists a new approval record.
func (s *pgPatchStore) InsertApprovalRecord(ctx context.Context, rec *models.ApprovalRecord) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if rec.ID == "" {
		return errors.New("patches: approval record ID required")
	}
	const q = `
		INSERT INTO patch_approvals (
			id, patch_job_id, approver_id, approver_name, decision, comment, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := s.pool.Exec(ctx, q,
		rec.ID, rec.PatchJobID, rec.ApproverID, rec.ApproverName,
		rec.Decision, rec.Comment, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("patches: insert approval: %w", err)
	}
	return nil
}

func (s *pgPatchStore) insertApprovalTx(ctx context.Context, tx pgx.Tx, rec *models.ApprovalRecord) error {
	const q = `
		INSERT INTO patch_approvals (
			id, patch_job_id, approver_id, approver_name, decision, comment, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.Exec(ctx, q,
		rec.ID, rec.PatchJobID, rec.ApproverID, rec.ApproverName,
		rec.Decision, rec.Comment, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("patches: insert approval: %w", err)
	}
	return nil
}

// GetApprovalHistory returns all approval records for a job, ordered
// from oldest to newest.
func (s *pgPatchStore) GetApprovalHistory(ctx context.Context, jobID string) ([]models.ApprovalRecord, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	const q = `
		SELECT id, COALESCE(patch_job_id,''), COALESCE(approver_id,''), COALESCE(approver_name,''),
		       COALESCE(decision,''), COALESCE(comment,''), created_at
		FROM patch_approvals
		WHERE patch_job_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("patches: approval history: %w", err)
	}
	defer rows.Close()
	out := make([]models.ApprovalRecord, 0, 4)
	for rows.Next() {
		var r models.ApprovalRecord
		if err := rows.Scan(
			&r.ID, &r.PatchJobID, &r.ApproverID, &r.ApproverName,
			&r.Decision, &r.Comment, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("patches: scan approval: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertPatchJobTarget adds a new target endpoint to a patch job.
func (s *pgPatchStore) InsertPatchJobTarget(ctx context.Context, t *models.PatchJobTarget) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if t.ID == "" {
		return errors.New("patches: target ID required")
	}
	const q = `
		INSERT INTO patch_job_targets (
			id, patch_job_id, agent_id, hostname, status, error_msg, applied_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := s.pool.Exec(ctx, q,
		t.ID, t.PatchJobID, t.AgentID, t.Hostname,
		t.Status, t.ErrorMsg, t.AppliedAt, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("patches: insert target: %w", err)
	}
	return nil
}

func (s *pgPatchStore) insertTargetTx(ctx context.Context, tx pgx.Tx, t *models.PatchJobTarget) error {
	if t.ID == "" {
		return errors.New("patches: target ID required")
	}
	const q = `
		INSERT INTO patch_job_targets (
			id, patch_job_id, agent_id, hostname, status, error_msg, applied_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(ctx, q,
		t.ID, t.PatchJobID, t.AgentID, t.Hostname,
		t.Status, t.ErrorMsg, t.AppliedAt, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("patches: insert target: %w", err)
	}
	return nil
}

// GetPatchJobTargets returns all targets for a patch job.
func (s *pgPatchStore) GetPatchJobTargets(ctx context.Context, jobID string) ([]models.PatchJobTarget, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	const q = `
		SELECT id, COALESCE(patch_job_id,''), COALESCE(agent_id,''), COALESCE(hostname,''),
		       COALESCE(status,'pending'), COALESCE(error_msg,''), applied_at, created_at, updated_at
		FROM patch_job_targets
		WHERE patch_job_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("patches: list targets: %w", err)
	}
	defer rows.Close()
	out := make([]models.PatchJobTarget, 0, 4)
	for rows.Next() {
		var t models.PatchJobTarget
		if err := rows.Scan(
			&t.ID, &t.PatchJobID, &t.AgentID, &t.Hostname,
			&t.Status, &t.ErrorMsg, &t.AppliedAt, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("patches: scan target: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdatePatchJobTarget updates the status of a target endpoint.
func (s *pgPatchStore) UpdatePatchJobTarget(ctx context.Context, t *models.PatchJobTarget) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if t.ID == "" {
		return errors.New("patches: target ID required")
	}
	const q = `
		UPDATE patch_job_targets SET
			status = $2,
			error_msg = $3,
			applied_at = $4,
			updated_at = $5
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q, t.ID, t.Status, t.ErrorMsg, t.AppliedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("patches: update target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("patches: target not found")
	}
	return nil
}

// GetPatchStats returns aggregate statistics for the dashboard. If
// orgID is non-empty, results are scoped to that org.
func (s *pgPatchStore) GetPatchStats(ctx context.Context, orgID string) (*models.PatchStats, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	stats := &models.PatchStats{
		ByState:    map[string]int{},
		BySeverity: map[string]int{},
	}

	// Build optional org filter.
	orgFilter := ""
	args := []any{}
	if orgID != "" {
		orgFilter = " WHERE org_id = $1"
		args = append(args, orgID)
	}

	// Total + by state + by severity.
	q := fmt.Sprintf(`
		SELECT COALESCE(state,'pending_approval'), COALESCE(severity,'standard')
		FROM patch_jobs%s
	`, orgFilter)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("patches: stats query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state, severity string
		if err := rows.Scan(&state, &severity); err != nil {
			return nil, fmt.Errorf("patches: stats scan: %w", err)
		}
		stats.TotalJobs++
		stats.ByState[state]++
		stats.BySeverity[severity]++
		if state == StatePendingApproval {
			stats.PendingApproval++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("patches: stats rows: %w", err)
	}

	// Recent failures (24h).
	failQ := `SELECT COUNT(*) FROM patch_jobs WHERE state = 'failed' AND updated_at >= NOW() - INTERVAL '24 hours'`
	if orgID != "" {
		failQ = `SELECT COUNT(*) FROM patch_jobs WHERE state = 'failed' AND updated_at >= NOW() - INTERVAL '24 hours' AND org_id = $1`
		if err := s.pool.QueryRow(ctx, failQ, orgID).Scan(&stats.RecentFailures); err != nil {
			return nil, fmt.Errorf("patches: recent failures: %w", err)
		}
	} else {
		if err := s.pool.QueryRow(ctx, failQ).Scan(&stats.RecentFailures); err != nil {
			return nil, fmt.Errorf("patches: recent failures: %w", err)
		}
	}

	// Average approval time: the gap between created_at and the first
	// "approved" approval record, averaged across approved jobs.
	avgQ := `
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (a.first_approval - j.created_at)) / 3600.0), 0)
		FROM patch_jobs j
		JOIN LATERAL (
			SELECT MIN(created_at) AS first_approval
			FROM patch_approvals
			WHERE patch_job_id = j.id AND decision = 'approved'
		) a ON true
		WHERE j.state IN ('approved', 'scheduled', 'in_progress', 'completed')
	`
	if orgID != "" {
		avgQ = `
			SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (a.first_approval - j.created_at)) / 3600.0), 0)
			FROM patch_jobs j
			JOIN LATERAL (
				SELECT MIN(created_at) AS first_approval
				FROM patch_approvals
				WHERE patch_job_id = j.id AND decision = 'approved'
			) a ON true
			WHERE j.state IN ('approved', 'scheduled', 'in_progress', 'completed')
			  AND j.org_id = $1
		`
		if err := s.pool.QueryRow(ctx, avgQ, orgID).Scan(&stats.AvgApprovalTime); err != nil {
			return nil, fmt.Errorf("patches: avg approval time: %w", err)
		}
	} else {
		if err := s.pool.QueryRow(ctx, avgQ).Scan(&stats.AvgApprovalTime); err != nil {
			return nil, fmt.Errorf("patches: avg approval time: %w", err)
		}
	}

	return stats, nil
}

// ErrPatchJobNotFound is returned when a patch job id does not exist.
var ErrPatchJobNotFound = errors.New("patch job not found")

// joinAndPatches joins SQL fragments with " AND ".
func joinAndPatches(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// Ensure the json import is used (silence unused import in case of
// future refactors that remove json usage above).
var _ = json.Marshal
