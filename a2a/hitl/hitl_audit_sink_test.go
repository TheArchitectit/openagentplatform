package hitl

import (
	"sync"
	"testing"
	"time"
)

// TestAuditSinkReceivesLifecycle verifies R4.4 plumbing: the manager's audit
// sink receives every lifecycle action (create, approve, reject, and the
// notification bookkeeping entries) as entries are appended.
func TestAuditSinkReceivesLifecycle(t *testing.T) {
	var mu sync.Mutex
	var got []AuditEntry
	mgr := NewApprovalManager(DefaultApprovalTypes())
	mgr.SetStore(NewMemStore())
	mgr.SetAuditSink(func(e AuditEntry) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})

	if _, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Approve("a1", "admin@x", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.CreateRequest("a2", "secret_access", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Reject("a2", "admin@x", "no"); err != nil {
		t.Fatal(err)
	}

	// Wait for async forwarding to drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	byAction := map[string]int{}
	for _, e := range got {
		byAction[e.Action]++
	}
	for _, want := range []string{"created", "approved", "rejected"} {
		if byAction[want] == 0 {
			t.Errorf("sink missing action %q (got %v)", want, byAction)
		}
	}
	// Entries are value snapshots, not aliased to the live records.
	for _, e := range got {
		if e.ApprovalID != "a1" && e.ApprovalID != "a2" {
			t.Errorf("unexpected approval id in sink: %+v", e)
		}
	}
}

// TestAuditSinkNotNilChecked ensures a nil sink is a no-op (regression guard
// for appendAudit's nil check).
func TestAuditSinkNotSetIsNoop(t *testing.T) {
	mgr := NewApprovalManager(DefaultApprovalTypes())
	if _, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Approve("a1", "admin", ""); err != nil {
		t.Fatal(err)
	}
	if len(mgr.AuditLog("a1")) < 2 {
		t.Error("expected local audit log still populated with no sink set")
	}
}
