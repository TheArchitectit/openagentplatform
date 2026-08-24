package bridge

import (
	"context"
	"fmt"
	"testing"
)

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
