package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================
// Test helpers
// ============================================================

func testFactory(framework string) Factory {
	var counter atomic.Int64
	return func(ctx context.Context, fw string) (*Instance, error) {
		id := counter.Add(1)
		return NewInstance(InstanceConfig{
			ID:        fmt.Sprintf("inst-%d", id),
			Framework: fw,
			DestroyFunc: func(ctx context.Context) error {
				return nil
			},
			HealthCheckFunc: func(ctx context.Context) error {
				return nil
			},
			Metadata: make(map[string]string),
		}), nil
	}
}

func testConfig() Config {
	cfg := DefaultConfig("test")
	cfg.MinWarm = 2
	cfg.Max = 5
	cfg.IdleTarget = 2
	cfg.CheckoutTimeout = 1 * time.Second
	cfg.HealthCheckInterval = 100 * time.Millisecond
	cfg.DestroyTimeout = 1 * time.Second
	return cfg
}

// ============================================================
// Tests
// ============================================================

func TestPoolPreWarm(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	snap := p.MetricsSnapshot()
	if snap.WarmCount != 2 {
		t.Errorf("expected 2 warm instances, got %d", snap.WarmCount)
	}
}

func TestPoolCheckoutReturn(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	inst, err := p.Checkout(ctx)
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if inst == nil {
		t.Fatal("expected non-nil instance")
	}
	if inst.State() != StateCheckedOut {
		t.Errorf("expected checked-out state, got %d", inst.State())
	}

	p.Return(inst)

	snap := p.MetricsSnapshot()
	if snap.CheckoutsTotal != 1 {
		t.Errorf("expected 1 checkout, got %d", snap.CheckoutsTotal)
	}
}

func TestPoolMultipleCheckoutReturn(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var instances []*Instance
	for i := 0; i < 5; i++ {
		inst, err := p.Checkout(ctx)
		if err != nil {
			t.Fatalf("Checkout %d: %v", i, err)
		}
		instances = append(instances, inst)
	}

	snap := p.MetricsSnapshot()
	if snap.WarmCount != 0 {
		t.Errorf("expected 0 warm after 5 checkouts, got %d", snap.WarmCount)
	}

	for _, inst := range instances {
		p.Return(inst)
	}

	snap = p.MetricsSnapshot()
	if snap.WarmCount != 5 {
		t.Errorf("expected 5 warm after returns, got %d", snap.WarmCount)
	}
}

func TestPoolExhaustionTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.MinWarm = 1
	cfg.Max = 1
	cfg.CheckoutTimeout = 200 * time.Millisecond
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First checkout succeeds.
	inst, err := p.Checkout(ctx)
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	// Second checkout should timeout.
	_, err = p.Checkout(ctx)
	if !errors.Is(err, ErrCheckoutTimeout) {
		t.Errorf("expected ErrCheckoutTimeout, got %v", err)
	}

	p.Return(inst)
}

func TestPoolClosedRejectsCheckout(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	p.closed.Store(true)

	_, err := p.Checkout(ctx)
	if !errors.Is(err, ErrPoolClosed) {
		t.Errorf("expected ErrPoolClosed, got %v", err)
	}
}

func TestPoolRecycleOnMaxTasks(t *testing.T) {
	cfg := testConfig()
	cfg.MaxTasksPerInstance = 1
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	inst, err := p.Checkout(ctx)
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	p.Return(inst) // tasks completed: 1 — should trigger recycle (max=1)

	snap := p.MetricsSnapshot()
	if snap.DestroysTotal == 0 {
		t.Error("expected at least 1 destroy for recycled instance")
	}
}

func TestPoolShutdown(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := p.Shutdown(2 * time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	snap := p.MetricsSnapshot()
	if snap.WarmCount != 0 {
		t.Errorf("expected 0 warm after shutdown, got %d", snap.WarmCount)
	}
}

func TestPoolNoLeakBetweenCheckouts(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First task: set metadata on instance.
	inst1, _ := p.Checkout(ctx)
	inst1.mu.Lock()
	inst1.metadata["task_id"] = "task-1"
	inst1.mu.Unlock()
	p.Return(inst1)

	// Return marks instance idle, which resets metadata via markIdle.
	// Second checkout should see clean metadata.
	inst2, _ := p.Checkout(ctx)
	inst2.mu.Lock()
	taskID := inst2.metadata["task_id"]
	inst2.mu.Unlock()
	if taskID != "" {
		t.Errorf("metadata leaked between checkouts: task_id=%s", taskID)
	}
	p.Return(inst2)
}

func TestPoolConcurrentCheckout(t *testing.T) {
	cfg := testConfig()
	cfg.Max = 10
	cfg.MinWarm = 3
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst, err := p.Checkout(ctx)
			if err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			p.Return(inst)
		}()
	}
	wg.Wait()

	snap := p.MetricsSnapshot()
	if snap.CheckoutsTotal != 20 {
		t.Errorf("expected 20 checkouts, got %d", snap.CheckoutsTotal)
	}
}

func TestPoolReturnNilIsNoop(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Should not panic.
	p.Return(nil)
}

func TestPoolMetricsAccumulate(t *testing.T) {
	cfg := testConfig()
	p := New(cfg, testFactory("test"))
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 10; i++ {
		inst, err := p.Checkout(ctx)
		if err != nil {
			t.Fatalf("Checkout %d: %v", i, err)
		}
		p.Return(inst)
	}

	snap := p.MetricsSnapshot()
	if snap.CheckoutsTotal != 10 {
		t.Errorf("expected 10 checkouts, got %d", snap.CheckoutsTotal)
	}
	if snap.CreationsTotal < 2 {
		t.Errorf("expected at least 2 creations (pre-warm), got %d", snap.CreationsTotal)
	}
}
