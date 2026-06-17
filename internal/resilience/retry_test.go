package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetrySuccess: the wrapped function succeeds on the first attempt. The
// retry helper must not invoke the function more than once.
func TestRetrySuccess(t *testing.T) {
	calls := 0
	cfg := RetryConfig{
		MaxAttempts:    3,
		BaseBackoff:    1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		JitterFraction: 0,
	}
	err := cfg.Do(context.Background(), func(_ context.Context, _ int) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

// TestRetryWithFailures: the wrapped function fails twice then succeeds. The
// retry helper must invoke it exactly 3 times and return nil.
func TestRetryWithFailures(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	cfg := RetryConfig{
		MaxAttempts:    5,
		BaseBackoff:    1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		JitterFraction: 0,
		IsRetryable: func(err error) bool {
			return errors.Is(err, transient)
		},
	}
	err := cfg.Do(context.Background(), func(_ context.Context, _ int) error {
		calls++
		if calls < 3 {
			return transient
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after eventual success, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

// TestRetryExceedsMax: when the wrapped function keeps failing, the retry helper
// must stop after MaxAttempts and return the last error.
func TestRetryExceedsMax(t *testing.T) {
	boom := errors.New("always-fails")
	calls := 0
	cfg := RetryConfig{
		MaxAttempts:    4,
		BaseBackoff:    1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		JitterFraction: 0,
		IsRetryable:    func(err error) bool { return true },
	}
	err := cfg.Do(context.Background(), func(_ context.Context, _ int) error {
		calls++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if calls != 4 {
		t.Errorf("expected exactly MaxAttempts=4 calls, got %d", calls)
	}
}

// TestRetryContextCancelled: when the context is cancelled mid-flight, the
// retry helper must stop calling the function and surface the context error.
func TestRetryContextCancelled(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())

	transient := errors.New("transient")
	cfg := RetryConfig{
		MaxAttempts:    10,
		BaseBackoff:    1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		JitterFraction: 0,
		IsRetryable:    func(err error) bool { return errors.Is(err, transient) },
	}

	// Cancel after the first failure so the second attempt never runs.
	err := cfg.Do(ctx, func(ctx context.Context, _ int) error {
		calls++
		if calls == 1 {
			cancel()
			// Give the cancellation a moment to propagate before
			// returning the retryable error.
			select {
			case <-time.After(2 * time.Millisecond):
			case <-ctx.Done():
			}
		}
		return transient
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls > 2 {
		t.Errorf("retry should have stopped after cancellation; got %d calls", calls)
	}
}
