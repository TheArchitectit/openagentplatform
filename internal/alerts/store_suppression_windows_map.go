package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// SuppressionWindowStore is the persistence seam for fleet-level
// alert-suppression windows. It is separate from patch-deploy windows and from
// per-user quiet hours.
type SuppressionWindowStore interface {
	CreateAlertSuppressionWindow(ctx context.Context, w *models.AlertSuppressionWindow) error
	GetAlertSuppressionWindows(ctx context.Context, orgID string) ([]models.AlertSuppressionWindow, error)
	UpdateAlertSuppressionWindow(ctx context.Context, w *models.AlertSuppressionWindow) error
	DeleteAlertSuppressionWindow(ctx context.Context, orgID, id string) error
	// ActiveAlertSuppressionWindows returns enabled windows for the given
	// org, client, and site that are active at now. An empty clientID/siteID
	// widens the scope.
	ActiveAlertSuppressionWindows(ctx context.Context, orgID, clientID, siteID string, now time.Time) ([]models.AlertSuppressionWindow, error)
}

// --- Suppression window CRUD --------------------------------------------

func (s *pgAlertStore) CreateAlertSuppressionWindow(ctx context.Context, w *models.AlertSuppressionWindow) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if w.ID == "" {
		return errors.New("alerts: suppression window ID required")
	}
	weekdays, err := jsonOrNull(weekdaysToInts(w.Weekdays))
	if err != nil {
		return fmt.Errorf("alerts: marshal weekdays: %w", err)
	}
	const q = `
		INSERT INTO alert_suppression_windows (
			id, org_id, name, client_id, site_id,
			start, "end", recurring, weekdays, timezone, enabled,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`
	_, err = s.pool.Exec(ctx, q,
		w.ID, w.OrgID, w.Name, nullIfEmpty(w.ClientID), nullIfEmpty(w.SiteID),
		w.Start, w.End, w.Recurring, weekdays, tzOrUTC(w.Timezone), w.Enabled,
		w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("alerts: create suppression window: %w", err)
	}
	return nil
}

func (s *pgAlertStore) GetAlertSuppressionWindows(ctx context.Context, orgID string) ([]models.AlertSuppressionWindow, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	const q = `
		SELECT id, org_id, name, COALESCE(client_id,''), COALESCE(site_id,''),
		       start, "end", COALESCE(recurring,false), weekdays,
		       COALESCE(timezone,'UTC'), COALESCE(enabled,true), created_at, updated_at
		FROM alert_suppression_windows
		WHERE org_id = $1
		ORDER BY created_at DESC
	`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("alerts: list suppression windows: %w", err)
	}
	defer rows.Close()
	out := make([]models.AlertSuppressionWindow, 0, 8)
	for rows.Next() {
		var w models.AlertSuppressionWindow
		var weekdays []byte
		if err := rows.Scan(
			&w.ID, &w.OrgID, &w.Name, &w.ClientID, &w.SiteID,
			&w.Start, &w.End, &w.Recurring, &weekdays, &w.Timezone,
			&w.Enabled, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan suppression window: %w", err)
		}
		w.Weekdays = intsToWeekdays(weekdays)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *pgAlertStore) UpdateAlertSuppressionWindow(ctx context.Context, w *models.AlertSuppressionWindow) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	if w.ID == "" {
		return errors.New("alerts: suppression window ID required")
	}
	weekdays, err := jsonOrNull(weekdaysToInts(w.Weekdays))
	if err != nil {
		return fmt.Errorf("alerts: marshal weekdays: %w", err)
	}
	const q = `
		UPDATE alert_suppression_windows SET
			name = $2,
			client_id = $3,
			site_id = $4,
			start = $5,
			"end" = $6,
			recurring = $7,
			weekdays = $8,
			timezone = $9,
			enabled = $10,
			updated_at = $11
		WHERE id = $1 AND org_id = $12
	`
	tag, err := s.pool.Exec(ctx, q,
		w.ID, w.Name, nullIfEmpty(w.ClientID), nullIfEmpty(w.SiteID),
		w.Start, w.End, w.Recurring, weekdays, tzOrUTC(w.Timezone), w.Enabled,
		w.UpdatedAt, w.OrgID,
	)
	if err != nil {
		return fmt.Errorf("alerts: update suppression window: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertSuppressionWindowNotFound
	}
	return nil
}

