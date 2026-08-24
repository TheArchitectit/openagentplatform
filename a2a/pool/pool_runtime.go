package pool

import (
	"context"
	"fmt"
	"time"
)

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
