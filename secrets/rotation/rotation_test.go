package rotation

import (
	"context"
	"fmt"
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
