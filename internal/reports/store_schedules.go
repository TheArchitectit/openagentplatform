package reports

import (
	"context"
	"time"

	"github.com/google/uuid"
)

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

// ListDueSchedules returns enabled schedules across all orgs whose
// next_run_at is before the given time.
func (s *PGStore) ListDueSchedules(ctx context.Context, now time.Time) ([]*ReportSchedule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, org_id, template_id, cron_expr, format, params, delivery_method,
			delivery_target, enabled, last_run_at, next_run_at, created_at, updated_at
		 FROM report_schedules
		 WHERE enabled = TRUE AND next_run_at IS NOT NULL AND next_run_at < $1
		 ORDER BY next_run_at ASC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReportSchedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sched)
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
