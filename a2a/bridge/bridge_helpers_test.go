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
