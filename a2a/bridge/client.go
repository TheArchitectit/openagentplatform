// Package bridge - client.go implements the HTTP client for the Python
// adapter service. It provides synchronous invoke, streaming, cancel,
// adapter discovery, and cost/budget query methods with exponential
// backoff retry on 5xx errors and a circuit breaker that trips after
// consecutive failures.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
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

	mu         sync.Mutex
	failures   int
	state      circuitState
	openedAt   time.Time
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
func NewAdapterClient(cfg ClientConfig) *AdapterClient {
	base := cfg.BaseURL
	if base == "" {
		base = "http://localhost:8001"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = DefaultMaxRetries
	}
	threshold := cfg.CircuitBreakerThreshold
	if threshold <= 0 {
		threshold = DefaultCircuitBreakerThreshold
	}
	cooldown := cfg.CircuitBreakerCooldown
	if cooldown <= 0 {
		cooldown = DefaultCircuitBreakerCooldown
	}

	var httpClient *http.Client
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	} else {
		httpClient = &http.Client{Timeout: timeout}
	}

	return &AdapterClient{
		baseURL:    strings.TrimRight(base, "/"),
		timeout:    timeout,
		authToken:  cfg.AuthToken,
		maxRetries: maxRetries,
		httpClient: httpClient,
		log:        cfg.Logger,
		cb:         newCircuitBreaker(threshold, cooldown),
	}
}

// BaseURL returns the configured base URL.
func (c *AdapterClient) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// CircuitState returns the current circuit breaker state.
func (c *AdapterClient) CircuitState() string {
	if c == nil || c.cb == nil {
		return "closed"
	}
	switch c.cb.State() {
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// ============================================================
// HTTP helpers
// ============================================================

// doRequest performs an HTTP request with circuit breaker and retry.
// It returns the response body and status code. Caller must close body
// if err is nil.
func (c *AdapterClient) doRequest(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	if c == nil {
		return nil, nil, ErrClientNotConfigured
	}

	if !c.cb.allow() {
		return nil, nil, ErrCircuitOpen
	}

	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.cb.recordFailure()
			return nil, nil, fmt.Errorf("bridge: marshal request: %w", err)
		}
		bodyBytes = b
	}

	fullURL := c.baseURL + path
	reqID := c.requestID.Add(1)

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms, ...
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				c.cb.recordFailure()
				return nil, nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			c.cb.recordFailure()
			return nil, nil, fmt.Errorf("bridge: create request: %w", err)
		}

		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		if c.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.authToken)
		}
		req.Header.Set("X-Request-ID", fmt.Sprintf("bridge-%d", reqID))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if c.log != nil {
				c.log.Warn("bridge: request error",
					"req_id", reqID,
					"method", method,
					"url", fullURL,
					"attempt", attempt,
					"err", err,
				)
			}
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if readErr != nil {
			lastErr = readErr
			continue
		}

		// 5xx -> retry
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("bridge: server error %d: %s", resp.StatusCode, string(respBody))
			if c.log != nil {
				c.log.Warn("bridge: server error",
					"req_id", reqID,
					"status", resp.StatusCode,
					"attempt", attempt,
				)
			}
			continue
		}

		// Success or 4xx
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.cb.recordSuccess()
		} else {
			// 4xx: do not retry, but also do not count as a circuit failure
			return resp, respBody, nil
		}

		return resp, respBody, nil
	}

	// All retries exhausted
	c.cb.recordFailure()
	return nil, nil, fmt.Errorf("bridge: request failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

// ============================================================
// Invoke
// ============================================================

// Invoke sends a synchronous invocation request to the adapter service.
// POST /api/v1/adapters/invoke
func (c *AdapterClient) Invoke(ctx context.Context, adapter string, messages []Part) (*InvokeResponse, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	req := &InvokeRequest{
		AdapterName: adapter,
		Messages:    messages,
	}

	_, body, err := c.doRequest(ctx, http.MethodPost, "/api/v1/adapters/invoke", req)
	if err != nil {
		return nil, err
	}

	var resp InvokeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal invoke response: %w", err)
	}
	return &resp, nil
}

// ============================================================
// Stream
// ============================================================

