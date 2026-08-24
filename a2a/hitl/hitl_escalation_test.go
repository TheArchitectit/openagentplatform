package hitl

import (
	"context"
	"testing"
	"time"
)

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
