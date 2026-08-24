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
// Metrics
// ============================================================

type Metrics struct {
	checkoutsTotal      atomic.Int64
	checkoutTimeouts    atomic.Int64
	creationsTotal      atomic.Int64
	creationFailures    atomic.Int64
	destroysTotal       atomic.Int64
	recyclesTotal       atomic.Int64
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
