package alerts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/pashagolub/pgxmock/v4"
)

// newTestStore builds a pgAlertStore backed by a pgxmock pool.
func newTestStore(t *testing.T) (*pgAlertStore, pgxmock.PgxPoolIface, func()) {
	t.Helper()
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	s := &pgAlertStore{pool: pool}
	return s, pool, pool.Close
}

func sampleWindow() *models.AlertSuppressionWindow {
	return &models.AlertSuppressionWindow{
		ID:        uuid.NewString(),
		OrgID:     uuid.NewString(),
		Name:      "Nightly maintenance",
		ClientID:  "",
		SiteID:    "",
		Start:     time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC),
		Recurring: false,
		Timezone:  "UTC",
		Enabled:   true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestCreateSuppressionWindowRoundTrip verifies INSERT is issued with the right
// column count and that the timezone is persisted (not coerced).
func TestCreateSuppressionWindowRoundTrip(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	w := sampleWindow()
	w.Timezone = "America/Los_Angeles"

	pool.ExpectExec(`INSERT INTO alert_suppression_windows`).
		WithArgs(
			w.ID, w.OrgID, w.Name, nil, nil, // client_id/site_id null when empty
			w.Start, w.End, w.Recurring, []byte(nil), w.Timezone, w.Enabled,
			w.CreatedAt, w.UpdatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.CreateAlertSuppressionWindow(context.Background(), w); err != nil {
		t.Fatalf("CreateAlertSuppressionWindow: %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestCreateSuppressionWindowMissingID refuses an empty ID without touching DB.
func TestCreateSuppressionWindowMissingID(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	w := sampleWindow()
	w.ID = ""
	if err := s.CreateAlertSuppressionWindow(context.Background(), w); err == nil {
		t.Fatal("expected error for empty ID")
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestListSuppressionWindowsOrgScoping verifies rows are scoped to org_id and
// the timezone column is read back.
func TestListSuppressionWindowsOrgScoping(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	otherOrg := uuid.NewString()
	rows := pgxmock.NewRows([]string{
		"id", "org_id", "name", "client_id", "site_id",
		"start", "end", "recurring", "weekdays", "timezone",
		"enabled", "created_at", "updated_at",
	}).AddRow(
		uuid.NewString(), orgID, "Win A", "", "",
		time.Now(), time.Now().Add(time.Hour), false, []byte(nil), "America/New_York",
		true, time.Now(), time.Now(),
	).AddRow(
		uuid.NewString(), otherOrg, "Win B", "", "",
		time.Now(), time.Now().Add(time.Hour), false, []byte(nil), "UTC",
		true, time.Now(), time.Now(),
	)

	pool.ExpectQuery(`SELECT .* FROM alert_suppression_windows WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(rows)

	got, err := s.GetAlertSuppressionWindows(context.Background(), orgID)
	if err != nil {
		t.Fatalf("GetAlertSuppressionWindows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows from query, got %d", len(got))
	}
	if got[0].Timezone != "America/New_York" {
		t.Errorf("expected timezone America/New_York, got %q", got[0].Timezone)
	}
	if got[1].Timezone != "UTC" {
		t.Errorf("expected timezone UTC, got %q", got[1].Timezone)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestUpdateSuppressionWindowNotFound verifies a zero-row UPDATE maps to the
// dedicated sentinel error (D5).
func TestUpdateSuppressionWindowNotFound(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	w := sampleWindow()
	pool.ExpectExec(`UPDATE alert_suppression_windows SET`).
		WithArgs(
			w.ID, w.Name, nil, nil,
			w.Start, w.End, w.Recurring, []byte(nil), w.Timezone, w.Enabled,
			w.UpdatedAt, w.OrgID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := s.UpdateAlertSuppressionWindow(context.Background(), w)
	if !errors.Is(err, ErrAlertSuppressionWindowNotFound) {
		t.Fatalf("expected ErrAlertSuppressionWindowNotFound, got %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestUpdateSuppressionWindowSuccess verifies a one-row UPDATE returns nil.
func TestUpdateSuppressionWindowSuccess(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	w := sampleWindow()
	pool.ExpectExec(`UPDATE alert_suppression_windows SET`).
		WithArgs(
			w.ID, w.Name, nil, nil,
			w.Start, w.End, w.Recurring, []byte(nil), w.Timezone, w.Enabled,
			w.UpdatedAt, w.OrgID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := s.UpdateAlertSuppressionWindow(context.Background(), w); err != nil {
		t.Fatalf("UpdateAlertSuppressionWindow: %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestDeleteSuppressionWindowNotFound verifies a zero-row DELETE maps to the
// dedicated sentinel error (D5).
func TestDeleteSuppressionWindowNotFound(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	id, orgID := uuid.NewString(), uuid.NewString()
	pool.ExpectExec(`DELETE FROM alert_suppression_windows WHERE id = \$1 AND org_id = \$2`).
		WithArgs(id, orgID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := s.DeleteAlertSuppressionWindow(context.Background(), orgID, id)
	if !errors.Is(err, ErrAlertSuppressionWindowNotFound) {
		t.Fatalf("expected ErrAlertSuppressionWindowNotFound, got %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestDeleteSuppressionWindowSuccess verifies org scoping in the WHERE clause
// and a one-row delete returns nil.
func TestDeleteSuppressionWindowSuccess(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	id, orgID := uuid.NewString(), uuid.NewString()
	pool.ExpectExec(`DELETE FROM alert_suppression_windows WHERE id = \$1 AND org_id = \$2`).
		WithArgs(id, orgID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	if err := s.DeleteAlertSuppressionWindow(context.Background(), orgID, id); err != nil {
		t.Fatalf("DeleteAlertSuppressionWindow: %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestActiveSuppressionWindowsForwardsScope verifies the Active query forwards
// orgID, clientID, and siteID as bind args (D1 + scoped suppression).
func TestActiveSuppressionWindowsForwardsScope(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID, clientID, siteID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "org_id", "name", "client_id", "site_id",
		"start", "end", "recurring", "weekdays", "timezone",
		"enabled", "created_at", "updated_at",
	})

	pool.ExpectQuery(`FROM alert_suppression_windows WHERE org_id = \$1`).
		WithArgs(orgID, clientID, siteID).
		WillReturnRows(rows)

	got, err := s.ActiveAlertSuppressionWindows(context.Background(), orgID, clientID, siteID, now)
	if err != nil {
		t.Fatalf("ActiveAlertSuppressionWindows: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestActiveSuppressionWindowsReadsNonRecurringActive verifies a returned row
// with NULL weekdays decodes to an empty Weekdays slice and IsActiveAt picks it
// up when now is inside [start,end).
func TestActiveSuppressionWindowsReadsNonRecurringActive(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	id := uuid.NewString()
	start := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 15, 23, 59, 59, 0, time.UTC)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "org_id", "name", "client_id", "site_id",
		"start", "end", "recurring", "weekdays", "timezone",
		"enabled", "created_at", "updated_at",
	}).AddRow(
		id, orgID, "Day window", "", "",
		start, end, false, []byte(nil), "UTC",
		true, time.Now(), time.Now(),
	)

	pool.ExpectQuery(`FROM alert_suppression_windows WHERE org_id = \$1`).
		WithArgs(orgID, nil, nil).
		WillReturnRows(rows)

	got, err := s.ActiveAlertSuppressionWindows(context.Background(), orgID, "", "", now)
	if err != nil {
		t.Fatalf("ActiveAlertSuppressionWindows: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 active window, got %d", len(got))
	}
	if len(got[0].Weekdays) != 0 {
		t.Errorf("expected empty weekdays for NULL column, got %v", got[0].Weekdays)
	}
	if !got[0].IsActiveAt(now) {
		t.Errorf("expected window to be active at %v", now)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
