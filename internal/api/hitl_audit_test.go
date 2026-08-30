package api

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/audit"
)

// openAuditPool returns a live pgxpool against OAP_TEST_PG_DSN, skipping when
// unset. The audit_events table must exist (internal/db/migrations/002).
func openAuditPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("OAP_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OAP_TEST_PG_DSN not set; skipping live database audit tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open audit pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping audit pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestHITLAuditIntegration is the R4.1-R4.4 end-to-end proof: a full
// approval lifecycle (create → approve) writes hash-chained events into
// the platform audit trail, queryable by approval id (R4.3).
func TestHITLAuditIntegration(t *testing.T) {
	pool := openAuditPool(t)
	svc := audit.New(pool)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	mgr := hitl.NewApprovalManager(hitl.DefaultApprovalTypes())
	mgr.SetStore(hitl.NewMemStore())
	WireHITLAudit(mgr, svc, log)

	id := "hitl-test-" + uuid.NewString()
	if _, err := mgr.CreateRequest(id, "secret_access", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Reject(id, "admin@example.com", "not in change window"); err != nil {
		t.Fatal(err)
	}

	// Wait for the async sink writes to land.
	deadline := time.Now().Add(5 * time.Second)
	var events []audit.Event
	for time.Now().Before(deadline) {
		var err error
		events, _, err = svc.GetEvents(context.Background(), audit.EventFilter{
			ResourceType: "hitl_approval",
			ResourceID:   id,
		})
		if err != nil {
			t.Fatalf("query audit events: %v", err)
		}
		if len(events) >= 3 { // created + rejected (+ notified if a notifier was wired)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	byAction := map[string]audit.Event{}
	for _, e := range events {
		byAction[e.Action] = e
	}
	if _, ok := byAction["hitl.created"]; !ok {
		t.Errorf("missing hitl.created audit event (actions: %v)", actionsOf(events))
	}
	rej, ok := byAction["hitl.rejected"]
	if !ok {
		t.Fatalf("missing hitl.rejected audit event (actions: %v)", actionsOf(events))
	}
	// R4.2: rejection justification carried in the audit details.
	if rej.Outcome != audit.OutcomeDenied {
		t.Errorf("rejected outcome = %s, want denied", rej.Outcome)
	}
	if rej.ActorID != "admin@example.com" || rej.ActorType != audit.ActorUser {
		t.Errorf("rejected actor = %s/%s, want admin@example.com/user", rej.ActorType, rej.ActorID)
	}
	// R4.1: timestamps present and ordered.
	if byAction["hitl.created"].Timestamp.After(rej.Timestamp.Add(time.Second)) {
		t.Error("created event timestamp after rejected")
	}

	// R4.3: the chain endpoint verifies tamper-evidence for this approval.
	ver, err := svc.GetEventChain(context.Background(), id)
	if err != nil {
		t.Fatalf("get chain: %v", err)
	}
	if !ver.Intact {
		t.Errorf("audit chain not intact: broken_at=%s", ver.BrokenAt)
	}
	if ver.TotalChecked < 2 {
		t.Errorf("chain covers %d events, want >= 2", ver.TotalChecked)
	}
}

func actionsOf(events []audit.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Action
	}
	return out
}
