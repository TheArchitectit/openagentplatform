package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
