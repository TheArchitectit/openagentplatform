package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// Instance
// ============================================================

// InstanceState tracks whether an instance is idle, checked out, or destroyed.
type InstanceState int32

const (
	StateIdle        InstanceState = 0
	StateCheckedOut  InstanceState = 1
	StateDestroyed   InstanceState = 2
)

// Instance represents a single warm framework process.
type Instance struct {
	id              string
	framework       string
	state           atomic.Int32
	tasksCompleted  atomic.Int64
	createdAt       time.Time
	destroyedAt     atomic.Pointer[time.Time]
	mu              sync.Mutex
	destroyFunc     func(ctx context.Context) error
	healthCheckFunc func(ctx context.Context) error
	cleanupFunc     func() error
	metadata        map[string]string
}

// InstanceConfig holds the configuration for creating an instance.
type InstanceConfig struct {
	ID              string
	Framework       string
	DestroyFunc     func(ctx context.Context) error
	HealthCheckFunc func(ctx context.Context) error
	CleanupFunc     func() error
	Metadata        map[string]string
}

// NewInstance creates a new instance with the given configuration.
func NewInstance(cfg InstanceConfig) *Instance {
	return &Instance{
		id:              cfg.ID,
		framework:       cfg.Framework,
		createdAt:       time.Now(),
		destroyFunc:     cfg.DestroyFunc,
		healthCheckFunc: cfg.HealthCheckFunc,
		cleanupFunc:     cfg.CleanupFunc,
		metadata:        cfg.Metadata,
	}
}

// ID returns the instance identifier.
func (inst *Instance) ID() string { return inst.id }

// Framework returns the framework name.
func (inst *Instance) Framework() string { return inst.framework }

// CreatedAt returns the creation timestamp.
func (inst *Instance) CreatedAt() time.Time { return inst.createdAt }

// TasksCompleted returns the number of tasks this instance has served.
func (inst *Instance) TasksCompleted() int64 { return inst.tasksCompleted.Load() }

// State returns the current instance state.
func (inst *Instance) State() InstanceState {
	return InstanceState(inst.state.Load())
}

// Metadata returns the instance metadata map.
func (inst *Instance) Metadata() map[string]string { return inst.metadata }

// ============================================================
// State transitions
// ============================================================

// markCheckedOut transitions the instance to checked-out state.
func (inst *Instance) markCheckedOut() {
	inst.state.Store(int32(StateCheckedOut))
}

// markIdle transitions the instance back to idle state, increments the
// task counter, and resets metadata for the next checkout.
func (inst *Instance) markIdle() {
	inst.tasksCompleted.Add(1)
	// Reset metadata to prevent cross-task data leaks.
	inst.mu.Lock()
	if inst.metadata == nil {
		inst.metadata = make(map[string]string)
	} else {
		for k := range inst.metadata {
			delete(inst.metadata, k)
		}
	}
	inst.mu.Unlock()
	inst.state.Store(int32(StateIdle))
}

// markDestroyed transitions the instance to destroyed state.
func (inst *Instance) markDestroyed() {
	now := time.Now()
	inst.destroyedAt.Store(&now)
	inst.state.Store(int32(StateDestroyed))
}

// isHealthy runs the health check function (if configured) and returns
// whether the instance is in a usable state.
func (inst *Instance) isHealthy() bool {
	if inst.State() != StateIdle {
		return false
	}
	if inst.healthCheckFunc == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return inst.healthCheckFunc(ctx) == nil
}

// destroy tears down the instance, running cleanup and then the
// destroy function. Safe to call multiple times.
func (inst *Instance) destroy(ctx context.Context) {
	if inst.State() == StateDestroyed {
		return
	}
	inst.markDestroyed()
	if inst.cleanupFunc != nil {
		_ = inst.cleanupFunc()
	}
	if inst.destroyFunc != nil {
		_ = inst.destroyFunc(ctx)
	}
}

// Reset clears instance state between checkouts. Returns false if the
// instance cannot be verifiably reset (caller should destroy it).
func (inst *Instance) Reset() bool {
	if inst.State() != StateCheckedOut && inst.State() != StateIdle {
		return false
	}
	inst.mu.Lock()
	if inst.metadata == nil {
		inst.metadata = make(map[string]string)
	} else {
		for k := range inst.metadata {
			delete(inst.metadata, k)
		}
	}
	inst.mu.Unlock()
	return true
}
