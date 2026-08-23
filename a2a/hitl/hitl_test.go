package hitl

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ============================================================
// Approval Manager Tests
// ============================================================

func testManager() *ApprovalManager {
	return NewApprovalManager(DefaultApprovalTypes())
}

func TestCreateApproval(t *testing.T) {
	mgr := testManager()
	req, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "task-1",
		map[string]any{"secret": "db-password"})
	if err != nil {
		t.Fatal(err)
	}
	if req.ID != "a1" {
		t.Errorf("expected ID a1, got %s", req.ID)
	}
	if req.Status != StatusPending {
		t.Errorf("expected pending, got %s", req.Status)
	}
	if req.ActionType != "secret_access" {
		t.Errorf("expected secret_access, got %s", req.ActionType)
	}
}

func TestCreateApprovalUnknownType(t *testing.T) {
	mgr := testManager()
	_, err := mgr.CreateRequest("a1", "unknown_type", "agent-1", "high", "", nil)
	if err == nil {
		t.Error("expected error for unknown action type")
	}
}

func TestApprove(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)

	err := mgr.Approve("a1", "admin-1", "looks safe")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := mgr.GetRequest("a1")
	if req.Status != StatusApproved {
		t.Errorf("expected approved, got %s", req.Status)
	}
	if req.DecidedBy != "admin-1" {
		t.Errorf("expected admin-1, got %s", req.DecidedBy)
	}
	if req.DecisionNote != "looks safe" {
		t.Errorf("expected 'looks safe', got %s", req.DecisionNote)
	}
}

func TestRejectRequiresReason(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)

	err := mgr.Reject("a1", "admin-1", "")
	if err == nil {
		t.Error("expected error for empty rejection reason")
	}
}

func TestReject(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)

	err := mgr.Reject("a1", "admin-1", "too risky")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := mgr.GetRequest("a1")
	if req.Status != StatusRejected {
		t.Errorf("expected rejected, got %s", req.Status)
	}
}

func TestDoubleDecideRejected(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)

	mgr.Approve("a1", "admin-1", "ok")
	err := mgr.Approve("a1", "admin-2", "also ok")
	if err != ErrAlreadyDecided {
		t.Errorf("expected ErrAlreadyDecided, got %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	mgr := testManager()
	_, err := mgr.GetRequest("nonexistent")
	if err != ErrApprovalNotFound {
		t.Errorf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestListPending(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)
	mgr.CreateRequest("a2", "patch_deploy", "agent-2", "medium", "", nil)
	mgr.CreateRequest("a3", "external_api", "agent-3", "low", "", nil)
	mgr.Approve("a2", "admin-1", "ok")

	pending := mgr.ListPending()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}
}

func TestListByStatus(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)
	mgr.CreateRequest("a2", "secret_access", "agent-1", "high", "", nil)
	mgr.Reject("a1", "admin-1", "no")

	rejected := mgr.ListByStatus(StatusRejected)
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
}

// ============================================================
// Audit Trail Tests
// ============================================================

func TestAuditLogCreated(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)

	entries := mgr.AuditLog("a1")
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].Action != "created" {
		t.Errorf("expected 'created', got %s", entries[0].Action)
	}
}

func TestAuditLogApprove(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)
	mgr.Approve("a1", "admin-1", "approved")

	entries := mgr.AuditLog("a1")
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}
	if entries[1].Action != "approved" {
		t.Errorf("expected 'approved', got %s", entries[1].Action)
	}
	if entries[1].Actor != "admin-1" {
		t.Errorf("expected actor admin-1, got %s", entries[1].Actor)
	}
}

func TestAuditLogReject(t *testing.T) {
	mgr := testManager()
	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)
	mgr.Reject("a1", "admin-1", "too risky")

	entries := mgr.AuditLog("a1")
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}
	if entries[1].Reason != "too risky" {
		t.Errorf("expected reason 'too risky', got %s", entries[1].Reason)
	}
}

// ============================================================
// Store Integration Tests
// ============================================================

func TestStoreSaveAndGet(t *testing.T) {
	store := NewMemStore()
	mgr := testManager()
	mgr.SetStore(store)

	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)

	saved, err := store.GetApproval("a1")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != StatusPending {
		t.Errorf("expected pending in store, got %s", saved.Status)
	}
}

func TestStoreAuditTrail(t *testing.T) {
	store := NewMemStore()
	mgr := testManager()
	mgr.SetStore(store)

	mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil)
	mgr.Approve("a1", "admin-1", "ok")

	entries, err := store.GetAuditLog("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 audit entries in store, got %d", len(entries))
	}
}

