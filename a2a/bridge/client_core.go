package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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
