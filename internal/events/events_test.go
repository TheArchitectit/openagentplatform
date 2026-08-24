package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

// --- agentIDFromSubject tests ---

func TestAgentIDFromSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"oap.agents.agent-1.heartbeat", "agent-1"},
		{"oap.agents.a1.heartbeat", "a1"},
		{"oap.agents.my.dotted.id.heartbeat", "my.dotted.id"},
		// Too few parts
		{"oap.agents.heartbeat", ""},
		{"oap.agents", ""},
		{"oap", ""},
		{"", ""},
		// Wrong prefix
		{"wrong.agents.a1.heartbeat", ""},
		{"oap.agents.a1.other", "a1"}, // technically "a1" between agents and last
		// Edge: just "oap.agents.X" (3 parts)
		{"oap.agents.x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got := agentIDFromSubject(tt.subject)
			if got != tt.want {
				t.Errorf("agentIDFromSubject(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}


// --- CheckAssignmentSubject test ---

func TestCheckAssignmentSubject(t *testing.T) {
	got := CheckAssignmentSubject("agent-42")
	want := "oap.agents.agent-42.checks"
	if got != want {
		t.Errorf("CheckAssignmentSubject = %q, want %q", got, want)
	}
}

// --- HeartbeatHandler markOnline / markOffline / onlineAgentIDs ---

func TestHeartbeatHandler_MarkOnlineOffline(t *testing.T) {
	h := NewHeartbeatHandler(nil, nil, nil)

	// Initially empty.
	if ids := h.onlineAgentIDs(); len(ids) != 0 {
		t.Errorf("initial onlineAgentIDs = %v, want empty", ids)
	}

	// Mark online.
	h.markOnline("agent-1")
	h.markOnline("agent-2")
	ids := h.onlineAgentIDs()
	if len(ids) != 2 {
		t.Fatalf("onlineAgentIDs after markOnline = %d, want 2", len(ids))
	}
	idMap := map[string]bool{}
	for _, id := range ids {
		idMap[id] = true
	}
	if !idMap["agent-1"] || !idMap["agent-2"] {
		t.Errorf("expected agent-1 and agent-2 online, got %v", ids)
	}

	// Mark offline.
	h.markOffline("agent-1")
	ids = h.onlineAgentIDs()
	if len(ids) != 1 {
		t.Fatalf("onlineAgentIDs after markOffline = %d, want 1", len(ids))
	}
	if ids[0] != "agent-2" {
		t.Errorf("expected agent-2 still online, got %v", ids)
	}

	// Mark offline nonexistent is a no-op.
	h.markOffline("agent-999")
	ids = h.onlineAgentIDs()
	if len(ids) != 1 {
		t.Errorf("markOffline nonexistent changed count to %d", len(ids))
	}
}

func TestHeartbeatHandler_MarkOnlineIdempotent(t *testing.T) {
	h := NewHeartbeatHandler(nil, nil, nil)
	h.markOnline("agent-1")
	h.markOnline("agent-1")
	ids := h.onlineAgentIDs()
	if len(ids) != 1 {
		t.Errorf("duplicate markOnline should be idempotent, got %d", len(ids))
	}
}

// --- NewHeartbeatHandler / NewCheckDispatcher nil-safe construction ---

func TestNewHeartbeatHandler_NilLogger(t *testing.T) {
	h := NewHeartbeatHandler(nil, nil, nil)
	if h == nil {
		t.Fatal("NewHeartbeatHandler returned nil")
	}
	if h.online == nil {
		t.Error("online map should be initialized")
	}
	if h.stopCh == nil {
		t.Error("stopCh should be initialized")
	}
}

func TestNewCheckDispatcher_NilLogger(t *testing.T) {
	d := NewCheckDispatcher(nil, nil, nil, nil)
	if d == nil {
		t.Fatal("NewCheckDispatcher returned nil")
	}
	if d.stopCh == nil {
		t.Error("stopCh should be initialized")
	}
}

// --- Client.Close idempotency ---

func TestClient_CloseNil(t *testing.T) {
	// Should not panic.
	var c *Client
	c.Close()
}

func TestClient_CloseIdempotent(t *testing.T) {
	// We can't easily create a real NATS client in tests without a server,
	// but we can test that Close is safe to call on a zero-value Client
	// (nil conn).
	c := &Client{log: nil}
	// Close should handle nil conn gracefully.
	// Note: Close calls c.conn.Drain() when conn != nil, so with conn == nil
	// it should return early after draining subs (which is nil).
	c.Close()
	c.Close() // second call should not panic
}

func TestClient_IsConnected_NilClient(t *testing.T) {
	var c *Client
	if c.IsConnected() {
		t.Error("nil client should report not connected")
	}
}

func TestClient_IsConnected_NilConn(t *testing.T) {
	c := &Client{}
	if c.IsConnected() {
		t.Error("client with nil conn should report not connected")
	}
}

// --- NewHeaderCarrier tests ---

func TestNewHeaderCarrier(t *testing.T) {
	carrier := NewHeaderCarrier(nil)
	carrier.Set("key1", "val1")
	if got := carrier.Get("key1"); got != "val1" {
		t.Errorf("Get = %q, want %q", got, "val1")
	}
	keys := carrier.Keys()
	if len(keys) != 1 || keys[0] != "key1" {
		t.Errorf("Keys = %v, want [key1]", keys)
	}
}

func TestNewHeaderCarrier_NilHeader(t *testing.T) {
	carrier := NewHeaderCarrier(nil)
	// Should still work: creates internal header.
	carrier.Set("foo", "bar")
	if got := carrier.Get("foo"); got != "bar" {
		t.Errorf("Get = %q, want %q", got, "bar")
	}
}

// --- HeartbeatHandler concurrent markOnline/markOffline ---

func TestHeartbeatHandler_ConcurrentAccess(t *testing.T) {
	h := NewHeartbeatHandler(nil, nil, nil)
	var wg sync.WaitGroup
	n := 100

	// Concurrent markOnline.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			h.markOnline("agent-" + string(rune('A'+id%26)))
		}(i)
	}
	wg.Wait()

	ids := h.onlineAgentIDs()
	if len(ids) == 0 {
		t.Error("expected some online agents after concurrent markOnline")
	}

	// Concurrent markOffline + onlineAgentIDs.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			h.markOffline("agent-" + string(rune('A'+id%26)))
			_ = h.onlineAgentIDs()
		}(i)
	}
	wg.Wait()
}

