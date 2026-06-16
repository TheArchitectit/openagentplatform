// Package alerts - store.go implements the PostgreSQL persistence layer
// for alerts, alert rules, state-machine history, and notification records.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// AlertFilter is the filter set for ListAlerts. Zero-valued fields are
// ignored. TimeRange is an inclusive [from, to] window on created_at.
type AlertFilter struct {
	State     string
	Severity  string
	AgentID   string
	SiteID    string
	OrgID     string
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
}

// Store is the full persistence interface for alerts, rules, state
// history, and notifications. The engine and HTTP handlers use this
// interface; pgAlertStore is the default implementation.
type Store interface {
	InsertAlert(ctx context.Context, a *models.Alert) error
	GetAlert(ctx context.Context, id string) (*models.Alert, error)
	GetAlertByDedupKey(ctx context.Context, dedupKey string) (*models.Alert, error)
	ListAlerts(ctx context.Context, f AlertFilter) ([]models.Alert, int, error)
	UpdateAlertState(ctx context.Context, a *models.Alert) error

	GetAlertRules(ctx context.Context, orgID string) ([]models.AlertRule, error)
	CreateAlertRule(ctx context.Context, r *models.AlertRule) error
	UpdateAlertRule(ctx context.Context, r *models.AlertRule) error
	DeleteAlertRule(ctx context.Context, id string) error

	InsertStateTransition(ctx context.Context, t *models.AlertStateMachine) error
	GetStateHistory(ctx context.Context, alertID string) ([]models.AlertStateMachine, error)

	InsertNotificationRecord(ctx context.Context, n *models.NotificationRecord) error
	GetNotificationHistory(ctx context.Context, alertID string) ([]models.NotificationRecord, error)

	// GetNotificationChannelsForRule returns the notification channels
	// configured for an alert rule. The engine calls this to fan out
	// alerts to channels when state changes.
	GetNotificationChannelsForRule(ctx context.Context, ruleID string) ([]notify.NotificationChannel, error)
	// ResolveChannelIDs resolves a set of channel IDs to fully-loaded
	// notification channels. Used by the routing engine.
	ResolveChannelIDs(ctx context.Context, ids []string) ([]notify.NotificationChannel, error)
	// NotificationChannel CRUD used by the API.
	ListNotificationChannels(ctx context.Context, orgID, userID string) ([]notify.NotificationChannel, error)
	GetNotificationChannel(ctx context.Context, id string) (*notify.NotificationChannel, error)
	CreateNotificationChannel(ctx context.Context, c *notify.NotificationChannel) error
	UpdateNotificationChannel(ctx context.Context, c *notify.NotificationChannel) error
	DeleteNotificationChannel(ctx context.Context, id string) error
	// Alert preferences used by both the API and the engine.
	GetUserPreferences(ctx context.Context, userID, orgID string) (*UserAlertPreferences, error)
	UpsertUserPreferences(ctx context.Context, p *UserAlertPreferences) error
	GetGlobalPreferences(ctx context.Context, orgID string) (*GlobalAlertPreferences, error)
	UpsertGlobalPreferences(ctx context.Context, p *GlobalAlertPreferences) error
	// Routing rule and junction-table operations.
	ListRoutingRules(ctx context.Context, orgID string) ([]RoutingRule, error)
	GetRoutingRule(ctx context.Context, id string) (*RoutingRule, error)
	CreateRoutingRule(ctx context.Context, r *RoutingRule) error
	UpdateRoutingRule(ctx context.Context, r *RoutingRule) error
	DeleteRoutingRule(ctx context.Context, id string) error
	SetRuleChannels(ctx context.Context, ruleID string, channelIDs []string) error
	GetRuleChannels(ctx context.Context, ruleID string) ([]string, error)
	GetAlertRuleChannels(ctx context.Context, alertRuleID string) ([]string, error)
	SetAlertRuleChannels(ctx context.Context, alertRuleID string, channelIDs []string) error
	// Default channels (org-level routing fallback).
	GetDefaultChannelIDs(ctx context.Context, orgID string) ([]string, error)
	SetDefaultChannels(ctx context.Context, orgID string, channelIDs []string) error
}