// Stream sends a streaming invocation request. It returns a channel of
// StreamEvent and a cancel function. The channel is closed when the
// stream completes or an error occurs. The cancel function aborts the
// underlying HTTP request.
//
// POST /api/v1/adapters/stream (SSE response)
func (c *AdapterClient) Stream(ctx context.Context, adapter string, messages []Part) (<-chan StreamEvent, func(), error) {
	if c == nil {
		return nil, nil, ErrClientNotConfigured
	}

	if !c.cb.allow() {
		return nil, nil, ErrCircuitOpen
	}

	req := &StreamRequest{
		AdapterName: adapter,
		Messages:    messages,
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		c.cb.recordFailure()
		return nil, nil, fmt.Errorf("bridge: marshal stream request: %w", err)
	}

	fullURL := c.baseURL + "/api/v1/adapters/stream"
	reqID := c.requestID.Add(1)

	// Use a separate context for the HTTP request so we can cancel
	// it independently of the caller's context.
	streamCtx, cancel := context.WithCancel(ctx)

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		cancel()
		c.cb.recordFailure()
		return nil, nil, fmt.Errorf("bridge: create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	httpReq.Header.Set("X-Request-ID", fmt.Sprintf("bridge-stream-%d", reqID))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		cancel()
		c.cb.recordFailure()
		return nil, nil, fmt.Errorf("bridge: stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode >= 500 {
			c.cb.recordFailure()
		}
		return nil, nil, fmt.Errorf("bridge: stream status %d: %s", resp.StatusCode, string(respBody))
	}

	c.cb.recordSuccess()

	events := make(chan StreamEvent, 32)

	go func() {
		defer close(events)
		defer cancel()
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			// SSE format: "data: <json>"
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				if c.log != nil {
					c.log.Warn("bridge: parse SSE event",
						"req_id", reqID,
						"err", err,
					)
				}
				continue
			}

			select {
			case events <- event:
			case <-streamCtx.Done():
				return
			}

			// Terminal events end the stream
			if event.EventType == "done" || event.EventType == "error" {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if c.log != nil {
				c.log.Warn("bridge: stream read error",
					"req_id", reqID,
					"err", err,
				)
			}
		}
	}()

	cancelFunc := func() {
		cancel()
	}

	return events, cancelFunc, nil
}

// ============================================================
// Cancel
// ============================================================

// Cancel cancels a running adapter task.
// POST /api/v1/adapters/{taskId}/cancel
func (c *AdapterClient) Cancel(ctx context.Context, adapter, taskID string) (bool, error) {
	if c == nil {
		return false, ErrClientNotConfigured
	}

	path := fmt.Sprintf("/api/v1/adapters/%s/cancel", url.PathEscape(taskID))
	body := &CancelRequest{Reason: "cancelled via A2A bridge"}

	_, respBody, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return false, err
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return false, fmt.Errorf("bridge: unmarshal cancel response: %w", err)
	}

	if !resp.Success && resp.Error != "" {
		return false, fmt.Errorf("bridge: cancel failed: %s", resp.Error)
	}
	return resp.Success, nil
}

// ============================================================
// Adapter discovery
// ============================================================

// ListAdapters returns all available adapters from the Python service.
// GET /api/v1/adapters
func (c *AdapterClient) ListAdapters(ctx context.Context) ([]AdapterInfo, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	_, body, err := c.doRequest(ctx, http.MethodGet, "/api/v1/adapters", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Adapters []AdapterInfo `json:"adapters"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal adapters response: %w", err)
	}
	if resp.Adapters == nil {
		resp.Adapters = []AdapterInfo{}
	}
	return resp.Adapters, nil
}

// GetAdapterCard retrieves the AgentCard for a named adapter.
// GET /api/v1/adapters/{name}/card
func (c *AdapterClient) GetAdapterCard(ctx context.Context, name string) (*models.AgentCard, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	path := fmt.Sprintf("/api/v1/adapters/%s/card", url.PathEscape(name))
	_, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var info AdapterInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal adapter card: %w", err)
	}
	return AgentCardFromAdapter(&info), nil
}

// GetAdapterHealth checks the health of a named adapter.
// GET /api/v1/adapters/{name}/health
func (c *AdapterClient) GetAdapterHealth(ctx context.Context, name string) (*HealthStatus, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	path := fmt.Sprintf("/api/v1/adapters/%s/health", url.PathEscape(name))
	_, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var health HealthStatus
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal health response: %w", err)
	}
	return &health, nil
}

// ============================================================
// Cost / Budget
// ============================================================

// GetCostUsage retrieves cost usage data for an org within a time window.
// GET /api/v1/cost/usage?org_id=...&from=...&to=...
func (c *AdapterClient) GetCostUsage(ctx context.Context, orgID string, from, to time.Time) (*UsageReport, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	q := url.Values{}
	if orgID != "" {
		q.Set("org_id", orgID)
	}
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))

	path := "/api/v1/cost/usage"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	_, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var report UsageReport
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal usage report: %w", err)
	}
	return &report, nil
}

// GetBudgetStatus returns all configured cost budgets.
// GET /api/v1/cost/budgets
func (c *AdapterClient) GetBudgetStatus(ctx context.Context) ([]BudgetInfo, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	_, body, err := c.doRequest(ctx, http.MethodGet, "/api/v1/cost/budgets", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Budgets []BudgetInfo `json:"budgets"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal budgets response: %w", err)
	}
	if resp.Budgets == nil {
		resp.Budgets = []BudgetInfo{}
	}
	return resp.Budgets, nil
}
