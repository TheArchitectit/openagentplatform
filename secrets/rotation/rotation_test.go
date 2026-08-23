package rotation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/secrets"
)

// --- Helpers ---

func newTestScheduler(t *testing.T) (*RotationScheduler, *secrets.MemoryBackend) {
	t.Helper()
	backend := secrets.NewMemoryBackend()
	scheduler := NewScheduler(backend, nil)
	return scheduler, backend
}

func mustSet(t *testing.T, backend secrets.SecretBackend, path string, data map[string]any) {
	t.Helper()
	_, err := backend.Set(context.Background(), path, data, secrets.SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
}

// --- RotationScheduler Tests ---

func TestScheduler_RegisterPolicy(t *testing.T) {
	s, _ := newTestScheduler(t)
	s.RegisterPolicy(RotationPolicy{
		Path:     "test/secret",
		Backend:  "memory",
		Interval: 1 * time.Hour,
		Enabled:  true,
	})

	policies := s.Policies()
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	if policies[0].Path != "test/secret" {
		t.Fatalf("expected test/secret, got %s", policies[0].Path)
	}
}

func TestScheduler_UnregisterPolicy(t *testing.T) {
	s, _ := newTestScheduler(t)
	s.RegisterPolicy(RotationPolicy{Path: "x", Enabled: true})
	s.UnregisterPolicy("x")
	if len(s.Policies()) != 0 {
		t.Fatal("expected 0 policies after unregister")
	}
}

func TestScheduler_NeedsRotation(t *testing.T) {
	s, _ := newTestScheduler(t)

	// No policy = false.
	if s.NeedsRotation("missing") {
		t.Fatal("expected false for missing policy")
	}

	// Disabled policy = false.
	s.RegisterPolicy(RotationPolicy{Path: "a", Enabled: false})
	if s.NeedsRotation("a") {
		t.Fatal("expected false for disabled policy")
	}

	// Enabled with short interval = true.
	s.RegisterPolicy(RotationPolicy{
		Path:     "b",
		Enabled:  true,
		Interval: 1 * time.Millisecond,
	})
	time.Sleep(2 * time.Millisecond)
	if !s.NeedsRotation("b") {
		t.Fatal("expected true for expired interval")
	}
}

func TestScheduler_NeedsRotation_MaxAge(t *testing.T) {
	s, _ := newTestScheduler(t)
	s.RegisterPolicy(RotationPolicy{
		Path:    "c",
		Enabled: true,
		MaxAge:  1 * time.Millisecond,
	})
	time.Sleep(2 * time.Millisecond)
	if !s.NeedsRotation("c") {
		t.Fatal("expected true for expired MaxAge")
	}
}

func TestScheduler_RotateNow(t *testing.T) {
	s, backend := newTestScheduler(t)
	mustSet(t, backend, "r/secret", map[string]any{"k": "v1"})

	s.RegisterPolicy(RotationPolicy{
		Path:    "r/secret",
		Backend: "memory",
		Enabled: true,
	})

	event := s.RotateNow(context.Background(), "r/secret")
	if !event.Success {
		t.Fatalf("expected success, got error: %v", event.Error)
	}
	if event.NewVersion != 2 {
		t.Fatalf("expected version 2, got %d", event.NewVersion)
	}

	record := s.Record("r/secret")
	if record.RotationCount != 1 {
		t.Fatalf("expected rotation count 1, got %d", record.RotationCount)
	}
}

func TestScheduler_RotateNow_NoPolicy(t *testing.T) {
	s, _ := newTestScheduler(t)
	event := s.RotateNow(context.Background(), "missing")
	if event.Error == nil {
		t.Fatal("expected error for missing policy")
	}
}

func TestScheduler_RotateNow_BackendError(t *testing.T) {
	s, _ := newTestScheduler(t)
	// Don't set any secret — Rotate on nonexistent path should fail.
	s.RegisterPolicy(RotationPolicy{
		Path:    "no/such/path",
		Backend: "memory",
		Enabled: true,
	})

	event := s.RotateNow(context.Background(), "no/such/path")
	if event.Error == nil {
		t.Fatal("expected backend error")
	}
	if event.Success {
		t.Fatal("expected success=false")
	}
}

// --- Lifecycle Hook Tests ---

func TestScheduler_PreRotationHook(t *testing.T) {
	s, backend := newTestScheduler(t)
	mustSet(t, backend, "hook/secret", map[string]any{"k": "v"})

	s.RegisterPolicy(RotationPolicy{
		Path:    "hook/secret",
		Backend: "memory",
		Enabled: true,
	})

	var preCalled atomic.Bool
	s.AddHandler("hook/secret", &testHandler{
		preFn: func(ctx context.Context, path string) error {
			preCalled.Store(true)
			return nil
		},
	})

	s.RotateNow(context.Background(), "hook/secret")
	if !preCalled.Load() {
		t.Fatal("expected pre-rotation hook to be called")
	}
}

func TestScheduler_PreRotationHook_Abort(t *testing.T) {
	s, backend := newTestScheduler(t)
	mustSet(t, backend, "abort/secret", map[string]any{"k": "v"})

	s.RegisterPolicy(RotationPolicy{
		Path:    "abort/secret",
		Backend: "memory",
		Enabled: true,
	})

	s.AddHandler("abort/secret", &testHandler{
		preFn: func(ctx context.Context, path string) error {
			return fmt.Errorf("rotation not allowed")
		},
	})

	event := s.RotateNow(context.Background(), "abort/secret")
	if event.Error == nil {
		t.Fatal("expected error from aborted pre-rotation")
	}
	if event.Success {
		t.Fatal("expected success=false for aborted rotation")
	}
}

func TestScheduler_PostRotationHook(t *testing.T) {
	s, backend := newTestScheduler(t)
	mustSet(t, backend, "post/secret", map[string]any{"k": "v"})

	s.RegisterPolicy(RotationPolicy{
		Path:    "post/secret",
		Backend: "memory",
		Enabled: true,
	})

	var postEvent *RotationEvent
	s.AddHandler("post/secret", &testHandler{
		postFn: func(ctx context.Context, path string, event RotationEvent) {
			cp := event
			postEvent = &cp
		},
	})

	s.RotateNow(context.Background(), "post/secret")
	if postEvent == nil {
		t.Fatal("expected post-rotation hook to be called")
	}
	if !postEvent.Success {
		t.Fatal("expected success in post event")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	s, backend := newTestScheduler(t)
	mustSet(t, backend, "auto/secret", map[string]any{"k": "v1"})

	s.RegisterPolicy(RotationPolicy{
		Path:     "auto/secret",
		Backend:  "memory",
		Enabled:  true,
		Interval: 1 * time.Millisecond,
	})

	s.Start(context.Background(), 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	s.Stop()

	record := s.Record("auto/secret")
	if record.RotationCount == 0 {
		t.Fatal("expected at least 1 rotation from background loop")
	}
}

// --- testHandler ---

type testHandler struct {
	preFn  func(ctx context.Context, path string) error
	postFn func(ctx context.Context, path string, event RotationEvent)
}

func (h *testHandler) PreRotation(ctx context.Context, path string) error {
	if h.preFn != nil {
		return h.preFn(ctx, path)
	}
	return nil
}

func (h *testHandler) PostRotation(ctx context.Context, path string, event RotationEvent) {
	if h.postFn != nil {
		h.postFn(ctx, path, event)
	}
}

// --- LifecycleManager Tests ---

func TestLifecycleManager_BasicChain(t *testing.T) {
	lm := NewLifecycleManager(nil)
	var order []string
	var mu sync.Mutex

	record := func(phase string) {
		mu.Lock()
		order = append(order, phase)
		mu.Unlock()
	}

	lm.Register(PhasePreValidation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		record("pre_validation")
		return nil
	})
	lm.Register(PhasePreRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		record("pre_rotation")
		return nil
	})
	lm.Register(PhasePostRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		record("post_rotation")
		return nil
	})
	lm.Register(PhasePostValidation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		record("post_validation")
		return nil
	})

	err := lm.RunChain(context.Background(), "test/path", func() error {
		record("rotation")
		return nil
	})
	if err != nil {
		t.Fatalf("RunChain: %v", err)
	}

	expected := []string{"pre_validation", "pre_rotation", "rotation", "post_rotation", "post_validation"}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != len(expected) {
		t.Fatalf("expected %d phases, got %d: %v", len(expected), len(order), order)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("phase %d: expected %s, got %s", i, expected[i], order[i])
		}
	}
}

