package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// --- Alert rules -----------------------------------------------------------

// GetAlertRules returns all alert rules, optionally filtered by org_id.
func (s *pgAlertStore) GetAlertRules(ctx context.Context, orgID string) ([]models.AlertRule, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	var (
		rows pgx.Rows
		err  error
	)
	if orgID != "" {
		const q = `
			SELECT id, COALESCE(org_id,''), COALESCE(name,''), COALESCE(description,''),
			       COALESCE(check_id,''), COALESCE(agent_id,''), COALESCE(site_id,''),
			       COALESCE(min_severity,'warning'), notify_channels, COALESCE(enabled,false),
			       offline_silence_seconds,
			       created_at, updated_at
			FROM alert_rules
			WHERE org_id = $1
			ORDER BY created_at DESC
		`
		rows, err = s.pool.Query(ctx, q, orgID)
	} else {
		const q = `
			SELECT id, COALESCE(org_id,''), COALESCE(name,''), COALESCE(description,''),
			       COALESCE(check_id,''), COALESCE(agent_id,''), COALESCE(site_id,''),
			       COALESCE(min_severity,'warning'), notify_channels, COALESCE(enabled,false),
			       offline_silence_seconds,
			       created_at, updated_at
			FROM alert_rules
			ORDER BY created_at DESC
		`
		rows, err = s.pool.Query(ctx, q)
	}
	if err != nil {
		return nil, fmt.Errorf("alerts: list rules: %w", err)
	}
	defer rows.Close()
	out := make([]models.AlertRule, 0, 16)
	for rows.Next() {
		var r models.AlertRule
		var chans []byte
		var silence int
		if err := rows.Scan(
			&r.ID, &r.OrgID, &r.Name, &r.Description,
			&r.CheckID, &r.AgentID, &r.SiteID,
			&r.MinSeverity, &chans, &r.Enabled,
			&silence,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan rule: %w", err)
		}
		if len(chans) > 0 {
			_ = json.Unmarshal(chans, &r.NotifyChannels)
		}
		if silence > 0 {
			r.OfflineSilenceSeconds = &silence
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateAlertRule inserts a new alert rule. The rule's ID and timestamps
// must be set by the caller.
func (s *pgAlertStore) CreateAlertRule(ctx context.Context, r *models.AlertRule) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if r.ID == "" {
		return errors.New("alerts: rule ID required")
	}
	chans, err := jsonOrNull(r.NotifyChannels)
	if err != nil {
		return fmt.Errorf("alerts: marshal channels: %w", err)
	}
	var silence any
	if r.OfflineSilenceSeconds != nil {
		silence = *r.OfflineSilenceSeconds
	}
	const q = `
		INSERT INTO alert_rules (
			id, org_id, name, description,
			check_id, agent_id, site_id,
			min_severity, notify_channels, enabled,
			offline_silence_seconds,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,
			$5,$6,$7,
			$8,$9,$10,
			$11,
			$12,$13
		)
	`
	_, err = s.pool.Exec(ctx, q,
		r.ID, r.OrgID, r.Name, r.Description,
		r.CheckID, r.AgentID, r.SiteID,
		r.MinSeverity, chans, r.Enabled,
		silence,
		r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: create rule: %w", err)
	}
	return nil
}

// UpdateAlertRule updates an existing alert rule by id. Returns
// ErrAlertRuleNotFound if no row matches.
func (s *pgAlertStore) UpdateAlertRule(ctx context.Context, r *models.AlertRule) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if r.ID == "" {
		return errors.New("alerts: rule ID required")
	}
	chans, err := jsonOrNull(r.NotifyChannels)
	if err != nil {
		return fmt.Errorf("alerts: marshal channels: %w", err)
	}
	var silence any
	if r.OfflineSilenceSeconds != nil {
		silence = *r.OfflineSilenceSeconds
	}
	const q = `
		UPDATE alert_rules SET
			name = $2,
			description = $3,
			check_id = $4,
			agent_id = $5,
			site_id = $6,
			min_severity = $7,
			notify_channels = $8,
			enabled = $9,
			offline_silence_seconds = $10,
			updated_at = $11
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q,
		r.ID, r.Name, r.Description,
		r.CheckID, r.AgentID, r.SiteID,
		r.MinSeverity, chans, r.Enabled,
		silence,
		r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: update rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

// DeleteAlertRule deletes an alert rule by id. Returns ErrAlertRuleNotFound
// if no row matches.
func (s *pgAlertStore) DeleteAlertRule(ctx context.Context, orgID, id string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `DELETE FROM alert_rules WHERE id = $1 AND org_id = $2`
	tag, err := s.pool.Exec(ctx, q, id, orgID)
	if err != nil {
		return fmt.Errorf("alerts: delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}
