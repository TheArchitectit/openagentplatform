package bridge

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// Configuration
// ============================================================

const (
	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 60 * time.Second

	// DefaultMaxRetries is the max retry count for 5xx errors.
	DefaultMaxRetries = 3

	// DefaultCircuitBreakerThreshold is the number of consecutive failures
	// that trip the circuit breaker.
	DefaultCircuitBreakerThreshold = 5

	// DefaultCircuitBreakerCooldown is how long the circuit stays open
	// before allowing a probe request.
	DefaultCircuitBreakerCooldown = 30 * time.Second
)

// ClientConfig holds AdapterClient configuration. Zero-value fields are
// replaced with sensible defaults.

type ClientConfig struct {
	// BaseURL is the root URL of the Python adapter service.
	// Default: "http://localhost:8001".
	BaseURL string

	// Timeout is the per-request timeout. Default: 60s.
	Timeout time.Duration

	// AuthToken is a bearer token sent in the Authorization header.
	// Empty = no auth.
	AuthToken string

	// MaxRetries is the max retry count for 5xx responses.
	// Default: 3.
	MaxRetries int

	// CircuitBreakerThreshold is the consecutive failure count that
	// opens the circuit. Default: 5.
	CircuitBreakerThreshold int

	// CircuitBreakerCooldown is the open-circuit duration.
	// Default: 30s.
	CircuitBreakerCooldown time.Duration

	// Logger is an optional structured logger. Nil = silent.
	Logger *slog.Logger

	// HTTPClient is an optional custom *http.Client. Nil = uses Timeout.
	HTTPClient *http.Client
}

// ============================================================
// Errors
// ============================================================

var (
	// ErrCircuitOpen is returned when the circuit breaker is open.
	ErrCircuitOpen = errors.New("bridge: circuit breaker is open")

	// ErrAdapterNotFound is returned when a named adapter is unknown.
	ErrAdapterNotFound = errors.New("bridge: adapter not found")

	// ErrClientNotConfigured is returned for nil receivers.
	ErrClientNotConfigured = errors.New("bridge: client is nil")

	// ErrStreamCanceled is returned when a stream is canceled by the caller.
	ErrStreamCanceled = errors.New("bridge: stream canceled")
)

// ============================================================
// Circuit breaker
// ============================================================

// circuitState represents the state of the circuit breaker.
type circuitState int32

const (
	circuitClosed   circuitState = 0
	circuitOpen     circuitState = 1
	circuitHalfOpen circuitState = 2
)

// circuitBreaker implements a simple consecutive-failure circuit breaker.
type circuitBreaker struct {
	threshold int
	cooldown  time.Duration

	mu       sync.Mutex
	failures int
	state    circuitState
	openedAt time.Time
}

// newCircuitBreaker creates a circuit breaker with the given threshold
// and cooldown.
func newCircuitBreaker(threshold int, cooldown time.Duration) *circuitBreaker {
	return &circuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
		state:     circuitClosed,
	}
}

// allow returns true if a request should be permitted.
func (cb *circuitBreaker) allow() bool {
	if cb == nil {
		return true
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Since(cb.openedAt) >= cb.cooldown {
			cb.state = circuitHalfOpen
			return true
		}
		return false
	case circuitHalfOpen:
		return true
	}
	return false
}

// recordSuccess resets the failure count and closes the circuit.
func (cb *circuitBreaker) recordSuccess() {
	if cb == nil {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = circuitClosed
}

// recordFailure increments the failure count and opens the circuit
// if the threshold is reached.
func (cb *circuitBreaker) recordFailure() {
	if cb == nil {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.failures >= cb.threshold {
		cb.state = circuitOpen
		cb.openedAt = time.Now()
	}
}

// State returns the current circuit state (for diagnostics).
func (cb *circuitBreaker) State() circuitState {
	if cb == nil {
		return circuitClosed
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// ============================================================
// AdapterClient
// ============================================================

// AdapterClient is the HTTP client for the Python adapter service.
// It is safe for concurrent use.
type AdapterClient struct {
	baseURL    string
	timeout    time.Duration
	authToken  string
	maxRetries int
	httpClient *http.Client
	log        *slog.Logger

	cb *circuitBreaker

	// requestID is an atomic counter used to correlate log entries.
	requestID atomic.Uint64
}

// NewAdapterClient constructs an AdapterClient with the given config.
