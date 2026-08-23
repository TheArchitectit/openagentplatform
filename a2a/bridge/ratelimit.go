package bridge

import (
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// Rate Limiter — token-bucket per event type + aggregate.
// ============================================================

// RateLimiter enforces per-event-type and aggregate task creation limits
// using a sliding-window counter approach.
type RateLimiter struct {
	mu sync.Mutex

	// Per-event-type limits (tasks per minute).
	perTypeLimit int64
	// Aggregate limit (tasks per minute across all types).
	aggregateLimit int64

	// Sliding window: current minute bucket.
	windowStart time.Time
	typeCounts  map[RMMEventType]int64
	aggCount    int64

	// Metrics: total shed events (for observability).
	shedCount atomic.Int64
}

// NewRateLimiter creates a rate limiter with the given limits.
// perType is the max tasks/min per event type; aggregate is the total.
func NewRateLimiter(perType, aggregate int64) *RateLimiter {
	return &RateLimiter{
		perTypeLimit:   perType,
		aggregateLimit: aggregate,
		windowStart:    time.Now().Truncate(time.Minute),
		typeCounts:     make(map[RMMEventType]int64),
	}
}

// Allow returns true if the event type is within both per-type and
// aggregate limits. Increments counters when allowed.
func (rl *RateLimiter) Allow(et RMMEventType) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.rotateIfNeeded()

	// Check aggregate limit.
	if rl.aggCount >= rl.aggregateLimit {
		rl.shedCount.Add(1)
		return false
	}

	// Check per-type limit.
	if rl.typeCounts[et] >= rl.perTypeLimit {
		rl.shedCount.Add(1)
		return false
	}

	// Allow — increment.
	rl.typeCounts[et]++
	rl.aggCount++
	return true
}

// rotateIfNeeded advances to a new minute bucket if the current window
// has expired.
func (rl *RateLimiter) rotateIfNeeded() {
	now := time.Now().Truncate(time.Minute)
	if now.After(rl.windowStart) {
		rl.windowStart = now
		rl.typeCounts = make(map[RMMEventType]int64)
		rl.aggCount = 0
	}
}

// ShedCount returns the total number of shed (rate-limited) events.
func (rl *RateLimiter) ShedCount() int64 {
	return rl.shedCount.Load()
}

// CurrentCount returns the aggregate count in the current window.
func (rl *RateLimiter) CurrentCount() int64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rotateIfNeeded()
	return rl.aggCount
}

// ============================================================
// Circuit Breaker — halts task creation when gateway is failing.
// ============================================================

// CircuitState represents the circuit breaker state.
type CircuitState int

const (
	CircuitClosed   CircuitState = 0 // normal operation
	CircuitOpen     CircuitState = 1 // tripped — rejecting
	CircuitHalfOpen CircuitState = 2 // probe — allow one request
)

// CircuitBreaker tracks gateway health and halts task creation
// when too many failures occur.
type CircuitBreaker struct {
	mu            sync.Mutex
	state         CircuitState
	failureCount  int
	successCount  int
	threshold     int           // failures to trip
	recoveryTime  time.Duration // how long to stay open
	trippedAt     time.Time
	halfOpenAllow bool          // allow one probe in half-open
}

// NewCircuitBreaker creates a circuit breaker.
func NewCircuitBreaker(threshold int, recoveryTime time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        CircuitClosed,
		threshold:    threshold,
		recoveryTime: recoveryTime,
	}
}

// Allow returns true if the circuit is closed or half-open (and probe is allowed).
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.trippedAt) > cb.recoveryTime {
			cb.state = CircuitHalfOpen
			cb.halfOpenAllow = true
		}
		return false
	case CircuitHalfOpen:
		if cb.halfOpenAllow {
			cb.halfOpenAllow = false
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful task creation.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		cb.failureCount = 0
		cb.successCount = 0
		return
	}
	cb.failureCount = 0
	cb.successCount++
}

// RecordFailure records a failed task creation.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.successCount = 0
	if cb.failureCount >= cb.threshold {
		cb.state = CircuitOpen
		cb.trippedAt = time.Now()
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
