package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/internal/notify"
)

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
