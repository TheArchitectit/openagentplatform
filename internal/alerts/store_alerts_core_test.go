package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
)

// TestListAlertsClientIDRoundTrip verifies that the client_id column is
// selected and scanned back, so a tenant-scoped client survives a
// ListAlerts round-trip. The SELECT/Scan omits client_id, which silently
// drops the tenant scope from every listed alert.
func TestListAlertsClientIDRoundTrip(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	clientID := "client-tenant-1"
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "dedup_key", "check_id", "agent_id",
		"site_id", "org_id", "client_id", "alert_rule_id",
		"severity", "state",
		"message", "metadata",
		"acknowledged_by", "snoozed_until",
		"created_at", "updated_at", "resolved_at", "closed_at",
	}).AddRow(
		uuid.NewString(), "dk-1", "check-1", "agent-1",
		"site-1", orgID, clientID, "rule-1",
		"warning", "pending",
		"boom", []byte(`{}`),
		"", nil,
		now, now, nil, nil,
	)

	// The query must select client_id (COALESCE(client_id,'')) and the scan
	// must read it into the same position.
	pool.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery(`SELECT .* FROM alerts`).
		WithArgs(orgID, 50, 0).
		WillReturnRows(rows)

	got, total, err := s.ListAlerts(context.Background(), AlertFilter{OrgID: orgID, Limit: 50})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(got))
	}
	if got[0].ClientID != clientID {
		t.Fatalf("expected ClientID %q, got %q", clientID, got[0].ClientID)
	}
	if got[0].OrgID != orgID {
		t.Fatalf("expected OrgID %q, got %q", orgID, got[0].OrgID)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestListAlertsClientIDEmpty verifies that a NULL client_id column decodes
// to an empty string (COALESCE(client_id,”)), matching the nullable model.
func TestListAlertsClientIDEmpty(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "dedup_key", "check_id", "agent_id",
		"site_id", "org_id", "client_id", "alert_rule_id",
		"severity", "state",
		"message", "metadata",
		"acknowledged_by", "snoozed_until",
		"created_at", "updated_at", "resolved_at", "closed_at",
	}).AddRow(
		uuid.NewString(), "dk-2", "check-2", "agent-2",
		"site-2", orgID, "", "",
		"info", "open",
		"hi", []byte(`{}`),
		"", nil,
		now, now, nil, nil,
	)

	pool.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery(`SELECT .* FROM alerts`).
		WithArgs(orgID, 50, 0).
		WillReturnRows(rows)

	got, _, err := s.ListAlerts(context.Background(), AlertFilter{OrgID: orgID})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(got))
	}
	if got[0].ClientID != "" {
		t.Fatalf("expected empty ClientID for NULL column, got %q", got[0].ClientID)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestListAlertsColumnOrder verifies the SELECT column list and the Scan
// target order match exactly, so a positional mismatch cannot silently
// corrupt the alert struct.
func TestListAlertsColumnOrder(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	// Build the row with the full expected column set, in the order the
	// production query selects them. client_id sits immediately after
	// org_id (the fix), not appended at the end.
	rows := pgxmock.NewRows([]string{
		"id", "dedup_key", "check_id", "agent_id",
		"site_id", "org_id", "client_id", "alert_rule_id",
		"severity", "state",
		"message", "metadata",
		"acknowledged_by", "snoozed_until",
		"created_at", "updated_at", "resolved_at", "closed_at",
	}).AddRow(
		uuid.NewString(), "dk-3", "check-3", "agent-3",
		"site-3", orgID, "client-tenant-3", "rule-3",
		"critical", "acknowledged",
		"msg", []byte(`{"k":"v"}`),
		"actor-1", nil,
		now, now, nil, nil,
	)

	pool.ExpectQuery(`SELECT COUNT\(\*\) FROM alerts WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectQuery(`SELECT .* FROM alerts`).
		WithArgs(orgID, 50, 0).
		WillReturnRows(rows)

	got, _, err := s.ListAlerts(context.Background(), AlertFilter{OrgID: orgID})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(got))
	}
	a := got[0]
	if a.ID == "" || a.DedupKey != "dk-3" || a.CheckID != "check-3" ||
		a.AgentID != "agent-3" || a.SiteID != "site-3" ||
		a.OrgID != orgID || a.ClientID != "client-tenant-3" ||
		a.AlertRuleID != "rule-3" || a.Severity != "critical" ||
		a.State != "acknowledged" || a.Message != "msg" ||
		a.AcknowledgedBy != "actor-1" ||
		a.CreatedAt != now || a.UpdatedAt != now {
		t.Fatalf("column misalignment: %+v", a)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
