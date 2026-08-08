package patches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
