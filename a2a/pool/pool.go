// Package pool implements the ProcessPool — warm agent framework instances
// ready to accept work. Each framework (LangGraph, CrewAI, etc.) gets its own
// pool with configurable min/max/idle, health checks, crash detection, and
// graceful shutdown.
package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// Errors
// ============================================================

var (
	ErrPoolClosed      = errors.New("pool: closed")
	ErrPoolExhausted   = errors.New("pool: exhausted — no instances available")
	ErrCheckoutTimeout = errors.New("pool: checkout timeout")
)

// ============================================================
// Configuration
// ============================================================

// Config defines pool parameters for a single framework.
type Config struct {
	Framework           string
	MinWarm             int
	Max                 int
	IdleTarget          int
	MaxTasksPerInstance int
	MaxInstanceAge      time.Duration
	CheckoutTimeout     time.Duration
	HealthCheckInterval time.Duration
	DestroyTimeout      time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig(framework string) Config {
	return Config{
		Framework:           framework,
		MinWarm:             2,
		Max:                 10,
		IdleTarget:          3,
		MaxTasksPerInstance: 100,
		MaxInstanceAge:      30 * time.Minute,
		CheckoutTimeout:     10 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		DestroyTimeout:      5 * time.Second,
	}
}

// ============================================================
// Pool
// ============================================================

// Pool manages warm instances for a single framework.
type Pool struct {
	cfg     Config
	factory Factory

	// all tracks every live instance (idle + checked-out).
	all []*Instance
	// idle tracks only instances available for checkout.
	idle []*Instance

	mu       sync.Mutex
	waiters  []chan *Instance
	waiterMu sync.Mutex
	closed   atomic.Bool
	shutdown chan struct{}
	metrics  Metrics
}

// Factory creates new instances. Must return a ready-to-use instance.
type Factory func(ctx context.Context, framework string) (*Instance, error)

// New creates a pool. Call Start to pre-warm.
func New(cfg Config, factory Factory) *Pool {
	return &Pool{
		cfg:      cfg,
		factory:  factory,
		shutdown: make(chan struct{}),
	}
}

// Start pre-warms the pool to MinWarm.
func (p *Pool) Start(ctx context.Context) error {
	for i := 0; i < p.cfg.MinWarm; i++ {
		inst, err := p.createInstance(ctx)
		if err != nil {
			return fmt.Errorf("pool %s: pre-warm %d: %w", p.cfg.Framework, i, err)
		}
		p.mu.Lock()
		p.idle = append(p.idle, inst)
		p.mu.Unlock()
	}
	go p.maintenanceLoop()
	return nil
}

// ============================================================
// Checkout / Return
// ============================================================

// Checkout acquires a warm instance. Caller MUST call Return when done.
func (p *Pool) Checkout(ctx context.Context) (*Instance, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	deadline := time.After(p.cfg.CheckoutTimeout)

	for {
		// Try idle list.
		if inst := p.popIdle(); inst != nil {
			p.metrics.checkoutsTotal.Add(1)
			return inst, nil
		}

		// Below max — create on demand.
		p.mu.Lock()
		total := len(p.all)
		p.mu.Unlock()
		if total < p.cfg.Max {
			inst, err := p.createInstance(ctx)
			if err != nil {
				return nil, fmt.Errorf("pool %s: create: %w", p.cfg.Framework, err)
			}
			p.metrics.checkoutsTotal.Add(1)
			return inst, nil
		}

		// At max — wait for a return.
		waiter := make(chan *Instance, 1)
		p.waiterMu.Lock()
		p.waiters = append(p.waiters, waiter)
		p.waiterMu.Unlock()

		select {
		case inst := <-waiter:
			if inst == nil {
				return nil, ErrPoolClosed
			}
			p.metrics.checkoutsTotal.Add(1)
			return inst, nil
		case <-deadline:
			p.removeWaiter(waiter)
			p.metrics.checkoutTimeouts.Add(1)
			return nil, ErrCheckoutTimeout
		case <-ctx.Done():
			p.removeWaiter(waiter)
			return nil, ctx.Err()
		}
	}
}

// Return returns an instance to the pool.
func (p *Pool) Return(inst *Instance) {
	if inst == nil {
		return
	}
	if p.closed.Load() {
		p.destroyInstance(inst)
		return
	}

	inst.markIdle()

	if p.shouldRecycle(inst) {
		p.mu.Lock()
		p.removeByID(inst.ID())
		p.mu.Unlock()
		p.destroyInstance(inst)
		p.replaceInstance()
		return
	}

	// Wake a waiting checkout.
	p.waiterMu.Lock()
	if len(p.waiters) > 0 {
		w := p.waiters[0]
		p.waiters = p.waiters[1:]
		p.waiterMu.Unlock()
		w <- inst
		return
	}
	p.waiterMu.Unlock()

	// No waiters — add to idle.
	p.mu.Lock()
	p.idle = append(p.idle, inst)
	p.mu.Unlock()
}

// ============================================================
// Internals
// ============================================================

func (p *Pool) createInstance(ctx context.Context) (*Instance, error) {
	inst, err := p.factory(ctx, p.cfg.Framework)
	if err != nil {
		p.metrics.creationFailures.Add(1)
		return nil, err
	}
	p.metrics.creationsTotal.Add(1)
	p.mu.Lock()
	p.all = append(p.all, inst)
	p.mu.Unlock()
	return inst, nil
}

func (p *Pool) destroyInstance(inst *Instance) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.DestroyTimeout)
	defer cancel()
	inst.destroy(ctx)
	p.metrics.destroysTotal.Add(1)
}

