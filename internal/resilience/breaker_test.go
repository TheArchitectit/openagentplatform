package resilience

import (
	"errors"
	"testing"
	"time"
)

// TestCircuitBreakerOpens verifies that after MaxFailures consecutive failures
// the breaker trips to StateOpen and subsequent calls are short-circuited with
// ErrOpen without invoking the supplied function.
func TestCircuitBreakerOpens(t *testing.T) {
	cb := NewCircuitBreaker(BreakerConfig{
		Name:        "test-opens",
		MaxFailures: 3,
		OpenDuration: 100 * time.Millisecond,
		HalfOpenMax:  1,
	})

	if got := cb.State(); got != StateClosed {
		t.Fatalf("initial state: got %s, want closed", got)
	}

	boom := errors.New("boom")

	// Drive three failures. The breaker should still be closed after each
	// call (we count consecutive failures), and open after the third.
	for i := 0; i < 3; i++ {
		if err := cb.Execute(func() error { return boom }); !errors.Is(err, boom) {
			t.Fatalf("call %d: expected boom, got %v", i, err)
		}
	}

	if got := cb.State(); got != StateOpen {
		t.Fatalf("after 3 failures: got %s, want open", got)
	}

	// Now the breaker is open: Execute must short-circuit with ErrOpen
	// without invoking fn. We use a sentinel fn error to detect any
	// accidental invocation.
	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrOpen) {
		t.Errorf("expected ErrOpen while breaker is open, got %v", err)
	}
	if called {
		t.Error("Execute invoked fn while breaker was open")
	}
}

// TestCircuitBreakerHalfOpen verifies that after the configured cooldown
// elapses the breaker transitions to half-open and a successful probe closes it
// permanently.
func TestCircuitBreakerHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(BreakerConfig{
		Name:         "test-half-open",
		MaxFailures:  2,
		OpenDuration: 20 * time.Millisecond,
		HalfOpenMax:  1,
	})

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return errors.New("fail") })
	}
	if got := cb.State(); got != StateOpen {
		t.Fatalf("expected open, got %s", got)
	}

	// Wait past the cooldown so the next Execute moves us into half-open.
	time.Sleep(30 * time.Millisecond)

	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("probe call in half-open: expected nil error, got %v", err)
	}

	if got := cb.State(); got != StateClosed {
		t.Errorf("after successful probe: got %s, want closed", got)
	}
}

// TestCircuitBreakerCloses verifies that once the breaker has been reset to
// closed, normal operation resumes — a single failure no longer trips the
// breaker, and successes do not affect state.
func TestCircuitBreakerCloses(t *testing.T) {
	cb := NewCircuitBreaker(BreakerConfig{
		Name:         "test-closes",
		MaxFailures:  3,
		OpenDuration: 10 * time.Millisecond,
		HalfOpenMax:  1,
	})

	// Trip, then recover.
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return errors.New("fail") })
	}
	time.Sleep(15 * time.Millisecond)
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("recovery probe: %v", err)
	}
	if got := cb.State(); got != StateClosed {
		t.Fatalf("after recovery: got %s, want closed", got)
	}

	// Single failure should NOT trip the breaker again.
	if err := cb.Execute(func() error { return errors.New("transient") }); err == nil {
		t.Fatal("expected an error from the failed call")
	}
	if got := cb.State(); got != StateClosed {
		t.Errorf("after 1 post-recovery failure: got %s, want closed", got)
	}

	// And a string of successes must keep the breaker closed.
	for i := 0; i < 5; i++ {
		if err := cb.Execute(func() error { return nil }); err != nil {
			t.Fatalf("post-recovery success %d: %v", i, err)
		}
	}
	if got := cb.State(); got != StateClosed {
		t.Errorf("after steady-state successes: got %s, want closed", got)
	}
}
