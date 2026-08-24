package bridge

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ============================================================
// Deduplicator Tests
// ============================================================

func TestDedupBasic(t *testing.T) {
	d := NewDeduplicator(1 * time.Minute)

	if d.IsDuplicate("key-1", "") {
		t.Error("first seen should not be duplicate")
	}
	if !d.IsDuplicate("key-1", "") {
		t.Error("second seen should be duplicate")
	}
}

func TestDedupEmptyKeyNeverDuplicate(t *testing.T) {
	d := NewDeduplicator(1 * time.Minute)
	if d.IsDuplicate("", "") {
		t.Error("empty key should never be duplicate")
	}
}

func TestDedupExpiry(t *testing.T) {
	d := NewDeduplicator(50 * time.Millisecond)
	d.IsDuplicate("key-1", "")
	time.Sleep(100 * time.Millisecond)
	if d.IsDuplicate("key-1", "") {
		t.Error("expired entry should not be duplicate")
	}
}

func TestDedupTaskIDTracking(t *testing.T) {
	d := NewDeduplicator(1 * time.Minute)
	d.IsDuplicate("key-1", "task-old")
	d.UpdateTaskID("key-1", "task-new")
	if got := d.ExistingTaskID("key-1"); got != "task-new" {
		t.Errorf("expected task-new, got %s", got)
	}
}

func TestDedupPurge(t *testing.T) {
	d := NewDeduplicator(50 * time.Millisecond)
	d.IsDuplicate("key-1", "")
	d.IsDuplicate("key-2", "")
	time.Sleep(100 * time.Millisecond)
	purged := d.Purge()
	if purged != 2 {
		t.Errorf("expected 2 purged, got %d", purged)
	}
	if d.Len() != 0 {
		t.Errorf("expected 0 entries after purge, got %d", d.Len())
	}
}

// ============================================================
// Circuit Breaker Tests
// ============================================================

func TestCircuitBreakerTripAndRecovery(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)

	// Closed initially.
	if cb.State() != CircuitClosed {
		t.Error("should start closed")
	}
	if !cb.Allow() {
		t.Error("closed should allow")
	}

	// Trip.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("should be open after threshold")
	}
	if cb.Allow() {
		t.Error("open should reject")
	}

	// Wait for recovery — first Allow() transitions to half-open.
	time.Sleep(60 * time.Millisecond)
	if cb.Allow() {
		t.Error("transition should not allow")
	}
	// Second Allow() is the probe.
	if !cb.Allow() {
		t.Error("should allow probe in half-open")
	}

	// Success closes circuit.
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Error("should be closed after success")
	}
}

func TestCircuitBreakerHalfOpenRejectsMultiple(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure() // trip
	time.Sleep(60 * time.Millisecond)

	// First Allow() transitions to HalfOpen but does NOT allow a request.
	if cb.Allow() {
		t.Error("transition call should not allow")
	}
	// Second Allow() is the probe — allowed once.
	if !cb.Allow() {
		t.Error("probe should be allowed")
	}
	// Third Allow() — probe already sent, rejected.
	if cb.Allow() {
		t.Error("second probe should be rejected")
	}
}

// ============================================================
// Rate Limiter Tests
// ============================================================

func TestRateLimiterPerType(t *testing.T) {
	rl := NewRateLimiter(2, 100)

	if !rl.Allow(EventCheckFailure) {
		t.Error("should allow 1st")
	}
	if !rl.Allow(EventCheckFailure) {
		t.Error("should allow 2nd")
	}
	if rl.Allow(EventCheckFailure) {
		t.Error("should reject 3rd (per-type limit)")
	}

	// Different type should still be allowed.
	if !rl.Allow(EventAlertFired) {
		t.Error("different type should be allowed")
	}
}

func TestRateLimiterAggregate(t *testing.T) {
	rl := NewRateLimiter(100, 3)

	if !rl.Allow(EventCheckFailure) {
		t.Error("1")
	}
	if !rl.Allow(EventAlertFired) {
		t.Error("2")
	}
	if !rl.Allow(EventPatchAvailable) {
		t.Error("3")
	}
	if rl.Allow(EventScriptError) {
		t.Error("should reject (aggregate limit)")
	}
}

// ============================================================
// Metrics Test
// ============================================================

func TestMetricsAccumulate(t *testing.T) {
	creator := newMockCreator(0)
	bridge := NewEventBridge(testBridgeConfig(), DefaultMappings(), creator)

	evt := testEvent(EventCheckFailure, "high")
	evt.ID = "m1"
	evt.DedupKey = "md-1"
	bridge.ProcessEvent(context.Background(), evt)

	evt2 := testEvent(EventCheckFailure, "high")
	evt2.ID = "m2"
	evt2.DedupKey = "md-2"
	bridge.ProcessEvent(context.Background(), evt2)

	// Duplicate of first.
	evt3 := testEvent(EventCheckFailure, "high")
	evt3.ID = "m1"
	evt3.DedupKey = "md-1"
	bridge.ProcessEvent(context.Background(), evt3)

	metrics := bridge.Metrics()
	if metrics["events_consumed"] != 3 {
		t.Errorf("expected 3 consumed, got %d", metrics["events_consumed"])
	}
	if metrics["tasks_created"] != 2 {
		t.Errorf("expected 2 tasks, got %d", metrics["tasks_created"])
	}
	if metrics["events_deduped"] != 1 {
		t.Errorf("expected 1 deduped, got %d", metrics["events_deduped"])
	}
}

// ============================================================
// Concurrent Processing Test
// ============================================================

func TestProcessEventConcurrent(t *testing.T) {
	cfg := testBridgeConfig()
	cfg.RateLimitPerType = 100
	cfg.RateLimitAggregate = 100
	creator := newMockCreator(0)
	bridge := NewEventBridge(cfg, DefaultMappings(), creator)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			evt := testEvent(EventCheckFailure, "high")
			evt.ID = fmt.Sprintf("conc-%d", n)
			evt.DedupKey = fmt.Sprintf("cd-%d", n)
			_, _ = bridge.ProcessEvent(context.Background(), evt)
		}(i)
	}
	wg.Wait()

	metrics := bridge.Metrics()
	if metrics["tasks_created"] != 20 {
		t.Errorf("expected 20 tasks, got %d", metrics["tasks_created"])
	}
}
