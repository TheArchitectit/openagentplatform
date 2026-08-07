package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	GetAlert(ctx context.Context, orgID, id string) (*models.Alert, error)
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

// GetAlert fetches a single alert by id, scoped to the given org.
// If orgID is non-empty, the query enforces org ownership; otherwise the
// caller is responsible for org verification.
// Returns ErrAlertNotFound when the id does not exist.
func (s *pgAlertStore) GetAlert(ctx context.Context, orgID, id string) (*models.Alert, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	args := []any{id}
	where := []string{"id = $1"}
	if orgID != "" {
		args = append(args, orgID)
		where = append(where, fmt.Sprintf("org_id = $%d", len(args)))
	}
	q := `
		SELECT id, COALESCE(dedup_key,''), check_id, COALESCE(agent_id,''),
		       COALESCE(site_id,''), COALESCE(org_id,''), COALESCE(alert_rule_id,''),
		       COALESCE(severity,''), COALESCE(state,'pending'),
		       COALESCE(message,''), metadata,
		       COALESCE(acknowledged_by,''), snoozed_until,
		       created_at, updated_at, resolved_at, closed_at
		FROM alerts
		WHERE ` + joinAnd(where) + `
		LIMIT 1
	`
	a := &models.Alert{}
	var meta []byte
	err := s.pool.QueryRow(ctx, q, args...).Scan(
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

// --- State history --------------------------------------------------------

// InsertStateTransition writes a state-machine record to the audit log.