// pgAlertStore is the default PostgreSQL-backed implementation of Store.
type pgAlertStore struct {
	pool *pgxpool.Pool
}

// NewPGStore constructs a Store backed by a pgx connection pool.
func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgAlertStore{pool: pool}
}

// InsertAlert inserts a new alert. The alert's ID and timestamps must
// be set by the caller. Returns an error if the dedup_key already exists.
func (s *pgAlertStore) InsertAlert(ctx context.Context, a *models.Alert) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if a.ID == "" {
		return errors.New("alerts: alert ID required")
	}
	meta, err := jsonOrNull(a.Metadata)
	if err != nil {
		return fmt.Errorf("alerts: marshal metadata: %w", err)
	}
	const q = `
		INSERT INTO alerts (
			id, dedup_key, check_id, agent_id, site_id, org_id, alert_rule_id,
			severity, state, message, metadata,
			acknowledged_by, snoozed_until,
			created_at, updated_at, resolved_at, closed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,
			$12,$13,
			$14,$15,$16,$17
		)
	`
	_, err = s.pool.Exec(ctx, q,
		a.ID, a.DedupKey, a.CheckID, a.AgentID, a.SiteID, a.OrgID, a.AlertRuleID,
		a.Severity, a.State, a.Message, meta,
		a.AcknowledgedBy, a.SnoozedUntil,
		a.CreatedAt, a.UpdatedAt, a.ResolvedAt, a.ClosedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: insert: %w", err)
	}
	return nil
}

