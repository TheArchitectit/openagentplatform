package relay

import (
	"context"
	"testing"
)

func TestMatchEngine_Admit_Types(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 10}, nil)
	engine := NewMatchEngine(svc)

	t.Run("unknown_type", func(t *testing.T) {
		msg := RendezvousMsg{Type: "bogus", AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
		leg, _, err := engine.Admit(nil, "agentA", msg)
		if err == nil || leg != nil {
			t.Fatal("expected error for unknown type")
		}
	})
}

func TestMatchEngine_Admit_PrincipalMismatch(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 10}, nil)
	engine := NewMatchEngine(svc)

	msg := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	leg, _, err := engine.Admit(nil, "agentB", msg) // cert says agentB, body says agentA
	if err == nil || leg != nil {
		t.Fatal("expected principal_mismatch error")
	}
}

func TestMatchEngine_Admit_MissingTarget(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 10}, nil)
	engine := NewMatchEngine(svc)

	msg := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "", TenantID: "t1"}
	leg, _, err := engine.Admit(nil, "agentA", msg)
	if err == nil || leg != nil {
		t.Fatal("expected target_id_required error")
	}
}

func TestMatchEngine_Admit_PendingNoMatch(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 10}, nil)
	engine := NewMatchEngine(svc)

	msg := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	leg, partner, err := engine.Admit(nil, "agentA", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if partner != nil {
		t.Fatal("expected nil partner (no counterpart yet)")
	}
	if leg == nil || leg.State != LegPending {
		t.Fatalf("expected LegPending, got %v", leg)
	}
}

func TestMatchEngine_Admit_DuplicateReplaces(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 100}, nil)
	engine := NewMatchEngine(svc)

	msg := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	leg1, _, err := engine.Admit(nil, "agentA", msg)
	if err != nil {
		t.Fatalf("admit 1: %v", err)
	}
	if leg1 == nil || leg1.State != LegPending {
		t.Fatal("first leg should be pending")
	}

	// Second leg for same triple — should replace the first.
	msg2 := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	leg2, _, err := engine.Admit(nil, "agentA", msg2)
	if err != nil {
		t.Fatalf("admit 2: %v", err)
	}
	if leg2 == nil || leg2.State != LegPending {
		t.Fatal("second leg should be pending")
	}
}

func TestMatchEngine_Admit_SymmetricMatch(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 100}, nil)
	engine := NewMatchEngine(svc)

	// Leg A→B (pending)
	msgA := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	legA, _, err := engine.Admit(nil, "agentA", msgA)
	if err != nil {
		t.Fatalf("admit A: %v", err)
	}
	if legA == nil || legA.State != LegPending {
		t.Fatal("legA should be pending")
	}

	// Leg B→A (should match A)
	msgB := RendezvousMsg{Type: RendezvousType, AgentID: "agentB", TargetID: "agentA", TenantID: "t1"}
	legB, partnerA, err := engine.Admit(nil, "agentB", msgB)
	if err != nil {
		t.Fatalf("admit B: %v", err)
	}
	if legB == nil || legB.State != LegMatched {
		t.Fatalf("legB should be LegMatched, got %v", legB)
	}
	if partnerA == nil || partnerA != legA {
		t.Fatal("partnerA should be legA")
	}
	if legA.State != LegMatched {
		t.Fatal("legA should have been promoted to LegMatched")
	}
}

func TestMatchEngine_ClosePair(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 100}, nil)
	engine := NewMatchEngine(svc)
	ctx := context.Background()
	_ = ctx

	// Admit A→B and B→A to get a matched pair.
	msgA := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	legA, _, _ := engine.Admit(nil, "agentA", msgA)
	msgB := RendezvousMsg{Type: RendezvousType, AgentID: "agentB", TargetID: "agentA", TenantID: "t1"}
	legB, _, _ := engine.Admit(nil, "agentB", msgB)

	engine.ClosePair(legA)

	// Both legs should be closed.
	legA.mu.Lock()
	stateA := legA.State
	legA.mu.Unlock()
	legB.mu.Lock()
	stateB := legB.State
	legB.mu.Unlock()

	if stateA != LegClosed || stateB != LegClosed {
		t.Fatalf("expected both closed, got A=%v B=%v", stateA, stateB)
	}
}

func TestMatchEngine_Admit_MaxConnections(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 1}, nil)
	engine := NewMatchEngine(svc)

	msg1 := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	leg1, _, err := engine.Admit(nil, "agentA", msg1)
	if err != nil {
		t.Fatalf("admit 1: %v", err)
	}
	_ = leg1

	// Second leg should hit the connection limit.
	msg2 := RendezvousMsg{Type: RendezvousType, AgentID: "agentA", TargetID: "agentB", TenantID: "t1"}
	leg2, _, err := engine.Admit(nil, "agentA", msg2)
	if err == nil {
		t.Fatal("expected connection limit error")
	}
	_ = leg2
}
