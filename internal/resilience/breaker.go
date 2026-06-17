// Package resilience – breaker.go.
//
// Circuit breaker that protects downstream dependencies from
// cascading failures.  The breaker transitions through three states:
//
//	closed   – requests pass through; consecutive failures are
//	           counted.  When the count reaches MaxFailures the
//	           breaker trips to open.
//	open     – requests are rejected immediately with ErrOpen.
//	           After OpenDuration elapses the breaker moves to
//	half-open.
//	half-open – up to HalfOpenMax probe requests are allowed.
//	           The first success closes the breaker; the first
//	           failure re-opens it for another full OpenDuration.
//
// State transitions are reported via a callback hook so the caller
// can emit metrics or log events.
package resilience

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrOpen is returned by Execute when the circuit is open and the
// call is short-circuited.
var ErrOpen = errors.New("resilience: circuit breaker is open")

// BreakerConfig holds tuning parameters for a CircuitBreaker.
type BreakerConfig struct {
	// Name identifies the breaker in logs and metrics.  Required.
	Name string

	// MaxFailures is the number of consecutive failures in the
	// closed state that trip the breaker.  Default 5.
	MaxFailures int

	// OpenDuration is how long the breaker stays open before
	// transitioning to half-open.  Default 30s.
	OpenDuration time.Duration

	// HalfOpenMax is the maximum number of probe calls allowed
	// in the half-open state.  Default 1.
	HalfOpenMax int

	// Logger receives structured state-transition events.  When
	// nil, slog.Default() is used.
	Logger *slog.Logger

	// OnStateChange, if set, is invoked synchronously whenever
	// the breaker transitions between states.  Use this to emit
	// Prometheus metrics.
	OnStateChange func(name string, from, to BreakerState)
}

// BreakerState represents the current state of a circuit breaker.
type BreakerState int32

// Breaker states.
const (
	StateClosed BreakerState = iota
	StateOpen
	StateHalfOpen
)

// String returns a human-readable name for a BreakerState.
func (s BreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker is a concurrency-safe three-state circuit breaker.
type CircuitBreaker struct {
	cfg BreakerConfig

	mu              sync.Mutex
	state           atomic.Int32 // BreakerState
	consecFailures  int
	halfOpenCount   int
	openUntil       time.Time
	halfOpenSuccess bool
}

// NewCircuitBreaker constructs a CircuitBreaker and returns it in the
// closed state.
func NewCircuitBreaker(cfg BreakerConfig) *CircuitBreaker {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = 1
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	cb := &CircuitBreaker{cfg: cfg}
	cb.state.Store(int32(StateClosed))
	return cb
}

// State returns the current breaker state.
func (cb *CircuitBreaker) State() BreakerState {
	return BreakerState(cb.state.Load())
}

// Execute runs fn under the protection of the circuit breaker.  When
// the breaker is open, fn is not called and ErrOpen is returned.
// When fn returns an error, the breaker records a failure.  When fn
// returns nil, the breaker records a success.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allow() {
		return ErrOpen
	}

	err := fn()
	cb.record(err)
	return err
}

// allow checks whether the call may proceed.  It also performs
// time-based transitions from open to half-open.
func (cb *CircuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch BreakerState(cb.state.Load()) {
	case StateClosed:
		return true

	case StateOpen:
		if time.Now().After(cb.openUntil) {
			cb.transition(StateHalfOpen)
			cb.halfOpenCount = 0
			cb.halfOpenSuccess = false
			// Allow this single probe request through.
			cb.halfOpenCount++
			return true
		}
		return false

	case StateHalfOpen:
		if cb.halfOpenCount < cb.cfg.HalfOpenMax {
			cb.halfOpenCount++
			return true
		}
		return false

	default:
		return false
	}
}

// record updates internal counters after a call completes.  The
// passed error is nil on success.
func (cb *CircuitBreaker) record(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	current := BreakerState(cb.state.Load())

	if err == nil {
		switch current {
		case StateHalfOpen:
			cb.halfOpenSuccess = true
			cb.transition(StateClosed)
			cb.consecFailures = 0
		case StateClosed:
			cb.consecFailures = 0
		}
		return
	}

	// Failure path.
	switch current {
	case StateClosed:
		cb.consecFailures++
		if cb.consecFailures >= cb.cfg.MaxFailures {
			cb.openUntil = time.Now().Add(cb.cfg.OpenDuration)
			cb.transition(StateOpen)
		}
	case StateHalfOpen:
		// Any failure in half-open re-opens the breaker.
		cb.openUntil = time.Now().Add(cb.cfg.OpenDuration)
		cb.transition(StateOpen)
	}
}

// transition atomically swaps state and fires the callback hook.
func (cb *CircuitBreaker) transition(to BreakerState) {
	from := BreakerState(cb.state.Swap(int32(to)))
	if from == to {
		return
	}
	if cb.cfg.OnStateChange != nil {
		cb.cfg.OnStateChange(cb.cfg.Name, from, to)
	}
	cb.cfg.Logger.Info("circuit breaker state change",
		"name", cb.cfg.Name,
		"from", from.String(),
		"to", to.String(),
	)
}

// IsBreakerError reports whether err originates from the breaker
// (i.e. ErrOpen) as opposed to the wrapped operation.
func IsBreakerError(err error) bool {
	return errors.Is(err, ErrOpen)
}

// ErrBreakerConfig is returned when breaker configuration is invalid.
var ErrBreakerConfig = errors.New("resilience: invalid breaker config")

// Validate checks that the configuration is usable.
func (c BreakerConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("%w: name is required", ErrBreakerConfig)
	}
	return nil
}
