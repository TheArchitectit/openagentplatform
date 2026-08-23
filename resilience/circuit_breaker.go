// Package resilience provides concurrency-safe primitives for handling transient failures.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State is the operating state of a CircuitBreaker.
type State uint8

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

var (
	// ErrCircuitOpen is returned when the circuit is rejecting calls.
	ErrCircuitOpen = errors.New("resilience: circuit breaker is open")
	// ErrInvalidCircuitBreakerConfig is returned for invalid breaker settings.
	ErrInvalidCircuitBreakerConfig = errors.New("resilience: invalid circuit breaker config")
	// ErrNilOperation is returned when Execute is called with a nil function.
	ErrNilOperation = errors.New("resilience: operation is nil")
)

// CircuitBreakerConfig controls when a circuit opens and recovers.
type CircuitBreakerConfig struct {
	FailureThreshold uint
	RecoveryTimeout  time.Duration
}

// CircuitBreaker prevents repeated calls to an unhealthy dependency.
type CircuitBreaker struct {
	mu sync.Mutex

	failureThreshold uint
	recoveryTimeout  time.Duration

	state         State
	failures      uint
	openedAt      time.Time
	halfOpenProbe bool
}

// NewCircuitBreaker creates a closed circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) (*CircuitBreaker, error) {
	if config.FailureThreshold == 0 {
		return nil, fmt.Errorf("%w: failure threshold must be greater than zero", ErrInvalidCircuitBreakerConfig)
	}
	if config.RecoveryTimeout <= 0 {
		return nil, fmt.Errorf("%w: recovery timeout must be greater than zero", ErrInvalidCircuitBreakerConfig)
	}

	return &CircuitBreaker{
		failureThreshold: config.FailureThreshold,
		recoveryTimeout:  config.RecoveryTimeout,
		state:            StateClosed,
	}, nil
}

// Execute runs operation when the circuit permits a call.
func (breaker *CircuitBreaker) Execute(ctx context.Context, operation func(context.Context) error) error {
	if operation == nil {
		return ErrNilOperation
	}
	if !breaker.allow(time.Now()) {
		return ErrCircuitOpen
	}

	err := operation(ctx)
	if err != nil {
		breaker.recordFailure(time.Now())
		return err
	}

	breaker.recordSuccess()
	return nil
}

// State returns the current circuit state, applying a pending recovery transition.
func (breaker *CircuitBreaker) State() State {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	breaker.transitionToHalfOpen(time.Now())
	return breaker.state
}

func (breaker *CircuitBreaker) allow(now time.Time) bool {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	breaker.transitionToHalfOpen(now)
	switch breaker.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		if breaker.halfOpenProbe {
			return false
		}
		breaker.halfOpenProbe = true
		return true
	default:
		return false
	}
}

func (breaker *CircuitBreaker) transitionToHalfOpen(now time.Time) {
	if breaker.state == StateOpen && now.Sub(breaker.openedAt) >= breaker.recoveryTimeout {
		breaker.state = StateHalfOpen
		breaker.halfOpenProbe = false
	}
}

func (breaker *CircuitBreaker) recordSuccess() {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	// Only reset failure count when in Closed state (normal operation).
	// In HalfOpen state, a successful probe closes the circuit but we
	// should not unconditionally overwrite state — a concurrent failure
	// may have already opened it.
	switch breaker.state {
	case StateClosed:
		breaker.failures = 0
	case StateHalfOpen:
		// Probe succeeded — close the circuit.
		breaker.state = StateClosed
		breaker.failures = 0
		breaker.halfOpenProbe = false
	case StateOpen:
		// Should not happen (open circuit rejects calls), but guard
		// against a race between allow() and recordSuccess().
	}
}

func (breaker *CircuitBreaker) recordFailure(now time.Time) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	if breaker.state == StateHalfOpen {
		breaker.open(now)
		return
	}

	breaker.failures++
	if breaker.failures >= breaker.failureThreshold {
		breaker.open(now)
	}
}

func (breaker *CircuitBreaker) open(now time.Time) {
	breaker.state = StateOpen
	breaker.openedAt = now
	breaker.halfOpenProbe = false
}