// --- Constants exist and have expected values ---

func TestConstants(t *testing.T) {
	if SubjectHeartbeatPrefix != "oap.agents.*.heartbeat" {
		t.Errorf("SubjectHeartbeatPrefix = %q", SubjectHeartbeatPrefix)
	}
	if SubjectCheckResultsPrefix != "oap.agents.*.results" {
		t.Errorf("SubjectCheckResultsPrefix = %q", SubjectCheckResultsPrefix)
	}
	if SubjectCheckResultPrefix != SubjectCheckResultsPrefix {
		t.Error("SubjectCheckResultPrefix should be alias for SubjectCheckResultsPrefix")
	}
	if SubjectAgentEvents != "oap.events.agent" {
		t.Errorf("SubjectAgentEvents = %q", SubjectAgentEvents)
	}
	if SubjectAlertEvents != "oap.events.alerts" {
		t.Errorf("SubjectAlertEvents = %q", SubjectAlertEvents)
	}
	if SubjectCheckResultEvent != "oap.events.checks.result" {
		t.Errorf("SubjectCheckResultEvent = %q", SubjectCheckResultEvent)
	}
	if SubjectPatchEvents != "oap.events.patches" {
		t.Errorf("SubjectPatchEvents = %q", SubjectPatchEvents)
	}
	if SubjectScriptEvents != "oap.events.scripts" {
		t.Errorf("SubjectScriptEvents = %q", SubjectScriptEvents)
	}
	if HeartbeatStaleThreshold != 120*1_000_000_000 {
		t.Errorf("HeartbeatStaleThreshold = %d", HeartbeatStaleThreshold)
	}
}

// --- previousStatus nil store ---

func TestHeartbeatHandler_PreviousStatus_NilStore(t *testing.T) {
	h := NewHeartbeatHandler(nil, nil, nil)
	status := h.previousStatus(nil, "agent-1")
	if status != "" {
		t.Errorf("previousStatus with nil store = %q, want empty", status)
	}
}

// --- Start / Stop error paths ---

func TestHeartbeatHandler_StartNilClient(t *testing.T) {
	h := NewHeartbeatHandler(nil, nil, nil)
	err := h.Start(nil)
	if err == nil {
		t.Error("Start with nil client should return error")
	}
}

func TestCheckDispatcher_StartNilClient(t *testing.T) {
	d := NewCheckDispatcher(nil, nil, nil, nil)
	err := d.Start(nil)
	if err == nil {
		t.Error("Start with nil client should return error")
	}
}

// --- AssignCheck nil client ---

func TestCheckDispatcher_AssignCheckNilClient(t *testing.T) {
	d := NewCheckDispatcher(nil, nil, nil, nil)
	err := d.AssignCheck(nil, "agent-1", "test")
	if err == nil {
		t.Error("AssignCheck with nil client should return error")
	}
}

// --- sweepStale context cancellation ---

func TestHeartbeatHandler_SweepStale_ContextCancel(t *testing.T) {
	// Verify that sweepStale returns when context is cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	h := NewHeartbeatHandler(nil, nil, nil)
	h.wg.Add(1)
	go h.sweepStale(ctx)
	cancel()
	h.wg.Wait() // should not deadlock
}

// Use time.Sleep-based approach for sweepStale cancellation.
func TestHeartbeatHandler_SweepStale_Stop(t *testing.T) {
	// This tests that the handler can be constructed and its stop channel
	// can be closed without panic.
	h := NewHeartbeatHandler(nil, nil, nil)
	close(h.stopCh)
	// stopCh is now closed; select on it should return immediately.
	select {
	case <-h.stopCh:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Error("stopCh should be immediately readable after close")
	}
}
