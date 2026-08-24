package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

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

// --- NewHeartbeatHandler nil-safe construction ---

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