// ============================================================
// Escalation Engine Tests
// ============================================================

func TestEscalationAutoReject(t *testing.T) {
	typeCfgs := []ApprovalTypeConfig{
		{Type: "test_type", TimeoutDuration: 10 * time.Millisecond, OnTimeout: "reject", MaxEscalations: 2},
	}
	mgr := NewApprovalManager(typeCfgs)
	mgr.CreateRequest("a1", "test_type", "agent-1", "high", "", nil)

	time.Sleep(20 * time.Millisecond)

	engine := NewEscalationEngine(mgr, 10*time.Millisecond)
	engine.Start(context.Background())
	time.Sleep(15 * time.Millisecond)
	engine.Stop()

	req, _ := mgr.GetRequest("a1")
	if req.Status != StatusExpired {
		t.Errorf("expected expired, got %s", req.Status)
	}
}

func TestEscalationEscalate(t *testing.T) {
	typeCfgs := []ApprovalTypeConfig{
		{Type: "test_type", TimeoutDuration: 10 * time.Millisecond, OnTimeout: "escalate", MaxEscalations: 3},
	}
	mgr := NewApprovalManager(typeCfgs)
	mgr.CreateRequest("a1", "test_type", "agent-1", "high", "", nil)

	time.Sleep(20 * time.Millisecond)

	engine := NewEscalationEngine(mgr, 10*time.Millisecond)
	engine.Start(context.Background())
	time.Sleep(15 * time.Millisecond)
	engine.Stop()

	req, _ := mgr.GetRequest("a1")
	if req.EscalationDepth != 1 {
		t.Errorf("expected escalation depth 1, got %d", req.EscalationDepth)
	}

	entries := mgr.AuditLog("a1")
	found := false
	for _, e := range entries {
		if e.Action == "escalated" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected escalation audit entry")
	}
}

func TestEscalationMaxDepthAutoReject(t *testing.T) {
	typeCfgs := []ApprovalTypeConfig{
		{Type: "test_type", TimeoutDuration: 5 * time.Millisecond, OnTimeout: "escalate", MaxEscalations: 0},
	}
	mgr := NewApprovalManager(typeCfgs)
	mgr.CreateRequest("a1", "test_type", "agent-1", "high", "", nil)

	time.Sleep(10 * time.Millisecond)

	engine := NewEscalationEngine(mgr, 5*time.Millisecond)
	engine.Start(context.Background())
	time.Sleep(15 * time.Millisecond)
	engine.Stop()

	req, _ := mgr.GetRequest("a1")
	if req.Status != StatusExpired {
		t.Errorf("expected expired at max depth, got %s", req.Status)
	}
}

// ============================================================
// Approval Type Config Tests
// ============================================================

func TestDefaultApprovalTypes(t *testing.T) {
	types := DefaultApprovalTypes()
	if len(types) != 6 {
		t.Errorf("expected 6 default types, got %d", len(types))
	}

	seen := make(map[string]bool)
	for _, at := range types {
		if seen[at.Type] {
			t.Errorf("duplicate type: %s", at.Type)
		}
		seen[at.Type] = true
		if at.TimeoutDuration <= 0 {
			t.Errorf("type %s has non-positive timeout", at.Type)
		}
		if at.MaxEscalations < 0 {
			t.Errorf("type %s has negative max escalations", at.Type)
		}
	}
}

// ============================================================
// Concurrent Safety Test
// ============================================================

func TestConcurrentAccess(t *testing.T) {
	mgr := testManager()

	// Create 20 approvals concurrently.
	for i := 0; i < 20; i++ {
		go func(n int) {
			id := fmt.Sprintf("a%d", n)
			mgr.CreateRequest(id, "secret_access", "agent-1", "high", "", nil)
		}(i)
	}

	time.Sleep(50 * time.Millisecond)

	pending := mgr.ListPending()
	if len(pending) != 20 {
		t.Errorf("expected 20 pending, got %d", len(pending))
	}
}

// ============================================================
// State Transition Tests
// ============================================================

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status   ApprovalStatus
		terminal bool
	}{
		{StatusPending, false},
		{StatusApproved, true},
		{StatusRejected, true},
		{StatusExpired, true},
		{StatusEscalated, false},
	}
	for _, tt := range tests {
		req := &ApprovalRequest{Status: tt.status}
		if req.IsTerminal() != tt.terminal {
			t.Errorf("status %s: expected terminal=%v", tt.status, tt.terminal)
		}
	}
}