func (s *pgAlertStore) DeleteAlertSuppressionWindow(ctx context.Context, orgID, id string) error {
	if s.pool == nil {
		return errors.New("alerts: nil pool")
	}
	const q = `DELETE FROM alert_suppression_windows WHERE id = $1 AND org_id = $2`
	tag, err := s.pool.Exec(ctx, q, id, orgID)
	if err != nil {
		return fmt.Errorf("alerts: delete suppression window: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertSuppressionWindowNotFound
	}
	return nil
}

// ActiveAlertSuppressionWindows returns enabled windows whose scope covers
// the given org/client/site and that are active at now. All three levels
// (org, client, site) are queried so the engine can pick the tightest match
// without a separate pass; the IsActiveAt check on each returned window makes
// the time-boundary decision.
func (s *pgAlertStore) ActiveAlertSuppressionWindows(ctx context.Context, orgID, clientID, siteID string, now time.Time) ([]models.AlertSuppressionWindow, error) {
	if s.pool == nil {
		return nil, errors.New("alerts: nil pool")
	}
	// Covers org-wide OR specific client OR specific site.
	q := `
		SELECT id, org_id, name, COALESCE(client_id,''), COALESCE(site_id,''),
		       start, "end", COALESCE(recurring,false), weekdays,
		       COALESCE(timezone,'UTC'),
		       COALESCE(enabled,true), created_at, updated_at
		FROM alert_suppression_windows
		WHERE org_id = $1
		  AND COALESCE(enabled,true) = true
		  AND (
		    (client_id IS NULL OR client_id = '')                          -- org-wide
		    OR client_id = $2                                              -- client-scoped
		    OR (client_id = $2 AND site_id = $3)                           -- site-scoped under client
		    OR (site_id = $3 AND (client_id IS NULL OR client_id = ''))    -- site-scoped (orphan)
		  )
	`
	rows, err := s.pool.Query(ctx, q, orgID, nullIfEmpty(clientID), nullIfEmpty(siteID))
	if err != nil {
		return nil, fmt.Errorf("alerts: active suppression windows: %w", err)
	}
	defer rows.Close()
	out := make([]models.AlertSuppressionWindow, 0, 4)
	for rows.Next() {
		var w models.AlertSuppressionWindow
		var weekdays []byte
		if err := rows.Scan(
			&w.ID, &w.OrgID, &w.Name, &w.ClientID, &w.SiteID,
			&w.Start, &w.End, &w.Recurring, &weekdays, &w.Timezone,
			&w.Enabled, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("alerts: scan suppression window: %w", err)
		}
		w.Weekdays = intsToWeekdays(weekdays)
		if w.IsActiveAt(now) {
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

// weekdaysToInts converts time.Weekday values to ints for JSON storage.
func weekdaysToInts(days []time.Weekday) []int {
	if len(days) == 0 {
		return nil
	}
	out := make([]int, 0, len(days))
	for _, d := range days {
		out = append(out, int(d))
	}
	return out
}

// intsToWeekdays converts stored ints back to time.Weekday. Invalid ints are
// skipped silently.
func intsToWeekdays(b []byte) []time.Weekday {
	if len(b) == 0 {
		return nil
	}
	var raw []int
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	out := make([]time.Weekday, 0, len(raw))
	for _, v := range raw {
		if v >= 0 && v <= 6 {
			out = append(out, time.Weekday(v))
		}
	}
	return out
}
