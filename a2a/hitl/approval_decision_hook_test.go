// Package hitl - decision hook tests (spec R5 wiring seam). The gateway
// installs a decision hook via AddDecisionHook; these tests pin the
// engine-side contract: hooks fire asynchronously with a value snapshot on
// every terminal decision (approved, rejected, expired-by-timeout) and
// never fire on non-terminal transitions.
package hitl

import (
	"sync"
	"testing"
	"time"
)

// hookRecorder collects decision-hook snapshots with a mutex.
type hookRecorder struct {
	mu       sync.Mutex
	snapshot []ApprovalRequest
}

func (r *hookRecorder) fn(req ApprovalRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshot = append(r.snapshot, req)
}

func (r *hookRecorder) wait(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.snapshot)
		r.mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("decision hook saw %d calls, want >= %d", len(r.snapshot), want)
}

func (r *hookRecorder) last(t *testing.T) ApprovalRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.snapshot) == 0 {
		t.Fatal("no decision hook snapshots")
	}
	return r.snapshot[len(r.snapshot)-1]
}

func TestDecisionHookFiresOnApprove(t *testing.T) {
	am := NewApprovalManager(DefaultApprovalTypes())
	rec := &hookRecorder{}
	am.AddDecisionHook(rec.fn)

	if _, err := am.CreateRequest("a1", "secret_access", "agent-1", "high", "task-9", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if err := am.Approve("a1", "admin@corp", "ship it"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	rec.wait(t, 1)
	got := rec.last(t)
	if got.Status != StatusApproved || got.DecidedBy != "admin@corp" || got.DecisionNote != "ship it" {
		t.Fatalf("snapshot = %+v, want approved/admin@corp/ship it", got)
	}
	if got.TaskID != "task-9" || got.ID != "a1" {
		t.Fatalf("snapshot linkage = %q/%q, want task-9/a1", got.TaskID, got.ID)
	}
}

func TestDecisionHookFiresOnReject(t *testing.T) {
	am := NewApprovalManager(DefaultApprovalTypes())
	rec := &hookRecorder{}
	am.AddDecisionHook(rec.fn)

	if _, err := am.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if err := am.Reject("a1", "admin@corp", "no change window"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	rec.wait(t, 1)
	got := rec.last(t)
	if got.Status != StatusRejected || got.DecisionNote != "no change window" {
		t.Fatalf("snapshot = %+v, want rejected/no change window", got)
	}
}

func TestDecisionHookFiresOnExpiry(t *testing.T) {
	types := []ApprovalTypeConfig{{Type: "quick", TimeoutDuration: 10 * time.Millisecond, OnTimeout: "reject"}}
	am := NewApprovalManager(types)
	rec := &hookRecorder{}
	am.AddDecisionHook(rec.fn)

	if _, err := am.CreateRequest("a1", "quick", "agent-1", "low", "task-1", nil); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	ee := NewEscalationEngine(am, time.Second)
	// Drive one expiry sweep directly (no ticker wait needed).
	ee.checkExpired(time.Now().Add(time.Second))
	rec.wait(t, 1)
	got := rec.last(t)
	if got.Status != StatusExpired {
		t.Fatalf("snapshot status = %q, want expired", got.Status)
	}
	if got.DecisionNote != "timeout exceeded" {
		t.Fatalf("snapshot note = %q, want timeout exceeded", got.DecisionNote)
	}
}

func TestDecisionHookNotFiredOnEscalate(t *testing.T) {
	types := []ApprovalTypeConfig{{
		Type: "slow", TimeoutDuration: 10 * time.Millisecond,
		OnTimeout: "escalate", MaxEscalations: 2, EscalationGroups: []string{"g1", "g2"},
	}}
	am := NewApprovalManager(types)
	rec := &hookRecorder{}
	am.AddDecisionHook(rec.fn)

	if _, err := am.CreateRequest("a1", "slow", "agent-1", "low", "task-1", nil); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	ee := NewEscalationEngine(am, time.Second)
	ee.checkExpired(time.Now().Add(time.Second))

	// Escalation re-arms to pending (non-terminal): no hook call. Give the
	// async path a chance to misfire, then assert nothing arrived.
	time.Sleep(100 * time.Millisecond)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.snapshot) != 0 {
		t.Fatalf("hook fired %d time(s) on non-terminal escalate, want 0", len(rec.snapshot))
	}
}

func TestDecisionHookMultipleHooks(t *testing.T) {
	am := NewApprovalManager(DefaultApprovalTypes())
	var wg sync.WaitGroup
	wg.Add(2)
	var mu sync.Mutex
	seen := map[string]int{}
	am.AddDecisionHook(func(ApprovalRequest) { mu.Lock(); seen["first"]++; mu.Unlock(); wg.Done() })
	am.AddDecisionHook(func(ApprovalRequest) { mu.Lock(); seen["second"]++; mu.Unlock(); wg.Done() })

	if _, err := am.CreateRequest("a1", "patch_deploy", "agent-1", "high", "", nil); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if err := am.Approve("a1", "admin", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hooks did not both fire")
	}
	if seen["first"] != 1 || seen["second"] != 1 {
		t.Fatalf("hook counts = %+v, want one call each", seen)
	}
}