func TestLifecycleManager_AbortOnPreError(t *testing.T) {
	lm := NewLifecycleManager(nil)
	var postCalled bool

	lm.Register(PhasePreRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		return fmt.Errorf("blocked")
	})
	lm.Register(PhasePostRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		postCalled = true
		return nil
	})

	err := lm.RunChain(context.Background(), "p", func() error {
		t.Fatal("rotation should not run when pre-hook fails")
		return nil
	})
	if err == nil {
		t.Fatal("expected error from RunChain")
	}
	if postCalled {
		t.Fatal("post hooks should not run when pre-hook fails")
	}
}

func TestLifecycleManager_RotationErrorTriggersPostHooks(t *testing.T) {
	lm := NewLifecycleManager(nil)
	var postErr error

	lm.Register(PhasePostRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error {
		if e.Error != nil {
			postErr = e.Error
		}
		return nil
	})

	err := lm.RunChain(context.Background(), "p", func() error {
		return fmt.Errorf("rotation failed")
	})
	if err == nil {
		t.Fatal("expected error from RunChain")
	}
	if postErr == nil {
		t.Fatal("expected post-hook to receive rotation error")
	}
}

func TestLifecycleManager_PathSpecificCallback(t *testing.T) {
	lm := NewLifecycleManager(nil)
	var called bool

	lm.Register(PhasePreRotation, "specific/path", 0, func(ctx context.Context, e LifecycleEvent) error {
		called = true
		return nil
	})

	// Different path should not trigger.
	lm.Run(context.Background(), PhasePreRotation, "other/path", nil)
	if called {
		t.Fatal("path-specific callback should not fire for different path")
	}

	// Correct path should trigger.
	lm.Run(context.Background(), PhasePreRotation, "specific/path", nil)
	if !called {
		t.Fatal("path-specific callback should fire for matching path")
	}
}

