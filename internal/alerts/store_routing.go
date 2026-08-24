package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrAlertNotFound is returned when an alert id does not exist.
var ErrAlertNotFound = errors.New("alert not found")

// ErrAlertRuleNotFound is returned when an alert rule id does not exist.
var ErrAlertRuleNotFound = errors.New("alert rule not found")

// ErrAlertSuppressionWindowNotFound is returned when a fleet-level
// alert-suppression-window id does not exist. It is deliberately distinct
// from ErrAlertRuleNotFound so callers can map it to a 404 without
// conflating the two entities.
var ErrAlertSuppressionWindowNotFound = errors.New("alert suppression window not found")

// ErrChannelNotFound is returned when a notification channel id does
// not exist.
var ErrChannelNotFound = errors.New("notification channel not found")

func (s *pgAlertStore) ListRoutingRules(ctx context.Context, orgID string) ([]RoutingRule, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	if orgID == "" {
		return []RoutingRule{}, nil
	}
	const q = `
		SELECT id, COALESCE(org_id,''), COALESCE(name,''), COALESCE(description,''),
		       priority, conditions, COALESCE(enabled,false),
		       created_at, updated_at
		FROM alert_routing_rules
		WHERE org_id = $1
		ORDER BY priority ASC, created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("alerts: list routing rules: %w", err)
	}
	defer rows.Close()
	out := make([]RoutingRule, 0, 8)
	for rows.Next() {
		var r RoutingRule
		var cond []byte
		if err := rows.Scan(
			&r.ID, &r.OrgID, &r.Name, &r.Description,
			&r.Priority, &cond, &r.Enabled,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan routing rule: %w", err)
		}
		if len(cond) > 0 {
			_ = json.Unmarshal(cond, &r.Conditions)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alerts: routing rules rows: %w", err)
	}
	// Populate ChannelIDs for each rule.
	for i := range out {
		ids, err := s.GetRuleChannels(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].ChannelIDs = ids
	}
	return out, nil
}

// GetRoutingRule fetches a single routing rule by id.
func (s *pgAlertStore) GetRoutingRule(ctx context.Context, id string) (*RoutingRule, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT id, COALESCE(org_id,''), COALESCE(name,''), COALESCE(description,''),
		       priority, conditions, COALESCE(enabled,false),
		       created_at, updated_at
		FROM alert_routing_rules
		WHERE id = $1
		LIMIT 1
	`
	var r RoutingRule
	var cond []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&r.ID, &r.OrgID, &r.Name, &r.Description,
		&r.Priority, &cond, &r.Enabled,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("routing rule not found")
		}
		return nil, fmt.Errorf("alerts: get routing rule: %w", err)
	}
	if len(cond) > 0 {
		_ = json.Unmarshal(cond, &r.Conditions)
	}
	r.ChannelIDs, _ = s.GetRuleChannels(ctx, id)
	return &r, nil
}

// CreateRoutingRule inserts a new routing rule.
func (s *pgAlertStore) CreateRoutingRule(ctx context.Context, r *RoutingRule) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if r.ID == "" {
		return errors.New("alerts: rule ID required")
	}
	cond, err := jsonOrNull(r.Conditions)
	if err != nil {
		return fmt.Errorf("alerts: marshal conditions: %w", err)
	}
	const q = `
		INSERT INTO alert_routing_rules (
			id, org_id, name, description, priority, conditions, enabled,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err = s.pool.Exec(ctx, q,
		r.ID, r.OrgID, r.Name, r.Description, r.Priority, cond, r.Enabled,
		r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: create routing rule: %w", err)
	}
	if len(r.ChannelIDs) > 0 {
		return s.SetRuleChannels(ctx, r.ID, r.ChannelIDs)
	}
	return nil
}

// UpdateRoutingRule updates an existing routing rule.
func (s *pgAlertStore) UpdateRoutingRule(ctx context.Context, r *RoutingRule) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if r.ID == "" {
		return errors.New("alerts: rule ID required")
	}
	cond, err := jsonOrNull(r.Conditions)
	if err != nil {
		return fmt.Errorf("alerts: marshal conditions: %w", err)
	}
	const q = `
		UPDATE alert_routing_rules SET
			name        = $2,
			description = $3,
			priority    = $4,
			conditions  = $5,
			enabled     = $6,
			updated_at  = $7
		WHERE id = $1
	`
	_, err = s.pool.Exec(ctx, q,
		r.ID, r.Name, r.Description, r.Priority, cond, r.Enabled, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: update routing rule: %w", err)
	}
	return s.SetRuleChannels(ctx, r.ID, r.ChannelIDs)
}

// DeleteRoutingRule removes a routing rule (and its junction rows via FK).
func (s *pgAlertStore) DeleteRoutingRule(ctx context.Context, id string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `DELETE FROM alert_routing_rules WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("alerts: delete routing rule: %w", err)
	}
	return nil
}

// SetRuleChannels replaces the channel set for a routing rule in the
// alert_rule_routing_channels junction.
func (s *pgAlertStore) SetRuleChannels(ctx context.Context, ruleID string, channelIDs []string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("alerts: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM alert_rule_routing_channels WHERE rule_id = $1`, ruleID); err != nil {
		return fmt.Errorf("alerts: clear rule channels: %w", err)
	}
	for _, cid := range channelIDs {
		if cid == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO alert_rule_routing_channels (rule_id, channel_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			ruleID, cid); err != nil {
			return fmt.Errorf("alerts: insert rule channel: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// GetRuleChannels returns the channel IDs for a routing rule.
func (s *pgAlertStore) GetRuleChannels(ctx context.Context, ruleID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `SELECT channel_id FROM alert_rule_routing_channels WHERE rule_id = $1 ORDER BY channel_id`
	rows, err := s.pool.Query(ctx, q, ruleID)
	if err != nil {
		return nil, fmt.Errorf("alerts: get rule channels: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 4)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("alerts: scan rule channel: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetAlertRuleChannels returns the channel IDs linked to a given
// alert rule through the alert_rule_channels junction.
func (s *pgAlertStore) GetAlertRuleChannels(ctx context.Context, alertRuleID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `SELECT channel_id FROM alert_rule_channels WHERE alert_rule_id = $1 ORDER BY channel_id`
	rows, err := s.pool.Query(ctx, q, alertRuleID)
	if err != nil {
		return nil, fmt.Errorf("alerts: get alert rule channels: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 4)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("alerts: scan alert rule channel: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetAlertRuleChannels replaces the channel set for an alert rule in
// the alert_rule_channels junction.
func (s *pgAlertStore) SetAlertRuleChannels(ctx context.Context, alertRuleID string, channelIDs []string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("alerts: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM alert_rule_channels WHERE alert_rule_id = $1`, alertRuleID); err != nil {
		return fmt.Errorf("alerts: clear alert rule channels: %w", err)
	}
	for _, cid := range channelIDs {
		if cid == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO alert_rule_channels (alert_rule_id, channel_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			alertRuleID, cid); err != nil {
			return fmt.Errorf("alerts: insert alert rule channel: %w", err)
		}
	}
	return tx.Commit(ctx)
}
