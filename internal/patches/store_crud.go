package patches

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
// targets and approval records, scoped to the given org.
// If orgID is non-empty, the query enforces org ownership.
// Returns ErrPatchJobNotFound when the id does not exist.
func (s *pgPatchStore) GetPatchJob(ctx context.Context, orgID, id string) (*models.PatchJob, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	args := []any{id}
	where := []string{"id = $1"}
	if orgID != "" {
		args = append(args, orgID)
		where = append(where, fmt.Sprintf("org_id = $%d", len(args)))
	}
	q := `
		SELECT id, COALESCE(org_id,''), COALESCE(title,''), COALESCE(description,''),
		       COALESCE(severity,'standard'), COALESCE(state,'pending_approval'),
		       COALESCE(created_by,''), scheduled_at, maintenance_window_start, maintenance_window_end,
		       approval_timeout, COALESCE(required_approvals,0), COALESCE(auto_approve_on_timeout,false),
		       COALESCE(package_name,''), COALESCE(package_version,''), COALESCE(rollback_version,''),
		       COALESCE(failure_reason,''), created_at, updated_at, completed_at
		FROM patch_jobs
		WHERE ` + joinAndPatches(where) + `
		LIMIT 1
	`
	job := &models.PatchJob{}
	err := s.pool.QueryRow(ctx, q, args...).Scan(
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
func (s *pgPatchStore) DeletePatchJob(ctx context.Context, orgID, id string) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	args := []any{id}
	where := "id = $1"
	if orgID != "" {
		args = append(args, orgID)
		where += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	q := "DELETE FROM patch_jobs WHERE " + where
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("patches: delete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPatchJobNotFound
	}
	return nil
}

// InsertApprovalRecord persists a new approval record.