// GetAlert fetches a single alert by id. Returns ErrAlertNotFound when
// the id does not exist.
func (s *pgAlertStore) GetAlert(ctx context.Context, id string) (*models.Alert, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT id, COALESCE(dedup_key,''), check_id, COALESCE(agent_id,''),
		       COALESCE(site_id,''), COALESCE(org_id,''), COALESCE(alert_rule_id,''),
		       COALESCE(severity,''), COALESCE(state,'pending'),
		       COALESCE(message,''), metadata,
		       COALESCE(acknowledged_by,''), snoozed_until,
		       created_at, updated_at, resolved_at, closed_at
		FROM alerts
		WHERE id = $1
		LIMIT 1
	`
	a := &models.Alert{}
	var meta []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.DedupKey, &a.CheckID, &a.AgentID,
		&a.SiteID, &a.OrgID, &a.AlertRuleID,
		&a.Severity, &a.State,
		&a.Message, &meta,
		&a.AcknowledgedBy, &a.SnoozedUntil,
		&a.CreatedAt, &a.UpdatedAt, &a.ResolvedAt, &a.ClosedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAlertNotFound
		}
		return nil, fmt.Errorf("alerts: get: %w", err)
	}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &a.Metadata)
	}
	return a, nil
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
		       COALESCE(site_id,''), COALESCE(org_id,''), COALESCE(alert_rule_id,''),
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
			&a.SiteID, &a.OrgID, &a.AlertRuleID,
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

// GetAlertByDedupKey looks up an existing alert by its dedup key. Used by
// the engine to decide between "create new" and "escalate existing".
// Returns ErrAlertNotFound if no matching alert exists.
func (s *pgAlertStore) GetAlertByDedupKey(ctx context.Context, dedupKey string) (*models.Alert, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT id, COALESCE(dedup_key,''), check_id, COALESCE(agent_id,''),
		       COALESCE(site_id,''), COALESCE(org_id,''), COALESCE(alert_rule_id,''),
		       COALESCE(severity,''), COALESCE(state,'pending'),
		       COALESCE(message,''), metadata,
		       COALESCE(acknowledged_by,''), snoozed_until,
		       created_at, updated_at, resolved_at, closed_at
		FROM alerts
		WHERE dedup_key = $1
		  AND state NOT IN ('closed')
		ORDER BY created_at DESC
		LIMIT 1
	`
	a := &models.Alert{}
	var meta []byte
	err := s.pool.QueryRow(ctx, q, dedupKey).Scan(
		&a.ID, &a.DedupKey, &a.CheckID, &a.AgentID,
		&a.SiteID, &a.OrgID, &a.AlertRuleID,
		&a.Severity, &a.State,
		&a.Message, &meta,
		&a.AcknowledgedBy, &a.SnoozedUntil,
		&a.CreatedAt, &a.UpdatedAt, &a.ResolvedAt, &a.ClosedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAlertNotFound
		}
		return nil, fmt.Errorf("alerts: get by dedup: %w", err)
	}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &a.Metadata)
	}
	return a, nil
}

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
		if err := rows.Scan(
			&r.ID, &r.OrgID, &r.Name, &r.Description,
			&r.CheckID, &r.AgentID, &r.SiteID,
			&r.MinSeverity, &chans, &r.Enabled,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan rule: %w", err)
		}
		if len(chans) > 0 {
			_ = json.Unmarshal(chans, &r.NotifyChannels)
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
	const q = `
		INSERT INTO alert_rules (
			id, org_id, name, description,
			check_id, agent_id, site_id,
			min_severity, notify_channels, enabled,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,
			$5,$6,$7,
			$8,$9,$10,
			$11,$12
		)
	`
	_, err = s.pool.Exec(ctx, q,
		r.ID, r.OrgID, r.Name, r.Description,
		r.CheckID, r.AgentID, r.SiteID,
		r.MinSeverity, chans, r.Enabled,
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
			updated_at = $10
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q,
		r.ID, r.Name, r.Description,
		r.CheckID, r.AgentID, r.SiteID,
		r.MinSeverity, chans, r.Enabled,
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
func (s *pgAlertStore) DeleteAlertRule(ctx context.Context, id string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `DELETE FROM alert_rules WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("alerts: delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

// --- State history --------------------------------------------------------

// InsertStateTransition writes a state-machine record to the audit log.
func (s *pgAlertStore) InsertStateTransition(ctx context.Context, t *models.AlertStateMachine) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `
		INSERT INTO alert_state_history (alert_id, from_state, to_state, event, actor, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := s.pool.Exec(ctx, q, t.AlertID, t.FromState, t.ToState, t.Event, t.Actor, t.Reason, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("alerts: insert state history: %w", err)
	}
	return nil
}

// GetStateHistory returns the transition timeline for an alert,
// ordered from oldest to newest.
func (s *pgAlertStore) GetStateHistory(ctx context.Context, alertID string) ([]models.AlertStateMachine, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT COALESCE(from_state,''), COALESCE(to_state,''), COALESCE(event,''),
		       COALESCE(actor,''), COALESCE(reason,''), created_at
		FROM alert_state_history
		WHERE alert_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, alertID)
	if err != nil {
		return nil, fmt.Errorf("alerts: state history: %w", err)
	}
	defer rows.Close()
	out := make([]models.AlertStateMachine, 0, 8)
	for rows.Next() {
		var t models.AlertStateMachine
		if err := rows.Scan(&t.FromState, &t.ToState, &t.Event, &t.Actor, &t.Reason, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("alerts: scan history: %w", err)
		}
		t.AlertID = alertID
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- Notifications ---------------------------------------------------------

// InsertNotificationRecord persists a notification delivery record.
func (s *pgAlertStore) InsertNotificationRecord(ctx context.Context, n *models.NotificationRecord) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if n.ID == "" {
		return errors.New("alerts: notification ID required")
	}
	const q = `
		INSERT INTO alert_notifications (id, alert_id, channel, recipient, status, error_msg, sent_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := s.pool.Exec(ctx, q, n.ID, n.AlertID, n.Channel, n.Recipient, n.Status, n.ErrorMsg, n.SentAt, n.CreatedAt)
	if err != nil {
		return fmt.Errorf("alerts: insert notification: %w", err)
	}
	return nil
}

// GetNotificationHistory returns all notification records for an alert,
// ordered from newest to oldest.
func (s *pgAlertStore) GetNotificationHistory(ctx context.Context, alertID string) ([]models.NotificationRecord, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT id, COALESCE(channel,''), COALESCE(recipient,''), COALESCE(status,'pending'),
		       COALESCE(error_msg,''), sent_at, created_at
		FROM alert_notifications
		WHERE alert_id = $1
		ORDER BY created_at DESC
	`
	rows, err := s.pool.Query(ctx, q, alertID)
	if err != nil {
		return nil, fmt.Errorf("alerts: notification history: %w", err)
	}
	defer rows.Close()
	out := make([]models.NotificationRecord, 0, 8)
	for rows.Next() {
		var n models.NotificationRecord
		if err := rows.Scan(&n.ID, &n.Channel, &n.Recipient, &n.Status, &n.ErrorMsg, &n.SentAt, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("alerts: scan notification: %w", err)
		}
		n.AlertID = alertID
		out = append(out, n)
	}
	return out, rows.Err()
}

// --- Notification channel CRUD ---------------------------------------------

// GetNotificationChannelsForRule resolves an alert rule's
// notify_channels list into fully-loaded notification channels. The rule
// stores a list of channel IDs; this method returns the corresponding
// channel records. If the rule is missing or has no channels, an empty
// slice is returned.
func (s *pgAlertStore) GetNotificationChannelsForRule(ctx context.Context, ruleID string) ([]notify.NotificationChannel, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	if ruleID == "" {
		return []notify.NotificationChannel{}, nil
	}
	const ruleQ = `SELECT notify_channels FROM alert_rules WHERE id = $1 LIMIT 1`
	var chansJSON []byte
	if err := s.pool.QueryRow(ctx, ruleQ, ruleID).Scan(&chansJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []notify.NotificationChannel{}, nil
		}
		return nil, fmt.Errorf("alerts: get rule channels: %w", err)
	}
	if len(chansJSON) == 0 {
		return []notify.NotificationChannel{}, nil
	}
	var ids []string
	if err := json.Unmarshal(chansJSON, &ids); err != nil {
		return nil, fmt.Errorf("alerts: decode notify_channels: %w", err)
	}
	if len(ids) == 0 {
		return []notify.NotificationChannel{}, nil
	}
	return s.loadChannelsByIDs(ctx, ids)
}

// loadChannelsByIDs fetches a set of channels by their IDs. Channels
// that do not exist or are disabled are silently filtered out. Order
// of the input is not preserved.
func (s *pgAlertStore) loadChannelsByIDs(ctx context.Context, ids []string) ([]notify.NotificationChannel, error) {
	if len(ids) == 0 {
		return []notify.NotificationChannel{}, nil
	}
	// Build an IN clause with positional placeholders.
	args := make([]any, len(ids))
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	q := fmt.Sprintf(`
		SELECT id, COALESCE(org_id,''), COALESCE(user_id,''), COALESCE(name,''),
		       COALESCE(type,''), COALESCE(enabled,false), config,
		       created_at, updated_at
		FROM notification_channels
		WHERE id IN (%s) AND enabled = true
	`, strings.Join(placeholders, ","))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("alerts: load channels: %w", err)
	}
	defer rows.Close()
	out := make([]notify.NotificationChannel, 0, len(ids))
	for rows.Next() {
		var c notify.NotificationChannel
		var config []byte
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.UserID, &c.Name,
			&c.Type, &c.Enabled, &config,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan channel: %w", err)
		}
		if len(config) > 0 {
			c.Config = config
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListNotificationChannels returns channels for an org or user. When
// userID is non-empty, both org-wide and user-owned channels are
// returned. Otherwise, only org-wide channels are returned.
func (s *pgAlertStore) ListNotificationChannels(ctx context.Context, orgID, userID string) ([]notify.NotificationChannel, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	var (
		rows pgx.Rows
		err  error
	)
	if userID != "" {
		const q = `
			SELECT id, COALESCE(org_id,''), COALESCE(user_id,''), COALESCE(name,''),
			       COALESCE(type,''), COALESCE(enabled,false), config,
			       created_at, updated_at
			FROM notification_channels
			WHERE org_id = $1 AND (user_id = '' OR user_id = $2)
			ORDER BY created_at DESC
		`
		rows, err = s.pool.Query(ctx, q, orgID, userID)
	} else {
		const q = `
			SELECT id, COALESCE(org_id,''), COALESCE(user_id,''), COALESCE(name,''),
			       COALESCE(type,''), COALESCE(enabled,false), config,
			       created_at, updated_at
			FROM notification_channels
			WHERE org_id = $1 AND user_id = ''
			ORDER BY created_at DESC
		`
		rows, err = s.pool.Query(ctx, q, orgID)
	}
	if err != nil {
		return nil, fmt.Errorf("alerts: list channels: %w", err)
	}
	defer rows.Close()
	out := make([]notify.NotificationChannel, 0, 8)
	for rows.Next() {
		var c notify.NotificationChannel
		var config []byte
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.UserID, &c.Name,
			&c.Type, &c.Enabled, &config,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan channel: %w", err)
		}
		if len(config) > 0 {
			c.Config = config
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResolveChannelIDs resolves a set of channel IDs to fully-loaded
// notification channels. Channels that do not exist or are disabled
// are silently filtered out. This is the Engine interface helper
// used by the routing engine after evaluating routing rules.
func (s *pgAlertStore) ResolveChannelIDs(ctx context.Context, ids []string) ([]notify.NotificationChannel, error) {
	return s.loadChannelsByIDs(ctx, ids)
}

// --- Alert preferences (Engine interface compliance) ---------------------

// GetUserPreferences fetches the per-user alert preferences. Returns
// ErrPreferencesNotFound if no row exists for the user.
func (s *pgAlertStore) GetUserPreferences(ctx context.Context, userID, orgID string) (*UserAlertPreferences, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	if userID == "" {
		return nil, errors.New("alerts: userID required")
	}
	const q = `
		SELECT user_id, COALESCE(org_id,''), quiet_hours, severity_threshold,
		       channel_preferences, COALESCE(mute_all,false), updated_at
		FROM alert_user_preferences
		WHERE user_id = $1 AND org_id = $2
		LIMIT 1
	`
	var prefs UserAlertPreferences
	var qhJSON []byte
	var chJSON []byte
	err := s.pool.QueryRow(ctx, q, userID, orgID).Scan(
		&prefs.UserID, &prefs.OrgID, &qhJSON, &prefs.SeverityThreshold,
		&chJSON, &prefs.MuteAll, &prefs.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPreferencesNotFound
		}
		return nil, fmt.Errorf("alerts: get user preferences: %w", err)
	}
	if len(qhJSON) > 0 {
		prefs.QuietHours, _ = UnmarshalQuietHours(qhJSON)
	}
	if len(chJSON) > 0 {
		_ = json.Unmarshal(chJSON, &prefs.ChannelPreferences)
	}
	return &prefs, nil
}

// GetDefaultChannelIDs returns the org-level default channel IDs for
// routing fallback. Returns an empty slice (not nil) when no defaults
// are configured. Used by the Engine interface for routing fallback
// when the Router is not configured.
func (s *pgAlertStore) GetDefaultChannelIDs(ctx context.Context, orgID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	if orgID == "" {
		return []string{}, nil
	}
	const q = `
		SELECT COALESCE(channel_ids, '{}'::jsonb)
		FROM alert_default_channels
		WHERE org_id = $1
		LIMIT 1
	`
	var raw []byte
	if err := s.pool.QueryRow(ctx, q, orgID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("alerts: get default channels: %w", err)
	}
	if len(raw) == 0 {
		return []string{}, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("alerts: decode default channels: %w", err)
	}
	return ids, nil
}

// SetDefaultChannels upserts the org-level default channel set.
func (s *pgAlertStore) SetDefaultChannels(ctx context.Context, orgID string, channelIDs []string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if orgID == "" {
		return errors.New("alerts: orgID required")
	}
	raw, err := jsonOrNull(channelIDs)
	if err != nil {
		return fmt.Errorf("alerts: marshal default channels: %w", err)
	}
	const q = `
		INSERT INTO alert_default_channels (org_id, channel_ids, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (org_id) DO UPDATE SET
			channel_ids = EXCLUDED.channel_ids,
			updated_at  = NOW()
	`
	_, err = s.pool.Exec(ctx, q, orgID, raw)
	if err != nil {
		return fmt.Errorf("alerts: set default channels: %w", err)
	}
	return nil
}

// --- User preferences CRUD -----------------------------------------------

// UpsertUserPreferences inserts or updates per-user alert preferences.
func (s *pgAlertStore) UpsertUserPreferences(ctx context.Context, p *UserAlertPreferences) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if p.UserID == "" {
		return errors.New("alerts: userID required")
	}
	qh, err := MarshalQuietHours(p.QuietHours)
	if err != nil {
		return fmt.Errorf("alerts: marshal quiet hours: %w", err)
	}
	ch, err := jsonOrNull(p.ChannelPreferences)
	if err != nil {
		return fmt.Errorf("alerts: marshal channel preferences: %w", err)
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO alert_user_preferences (
			user_id, org_id, quiet_hours, severity_threshold,
			channel_preferences, mute_all, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, org_id) DO UPDATE SET
			quiet_hours         = EXCLUDED.quiet_hours,
			severity_threshold  = EXCLUDED.severity_threshold,
			channel_preferences = EXCLUDED.channel_preferences,
			mute_all            = EXCLUDED.mute_all,
			updated_at          = EXCLUDED.updated_at
	`
	_, err = s.pool.Exec(ctx, q,
		p.UserID, p.OrgID, qh, p.SeverityThreshold,
		ch, p.MuteAll, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: upsert user preferences: %w", err)
	}
	return nil
}

// GetGlobalPreferences fetches the org-wide global alert preferences.
// Returns ErrPreferencesNotFound if no row exists.
func (s *pgAlertStore) GetGlobalPreferences(ctx context.Context, orgID string) (*GlobalAlertPreferences, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	if orgID == "" {
		return nil, errors.New("alerts: orgID required")
	}
	const q = `
		SELECT org_id, quiet_hours, retention_days, max_alerts_per_agent,
		       auto_resolve_seconds, updated_at
		FROM alert_global_preferences
		WHERE org_id = $1
		LIMIT 1
	`
	var prefs GlobalAlertPreferences
	var qhJSON []byte
	err := s.pool.QueryRow(ctx, q, orgID).Scan(
		&prefs.OrgID, &qhJSON, &prefs.RetentionDays, &prefs.MaxAlertsPerAgent,
		&prefs.AutoResolveSeconds, &prefs.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPreferencesNotFound
		}
		return nil, fmt.Errorf("alerts: get global preferences: %w", err)
	}
	if len(qhJSON) > 0 {
		prefs.DefaultQuietHours, _ = UnmarshalQuietHours(qhJSON)
	}
	return &prefs, nil
}

// UpsertGlobalPreferences inserts or updates org-wide global preferences.
func (s *pgAlertStore) UpsertGlobalPreferences(ctx context.Context, p *GlobalAlertPreferences) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if p.OrgID == "" {
		return errors.New("alerts: orgID required")
	}
	qh, err := MarshalQuietHours(p.DefaultQuietHours)
	if err != nil {
		return fmt.Errorf("alerts: marshal default quiet hours: %w", err)
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO alert_global_preferences (
			org_id, quiet_hours, retention_days, max_alerts_per_agent,
			auto_resolve_seconds, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (org_id) DO UPDATE SET
			quiet_hours         = EXCLUDED.quiet_hours,
			retention_days      = EXCLUDED.retention_days,
			max_alerts_per_agent = EXCLUDED.max_alerts_per_agent,
			auto_resolve_seconds = EXCLUDED.auto_resolve_seconds,
			updated_at          = EXCLUDED.updated_at
	`
	_, err = s.pool.Exec(ctx, q,
		p.OrgID, qh, p.RetentionDays, p.MaxAlertsPerAgent,
		p.AutoResolveSeconds, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: upsert global preferences: %w", err)
	}
	return nil
}

// --- Routing rules and junction tables -----------------------------------

// ListRoutingRules returns all routing rules for the given org.
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

// GetNotificationChannel fetches a single channel by id.
func (s *pgAlertStore) GetNotificationChannel(ctx context.Context, id string) (*notify.NotificationChannel, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT id, COALESCE(org_id,''), COALESCE(user_id,''), COALESCE(name,''),
		       COALESCE(type,''), COALESCE(enabled,false), config,
		       created_at, updated_at
		FROM notification_channels
		WHERE id = $1
		LIMIT 1
	`
	var c notify.NotificationChannel
	var config []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.OrgID, &c.UserID, &c.Name,
		&c.Type, &c.Enabled, &config,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChannelNotFound
		}
		return nil, fmt.Errorf("alerts: get channel: %w", err)
	}
	if len(config) > 0 {
		c.Config = config
	}
	return &c, nil
}

// CreateNotificationChannel inserts a new notification channel.
func (s *pgAlertStore) CreateNotificationChannel(ctx context.Context, c *notify.NotificationChannel) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if c.ID == "" {
		return errors.New("alerts: channel ID required")
	}
	const q = `
		INSERT INTO notification_channels (
			id, org_id, user_id, name, type, enabled, config,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := s.pool.Exec(ctx, q,
		c.ID, c.OrgID, c.UserID, c.Name, c.Type, c.Enabled, c.Config,
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: create channel: %w", err)
	}
	return nil
}

// UpdateNotificationChannel updates an existing channel by id.
func (s *pgAlertStore) UpdateNotificationChannel(ctx context.Context, c *notify.NotificationChannel) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if c.ID == "" {
		return errors.New("alerts: channel ID required")
	}
	const q = `
		UPDATE notification_channels SET
			name = $2,
			enabled = $3,
			config = $4,
			updated_at = $5
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q,
		c.ID, c.Name, c.Enabled, c.Config, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: update channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// DeleteNotificationChannel deletes a channel by id.
func (s *pgAlertStore) DeleteNotificationChannel(ctx context.Context, id string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `DELETE FROM notification_channels WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("alerts: delete channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// --- Errors ----------------------------------------------------------------

// ErrAlertNotFound is returned when an alert id does not exist.
var ErrAlertNotFound = errors.New("alert not found")

// ErrAlertRuleNotFound is returned when an alert rule id does not exist.
var ErrAlertRuleNotFound = errors.New("alert rule not found")

// ErrChannelNotFound is returned when a notification channel id does
// not exist.
var ErrChannelNotFound = errors.New("notification channel not found")

// --- helpers ---------------------------------------------------------------

// jsonOrNull marshals v to JSON, or returns nil if v is empty.
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// joinAnd joins SQL fragments with " AND ".
func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
