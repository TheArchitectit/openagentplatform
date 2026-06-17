// Package resilience – retry.go.
//
// Context-aware retry helper with exponential backoff and jitter.
// Callers supply a predicate that decides whether an error is
// retryable.  The helper aborts immediately when the caller's context
// is cancelled, so it is safe to use in request-scoped code paths.
package resilience

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// RetryConfig controls the retry behaviour.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts including the
	// first call.  Default 3.
	MaxAttempts int

	// BaseBackoff is the delay before the first retry.  Default
	// 100ms.  Subsequent retries double this value.
	BaseBackoff time.Duration

	// MaxBackoff caps the per-retry delay.  Default 10s.
	MaxBackoff time.Duration

	// JitterFraction adds randomness to each backoff to avoid
	// thundering-herd effects.  Default 0.10 (10%).
	JitterFraction float64

	// IsRetryable decides whether an error should trigger a
	// retry.  When nil, every non-nil error is retried.
	IsRetryable func(error) bool
}

// DefaultRetryConfig returns 3 attempts, 100ms base, 200ms/400ms
// exponential schedule with 10% jitter.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		BaseBackoff:    100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		JitterFraction: 0.10,
	}
}

// RetryableFunc is the signature of an operation that can be retried.
// It receives the current attempt number (1-indexed) and the context.
type RetryableFunc func(ctx context.Context, attempt int) error

// Do runs fn under the retry policy described by cfg.  It returns
// the last error encountered, or nil if all attempts succeeded.
// If the context is cancelled, the function returns immediately
// with ctx.Err() and the error wrapped with context.Canceled or
// context.DeadlineExceeded as appropriate.
func (cfg RetryConfig) Do(ctx context.Context, fn RetryableFunc) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 100 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 10 * time.Second
	}
	if cfg.JitterFraction < 0 {
		cfg.JitterFraction = 0
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return errors.Join(lastErr, err)
			}
			return err
		}

		err := fn(ctx, attempt)
		if err == nil {
			return nil
		}
		lastErr = err

		// Non-retryable error: stop immediately.
		if cfg.IsRetryable != nil && !cfg.IsRetryable(err) {
			return err
		}

		// Don't sleep after the final attempt.
		if attempt == cfg.MaxAttempts {
			break
		}

		delay := cfg.backoffFor(attempt)

		// Context-aware sleep.  select on ctx.Done so that
		// cancellation aborts the wait immediately.
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			if lastErr != nil {
				return errors.Join(lastErr, ctx.Err())
			}
			return ctx.Err()
		case <-t.C:
		}
	}
	return lastErr
}

// backoffFor computes the delay for the given attempt (1-indexed) using
// exponential growth and bounded jitter.  Attempt 1 returns
// BaseBackoff, attempt 2 returns 2x, attempt 3 returns 4x, and so on,
// capped at MaxBackoff.
func (cfg RetryConfig) backoffFor(attempt int) time.Duration {
	// Exponential: base * 2^(attempt-1).
	mult := 1 << (attempt - 1) // 1, 2, 4, 8, …
	delay := cfg.BaseBackoff * time.Duration(mult)
	if delay > cfg.MaxBackoff {
		delay = cfg.MaxBackoff
	}
	// Jitter: symmetric multiplicative jitter.
	if cfg.JitterFraction > 0 {
		jitter := float64(delay) * cfg.JitterFraction
		// rand.Float64() is in [0, 1) and we want [-jitter, +jitter].
		offset := (rand.Float64()*2 - 1) * jitter
		delay = time.Duration(float64(delay) + offset)
		if delay < 0 {
			delay = cfg.BaseBackoff
		}
	}
	return delay
}

// IsRetryableHTTP returns a predicate suitable for RetryConfig.IsRetryable
// that retries on transient network errors and 5xx status codes.
// The predicate expects a *RetryableHTTPError (see WithHTTPRetry).
func IsRetryableHTTP(err error) bool {
	if err == nil {
		return false
	}
	var r *RetryableHTTPError
	if errors.As(err, &r) {
		return r.StatusCode >= 500
	}
	return true
}

// RetryableHTTPError wraps an HTTP status code so the retry predicate
// can inspect it.
type RetryableHTTPError struct {
	StatusCode int
	Body       string
}

func (e *RetryableHTTPError) Error() string {
	return "retryable http error: status " + itoa(e.StatusCode)
}
