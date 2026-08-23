package bridge

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ============================================================
// Test helpers
// ============================================================

// mockCreator records CreateTask calls and returns a synthetic ID.
type mockCreator struct {
	mu    sync.Mutex
	calls int
	failN int // first N calls fail
}

func newMockCreator(failN int) *mockCreator {
	return &mockCreator{failN: failN}
}

func (m *mockCreator) CreateTask(_ context.Context, _ *TaskRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calls <= m.failN {
		return "", fmt.Errorf("simulated failure #%d", m.calls)
	}
	return fmt.Sprintf("task-%d", m.calls), nil
}

func (m *mockCreator) UpdateTask(_ context.Context, _ string, _ []Part) error {
	return nil
}

func (m *mockCreator) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func testEvent(typ RMMEventType, severity string) *RMMEvent {
	return &RMMEvent{
		ID:               fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:             typ,
		Severity:         severity,
		SourceAgentID:    "agent-1",
		AffectedResource: "server-1",
		Timestamp:        time.Now(),
	}
}

func testBridgeConfig() EventBridgeConfig {
	cfg := DefaultEventBridgeConfig()
	cfg.DedupWindow = 1 * time.Minute
	cfg.RateLimitPerType = 5
	cfg.RateLimitAggregate = 10
	cfg.CircuitThreshold = 3
	cfg.MaxRetries = 1
	return cfg
}

// ============================================================
// Event Mapping Tests
// ============================================================

func TestEventMappingsAllCovered(t *testing.T) {
	mappings := DefaultMappings()
	reg := NewMappingRegistry(mappings)

	for _, et := range AllEventTypes() {
		m, ok := reg.Lookup(et)
		if !ok {
			t.Errorf("event type %q has no mapping", et)
		}
		if m.SkillTag == "" {
			t.Errorf("event type %q has empty skill tag", et)
		}
	}
}

func TestDisabledMappingIgnored(t *testing.T) {
	mappings := DefaultMappings()
	mappings[0].Enabled = false
	reg := NewMappingRegistry(mappings)

	_, ok := reg.Lookup(mappings[0].EventType)
	if ok {
		t.Error("disabled mapping should not be found")
	}
}

func TestUnknownEventTypeIgnored(t *testing.T) {
	reg := NewMappingRegistry(DefaultMappings())
	_, ok := reg.Lookup("nonexistent_event")
	if ok {
		t.Error("unknown event type should not be found")
	}
}

func TestSetEnabled(t *testing.T) {
	reg := NewMappingRegistry(DefaultMappings())
	if err := reg.SetEnabled(EventCheckFailure, false); err != nil {
		t.Fatal(err)
	}
	_, ok := reg.Lookup(EventCheckFailure)
	if ok {
		t.Error("should be disabled")
	}
	if err := reg.SetEnabled(EventCheckFailure, true); err != nil {
		t.Fatal(err)
	}
	_, ok = reg.Lookup(EventCheckFailure)
	if !ok {
		t.Error("should be re-enabled")
	}
}

