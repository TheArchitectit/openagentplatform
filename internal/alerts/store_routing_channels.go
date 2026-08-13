package alerts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/internal/notify"
)

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
func (s *pgAlertStore) DeleteNotificationChannel(ctx context.Context, orgID, id string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `DELETE FROM notification_channels WHERE id = $1 AND org_id = $2`
	tag, err := s.pool.Exec(ctx, q, id, orgID)
	if err != nil {
		return fmt.Errorf("alerts: delete channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChannelNotFound
	}
	return nil
}
