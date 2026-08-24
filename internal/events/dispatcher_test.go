package events

import "testing"

// --- NewCheckDispatcher nil-safe construction ---

func TestNewCheckDispatcher_NilLogger(t *testing.T) {
	d := NewCheckDispatcher(nil, nil, nil, nil)
	if d == nil {
		t.Fatal("NewCheckDispatcher returned nil")
	}
	if d.stopCh == nil {
		t.Error("stopCh should be initialized")
	}
}

// --- CheckDispatcher Start / AssignCheck nil client paths ---

func TestCheckDispatcher_StartNilClient(t *testing.T) {
	d := NewCheckDispatcher(nil, nil, nil, nil)
	err := d.Start(nil)
	if err == nil {
		t.Error("Start with nil client should return error")
	}
}

func TestCheckDispatcher_AssignCheckNilClient(t *testing.T) {
	d := NewCheckDispatcher(nil, nil, nil, nil)
	err := d.AssignCheck(nil, "agent-1", "test")
	if err == nil {
		t.Error("AssignCheck with nil client should return error")
	}
}
