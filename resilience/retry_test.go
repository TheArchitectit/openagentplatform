package resilience

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRetryReturnsAfterSuccess(t *testing.T) {
	config := RetryConfig{MaxAttempts: 4, InitialDelay: time.Millisecond, MaxDelay: time.Second}
	attempts := 0
	var delays []time.Duration

	err := retry(context.Background(), config, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}, func() float64 { return 0.5 }, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	if err != nil {
		t.Fatalf("retry() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if want := []time.Duration{time.Millisecond, 2 * time.Millisecond}; !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
}

func TestRetryReturnsLastError(t *testing.T) {
	lastErr := errors.New("still unavailable")
	attempts := 0
	config := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Second}

	err := retry(context.Background(), config, func(context.Context) error {
		attempts++
		return lastErr
	}, func() float64 { return 0.5 }, func(context.Context, time.Duration) error { return nil })
	if !errors.Is(err, lastErr) {
		t.Fatalf("retry() error = %v, want %v", err, lastErr)
	}
	if attempts != config.MaxAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, config.MaxAttempts)
	}
}

func TestRetryCapsDelayAndAppliesJitter(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     250 * time.Millisecond,
		Jitter:       0.5,
	}
	var delays []time.Duration

	_ = retry(context.Background(), config, func(context.Context) error {
		return errors.New("temporary failure")
	}, func() float64 { return 0 }, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 125 * time.Millisecond, 125 * time.Millisecond}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
}

func TestRetryStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Second}

	err := retry(ctx, config, func(context.Context) error {
		cancel()
		return errors.New("temporary failure")
	}, func() float64 { return 0.5 }, sleep)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry() error = %v, want context.Canceled", err)
	}
}

func TestRetryRejectsInvalidConfig(t *testing.T) {
	tests := []RetryConfig{
		{},
		{MaxAttempts: 1, InitialDelay: -time.Second},
		{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Millisecond},
		{MaxAttempts: 1, Jitter: -0.1},
		{MaxAttempts: 1, Jitter: 1.1},
	}

	for _, config := range tests {
		err := Retry(context.Background(), config, func(context.Context) error { return nil })
		if !errors.Is(err, ErrInvalidRetryConfig) {
			t.Fatalf("Retry(%+v) error = %v, want ErrInvalidRetryConfig", config, err)
		}
	}
}