func TestLifecycleManager_Priority(t *testing.T) {
	lm := NewLifecycleManager(nil)
	var order []string
	var mu sync.Mutex

	lm.Register(PhasePreRotation, "", 10, func(ctx context.Context, e LifecycleEvent) error {
		mu.Lock()
		order = append(order, "low-priority")
		mu.Unlock()
		return nil
	})
	lm.Register(PhasePreRotation, "", 1, func(ctx context.Context, e LifecycleEvent) error {
		mu.Lock()
		order = append(order, "high-priority")
		mu.Unlock()
		return nil
	})

	lm.Run(context.Background(), PhasePreRotation, "x", nil)

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "high-priority" {
		t.Fatalf("expected [high-priority low-priority], got %v", order)
	}
}

func TestLifecycleManager_Unregister(t *testing.T) {
	lm := NewLifecycleManager(nil)
	lm.Register(PhasePreRotation, "path/a", 0, func(ctx context.Context, e LifecycleEvent) error {
		return nil
	})
	lm.Register(PhasePreRotation, "path/b", 0, func(ctx context.Context, e LifecycleEvent) error {
		return nil
	})

	lm.Unregister(PhasePreRotation, "path/a")
	if lm.CallbackCount(PhasePreRotation) != 1 {
		t.Fatalf("expected 1 callback after unregister, got %d", lm.CallbackCount(PhasePreRotation))
	}
}

func TestLifecycleManager_Clear(t *testing.T) {
	lm := NewLifecycleManager(nil)
	lm.Register(PhasePreRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error { return nil })
	lm.Register(PhasePostRotation, "", 0, func(ctx context.Context, e LifecycleEvent) error { return nil })

	lm.Clear()
	if lm.CallbackCount(PhasePreRotation) != 0 || lm.CallbackCount(PhasePostRotation) != 0 {
		t.Fatal("expected 0 callbacks after Clear")
	}
}
