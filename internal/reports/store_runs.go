package reports

import (
	"context"
	"time"

	"github.com/google/uuid"
)

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
