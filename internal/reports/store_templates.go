package reports

import (
	"context"
	"time"

	"github.com/google/uuid"
)

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
