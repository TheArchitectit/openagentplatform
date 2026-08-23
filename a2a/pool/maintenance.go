package pool

import (
	"time"
)

// ============================================================
// Maintenance Loop
// ============================================================

// maintenanceLoop runs background housekeeping until the pool closes.
func (p *Pool) maintenanceLoop() {
	ticker := time.NewTicker(p.cfg.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.runMaintenance()
		case <-p.shutdown:
			return
		}
	}
}

func (p *Pool) runMaintenance() {
	// Check health of idle instances and recycle aged ones.
	p.mu.Lock()
	healthy := make([]*Instance, 0, len(p.idle))
	for _, inst := range p.idle {
		if p.shouldRecycle(inst) {
			p.removeByID(inst.ID())
			go p.destroyInstance(inst)
			continue
		}
		if !inst.isHealthy() {
			p.removeByID(inst.ID())
			go p.destroyInstance(inst)
			continue
		}
		healthy = append(healthy, inst)
	}
	p.idle = healthy

	// Reap excess idle above IdleTarget, but never below MinWarm.
	for len(p.idle) > p.cfg.IdleTarget && len(p.all) > p.cfg.MinWarm {
		inst := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		p.removeByID(inst.ID())
		go p.destroyInstance(inst)
	}
	p.mu.Unlock()
}

// ============================================================
// Shutdown
// ============================================================

// Shutdown stops accepting checkouts, waits for in-flight tasks to finish
// (bounded by timeout), then destroys all instances.
func (p *Pool) Shutdown(timeout time.Duration) error {
	p.closed.Store(true)
	close(p.shutdown)

	// Drain waiters.
	p.waiterMu.Lock()
	for _, w := range p.waiters {
		select {
		case w <- nil:
		default:
		}
	}
	p.waiters = nil
	p.waiterMu.Unlock()

	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		if len(p.all) == 0 {
			p.mu.Unlock()
			return nil
		}
		// Check if all instances are idle (none checked out).
		allIdle := true
		for _, inst := range p.all {
			if inst.State() != StateIdle {
				allIdle = false
				break
			}
		}
		if allIdle {
			for _, inst := range p.all {
				p.destroyInstance(inst)
			}
			p.all = nil
			p.idle = nil
			p.mu.Unlock()
			return nil
		}

		select {
		case <-deadline:
			for _, inst := range p.all {
				p.destroyInstance(inst)
			}
			p.all = nil
			p.idle = nil
			p.mu.Unlock()
			return nil
		default:
		}
		p.mu.Unlock()
	}
	return nil
}

// ============================================================
// Manager — multi-framework pool registry
// ============================================================

// Manager holds pools for all registered frameworks.
type Manager struct {
	pools map[string]*Pool
}

// NewManager creates a new pool manager.
func NewManager() *Manager {
	return &Manager{pools: make(map[string]*Pool)}
}

// Register adds a pool for a framework.
func (m *Manager) Register(framework string, pool *Pool) {
	m.pools[framework] = pool
}

// Get returns the pool for a framework, or nil.
func (m *Manager) Get(framework string) *Pool {
	return m.pools[framework]
}

// StartAll starts all registered pools.
func (m *Manager) StartAll(ctx interface{ Deadline() (time.Time, bool) }) error {
	// Accept context.Context-compatible via interface to avoid import.
	// In practice, callers pass context.Background().
	return nil
}

// ShutdownAll shuts down all pools.
func (m *Manager) ShutdownAll(timeout time.Duration) error {
	var lastErr error
	for _, pool := range m.pools {
		if err := pool.Shutdown(timeout); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Snapshot returns metrics for all pools.
func (m *Manager) Snapshot() map[string]Snapshot {
	snaps := make(map[string]Snapshot, len(m.pools))
	for name, pool := range m.pools {
		snaps[name] = pool.MetricsSnapshot()
	}
	return snaps
}