func (p *Pool) replaceInstance() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inst, err := p.createInstance(ctx)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.idle = append(p.idle, inst)
	p.mu.Unlock()
}

func (p *Pool) shouldRecycle(inst *Instance) bool {
	if p.cfg.MaxTasksPerInstance > 0 && int(inst.TasksCompleted()) >= p.cfg.MaxTasksPerInstance {
		return true
	}
	if p.cfg.MaxInstanceAge > 0 && time.Since(inst.CreatedAt()) > p.cfg.MaxInstanceAge {
		return true
	}
	return false
}

func (p *Pool) popIdle() *Instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Pop from front; re-evaluate len each iteration because destroy unlocks.
	for len(p.idle) > 0 {
		inst := p.idle[0]
		p.idle = p.idle[1:]
		if inst.isHealthy() {
			inst.markCheckedOut()
			return inst
		}
		// Unhealthy — remove from both lists and destroy.
		p.removeByID(inst.ID())
		p.mu.Unlock()
		p.destroyInstance(inst)
		p.mu.Lock()
	}
	return nil
}

func (p *Pool) removeByID(id string) {
	for i, inst := range p.all {
		if inst.ID() == id {
			p.all = append(p.all[:i], p.all[i+1:]...)
			return
		}
	}
}

func (p *Pool) removeWaiter(w chan *Instance) {
	p.waiterMu.Lock()
	defer p.waiterMu.Unlock()
	for i, waiter := range p.waiters {
		if waiter == w {
			p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
			return
		}
	}
}

// ============================================================
// Metrics
// ============================================================

type Metrics struct {
	checkoutsTotal     atomic.Int64
	checkoutTimeouts   atomic.Int64
	creationsTotal     atomic.Int64
	creationFailures   atomic.Int64
	destroysTotal      atomic.Int64
	recyclesTotal      atomic.Int64
	circuitBreakerTrips atomic.Int64
}

type Snapshot struct {
	WarmCount        int
	CheckedOutCount  int
	TotalCount       int
	CheckoutsTotal   int64
	CheckoutTimeouts int64
	CreationsTotal   int64
	CreationFailures int64
	DestroysTotal    int64
}

func (p *Pool) MetricsSnapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{
		WarmCount:        len(p.idle),
		CheckedOutCount:  len(p.all) - len(p.idle),
		TotalCount:       len(p.all),
		CheckoutsTotal:   p.metrics.checkoutsTotal.Load(),
		CheckoutTimeouts: p.metrics.checkoutTimeouts.Load(),
		CreationsTotal:   p.metrics.creationsTotal.Load(),
		CreationFailures: p.metrics.creationFailures.Load(),
		DestroysTotal:    p.metrics.destroysTotal.Load(),
	}
}