func TestSetEnabledUnknownType(t *testing.T) {
	reg := NewMappingRegistry(DefaultMappings())
	err := reg.SetEnabled("nope", true)
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

// ============================================================
// Bridge Processing Tests
// ============================================================

func TestProcessEventCreatesTask(t *testing.T) {
	creator := newMockCreator(0)
	bridge := NewEventBridge(testBridgeConfig(), DefaultMappings(), creator)

	taskID, err := bridge.ProcessEvent(context.Background(), testEvent(EventCheckFailure, "high"))
	if err != nil {
		t.Fatal(err)
	}
	if taskID == "" {
		t.Error("expected non-empty task ID")
	}
	if creator.CallCount() != 1 {
		t.Errorf("expected 1 create call, got %d", creator.CallCount())
	}
}

func TestProcessEventUnknownTypeIgnored(t *testing.T) {
	creator := newMockCreator(0)
	bridge := NewEventBridge(testBridgeConfig(), DefaultMappings(), creator)

	taskID, err := bridge.ProcessEvent(context.Background(), testEvent("unknown_type", "high"))
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "" {
		t.Error("unknown type should produce empty task ID")
	}
	if creator.CallCount() != 0 {
		t.Error("should not create task for unknown type")
	}
}

func TestProcessEventDeduplication(t *testing.T) {
	creator := newMockCreator(0)
	bridge := NewEventBridge(testBridgeConfig(), DefaultMappings(), creator)

	evt := testEvent(EventCheckFailure, "high")
	evt.ID = "fixed-id"
	evt.DedupKey = "dedup-1"

	// First — creates task.
	taskID1, err := bridge.ProcessEvent(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if taskID1 == "" {
		t.Fatal("expected task ID from first event")
	}

	// Second (duplicate) — suppressed.
	taskID2, err := bridge.ProcessEvent(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if taskID2 != taskID1 {
		t.Errorf("duplicate should return same task ID, got %q and %q", taskID1, taskID2)
	}
	if creator.CallCount() != 1 {
		t.Errorf("dedup should prevent second create, got %d calls", creator.CallCount())
	}

	metrics := bridge.Metrics()
	if metrics["events_deduped"] != 1 {
		t.Errorf("expected 1 deduped, got %d", metrics["events_deduped"])
	}
}

func TestProcessEventSeverityFilter(t *testing.T) {
	cfg := testBridgeConfig()
	cfg.SeverityFilter = "medium" // only medium and above
	creator := newMockCreator(0)
	bridge := NewEventBridge(cfg, DefaultMappings(), creator)

	// Low severity — filtered.
	taskID, err := bridge.ProcessEvent(context.Background(), testEvent(EventCheckFailure, "low"))
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "" {
		t.Error("low severity should be filtered")
	}

	// High severity — passes.
	taskID, err = bridge.ProcessEvent(context.Background(), testEvent(EventCheckFailure, "high"))
	if err != nil {
		t.Fatal(err)
	}
	if taskID == "" {
		t.Error("high severity should pass filter")
	}

	metrics := bridge.Metrics()
	if metrics["events_filtered"] != 1 {
		t.Errorf("expected 1 filtered, got %d", metrics["events_filtered"])
	}
}

func TestProcessEventRateLimit(t *testing.T) {
	cfg := testBridgeConfig()
	cfg.RateLimitPerType = 2
	cfg.RateLimitAggregate = 100
	creator := newMockCreator(0)
	bridge := NewEventBridge(cfg, DefaultMappings(), creator)

	for i := 0; i < 2; i++ {
		evt := testEvent(EventCheckFailure, "high")
		evt.ID = fmt.Sprintf("evt-%d", i)
		evt.DedupKey = fmt.Sprintf("dedup-%d", i)
		if _, err := bridge.ProcessEvent(context.Background(), evt); err != nil {
			t.Fatal(err)
		}
	}

	// Third should be rate-limited.
	evt := testEvent(EventCheckFailure, "high")
	evt.ID = "evt-overflow"
	evt.DedupKey = "dedup-overflow"
	taskID, err := bridge.ProcessEvent(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "" {
		t.Error("should be rate-limited")
	}

	metrics := bridge.Metrics()
	if metrics["events_rate_limit"] != 1 {
		t.Errorf("expected 1 rate-limited, got %d", metrics["events_rate_limit"])
	}
}

func TestProcessEventCircuitBreaker(t *testing.T) {
	cfg := testBridgeConfig()
	cfg.CircuitThreshold = 2
	creator := newMockCreator(5) // first 5 calls fail
	bridge := NewEventBridge(cfg, DefaultMappings(), creator)

	// Cause failures to trip the breaker.
	for i := 0; i < 2; i++ {
		evt := testEvent(EventCheckFailure, "high")
		evt.ID = fmt.Sprintf("fail-%d", i)
		evt.DedupKey = fmt.Sprintf("fd-%d", i)
		_, _ = bridge.ProcessEvent(context.Background(), evt)
	}

	// Circuit should be open — next event rejected.
	evt := testEvent(EventAlertFired, "critical")
	evt.ID = "after-trip"
	evt.DedupKey = "fd-after"
	taskID, err := bridge.ProcessEvent(context.Background(), evt)
	if err == nil && taskID != "" {
		t.Error("circuit should be open")
	}

	metrics := bridge.Metrics()
	if metrics["events_circuit"] == 0 {
		t.Error("expected circuit events to be counted")
	}
}

func TestProcessEventTaskCreationFailureRetries(t *testing.T) {
	creator := newMockCreator(2) // first 2 calls fail
	cfg := testBridgeConfig()
	cfg.MaxRetries = 3
	bridge := NewEventBridge(cfg, DefaultMappings(), creator)

	evt := testEvent(EventScriptError, "medium")
	evt.ID = "retry-test"
	evt.DedupKey = "rd-1"

	taskID, err := bridge.ProcessEvent(context.Background(), evt)
	if err != nil {
		t.Fatalf("should succeed on retry: %v", err)
	}
	if taskID == "" {
		t.Error("expected task ID after retry")
	}
	if creator.CallCount() != 3 { // 2 fails + 1 success
		t.Errorf("expected 3 create calls (2 fail + 1 ok), got %d", creator.CallCount())
	}
}

func TestProcessEventTaskCreationExhaustedRetries(t *testing.T) {
	creator := newMockCreator(10) // always fail
	bridge := NewEventBridge(testBridgeConfig(), DefaultMappings(), creator)

	evt := testEvent(EventSecurityEvent, "critical")
	evt.ID = "exhaust-test"
	evt.DedupKey = "ed-1"

	_, err := bridge.ProcessEvent(context.Background(), evt)
	if err == nil {
		t.Error("expected error after exhausted retries")
	}

	metrics := bridge.Metrics()
	if metrics["conversion_fails"] != 1 {
		t.Errorf("expected 1 conversion fail, got %d", metrics["conversion_fails"])
	}
}

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
