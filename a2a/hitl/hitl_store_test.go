package hitl

import "testing"

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
