package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// UpdateAlertState updates the mutable state-related columns of an alert.
// All transition side-effects (timestamps, acknowledged_by, snoozed_until)
// must be set on the alert before calling. Returns ErrAlertNotFound if no
// row matches.
func (s *pgAlertStore) UpdateAlertState(ctx context.Context, a *models.Alert) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if a.ID == "" {
		return errors.New("alerts: alert ID required")
	}
	const q = `
		UPDATE alerts SET
			state = $2,
			acknowledged_by = $3,
			snoozed_until = $4,
			updated_at = $5,
			resolved_at = $6,
			closed_at = $7
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q,
		a.ID, a.State, a.AcknowledgedBy, a.SnoozedUntil,
		a.UpdatedAt, a.ResolvedAt, a.ClosedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: update state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertNotFound
	}
	return nil
}

// ListAlerts returns a filtered list of alerts plus the total matching
// count. Filters are applied additively. Results are ordered by created_at DESC.
func (s *pgAlertStore) ListAlerts(ctx context.Context, f AlertFilter) ([]models.Alert, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("alerts: nil pool")
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
	if f.AgentID != "" {
		add("agent_id = $%d", f.AgentID)
	}
	if f.SiteID != "" {
		add("site_id = $%d", f.SiteID)
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
		whereSQL = "WHERE " + joinAnd(where)
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM alerts "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("alerts: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(dedup_key,''), check_id, COALESCE(agent_id,''),
		       COALESCE(site_id,''), COALESCE(org_id,''), COALESCE(client_id,''),
		       COALESCE(alert_rule_id,''),
		       COALESCE(severity,''), COALESCE(state,'pending'),
		       COALESCE(message,''), metadata,
		       COALESCE(acknowledged_by,''), snoozed_until,
		       created_at, updated_at, resolved_at, closed_at
		FROM alerts
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("alerts: list: %w", err)
	}
	defer rows.Close()

	out := make([]models.Alert, 0, f.Limit)
	for rows.Next() {
		var a models.Alert
		var meta []byte
		if err := rows.Scan(
			&a.ID, &a.DedupKey, &a.CheckID, &a.AgentID,
			&a.SiteID, &a.OrgID, &a.ClientID, &a.AlertRuleID,
			&a.Severity, &a.State,
			&a.Message, &meta,
			&a.AcknowledgedBy, &a.SnoozedUntil,
			&a.CreatedAt, &a.UpdatedAt, &a.ResolvedAt, &a.ClosedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("alerts: scan: %w", err)
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &a.Metadata)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("alerts: rows err: %w", err)
	}
	return out, total, nil
}
