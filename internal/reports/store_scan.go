package reports

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// rowScanner is the subset of pgx.Row/pgx.Rows used by the scan helpers.
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
