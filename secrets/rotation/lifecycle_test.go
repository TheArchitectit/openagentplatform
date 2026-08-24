package rotation

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

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
