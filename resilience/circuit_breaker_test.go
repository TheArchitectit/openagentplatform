package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewCircuitBreakerRejectsInvalidConfig(t *testing.T) {
	tests := []CircuitBreakerConfig{
		{RecoveryTimeout: time.Second},
		{FailureThreshold: 1},
	}

	for _, config := range tests {
		_, err := NewCircuitBreaker(config)
		if !errors.Is(err, ErrInvalidCircuitBreakerConfig) {
			t.Fatalf("NewCircuitBreaker(%+v) error = %v, want ErrInvalidCircuitBreakerConfig", config, err)
		}
	}
}

func TestCircuitBreakerOpensAtFailureThreshold(t *testing.T) {
	breaker := newTestBreaker(t, 2, time.Hour)
	operationErr := errors.New("dependency unavailable")

	for range 2 {
		if err := breaker.Execute(context.Background(), func(context.Context) error {
			return operationErr
		}); !errors.Is(err, operationErr) {
			t.Fatalf("Execute() error = %v, want %v", err, operationErr)
		}
	}

	called := false
	err := breaker.Execute(context.Background(), func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Execute() error = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Fatal("operation ran while circuit was open")
	}
}

func TestCircuitBreakerHalfOpenProbeSuccessClosesCircuit(t *testing.T) {
	breaker := newTestBreaker(t, 1, 10*time.Millisecond)
	operationErr := errors.New("dependency unavailable")
	_ = breaker.Execute(context.Background(), func(context.Context) error { return operationErr })

	time.Sleep(15 * time.Millisecond)
	if err := breaker.Execute(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("half-open Execute() error = %v", err)
	}
	if state := breaker.State(); state != StateClosed {
		t.Fatalf("State() = %v, want StateClosed", state)
	}
}

func TestCircuitBreakerHalfOpenAllowsOneProbe(t *testing.T) {
	breaker := newTestBreaker(t, 1, 5*time.Millisecond)
	operationErr := errors.New("dependency unavailable")
	_ = breaker.Execute(context.Background(), func(context.Context) error { return operationErr })
	time.Sleep(10 * time.Millisecond)

	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- breaker.Execute(context.Background(), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	var calls atomic.Int32
	err := breaker.Execute(context.Background(), func(context.Context) error {
		calls.Add(1)
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("concurrent Execute() error = %v, want ErrCircuitOpen", err)
	}
	if calls.Load() != 0 {
		t.Fatal("second half-open probe ran")
	}

	close(release)
	if err := <-result; err != nil {
		t.Fatalf("probe Execute() error = %v", err)
	}
}

func TestCircuitBreakerFailedProbeReopensCircuit(t *testing.T) {
	breaker := newTestBreaker(t, 1, 5*time.Millisecond)
	operationErr := errors.New("dependency unavailable")
	_ = breaker.Execute(context.Background(), func(context.Context) error { return operationErr })
	time.Sleep(10 * time.Millisecond)

	if err := breaker.Execute(context.Background(), func(context.Context) error { return operationErr }); !errors.Is(err, operationErr) {
		t.Fatalf("probe Execute() error = %v, want %v", err, operationErr)
	}
	if state := breaker.State(); state != StateOpen {
		t.Fatalf("State() = %v, want StateOpen", state)
	}
}

func newTestBreaker(t *testing.T, threshold uint, recovery time.Duration) *CircuitBreaker {
	t.Helper()
	breaker, err := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: threshold,
		RecoveryTimeout:  recovery,
	})
	if err != nil {
		t.Fatalf("NewCircuitBreaker() error = %v", err)
	}
	return breaker
}
