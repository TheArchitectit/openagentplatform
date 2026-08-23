package resilience

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

var ErrInvalidRetryConfig = errors.New("resilience: invalid retry config")

// RetryConfig controls retry attempts and exponential backoff.
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Jitter       float64
}

// Retry calls operation until it succeeds, the attempt limit is reached, or the context ends.
func Retry(ctx context.Context, config RetryConfig, operation func(context.Context) error) error {
	return retry(ctx, config, operation, rand.Float64, sleep)
}

func retry(
	ctx context.Context,
	config RetryConfig,
	operation func(context.Context) error,
	random func() float64,
	wait func(context.Context, time.Duration) error,
) error {
	if err := validateRetryConfig(config); err != nil {
		return err
	}
	if operation == nil {
		return errors.New("resilience: operation is nil")
	}

	var lastErr error
	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = operation(ctx)
		if lastErr == nil {
			return nil
		}
		if attempt == config.MaxAttempts {
			break
		}

		delay := retryDelay(config, attempt, random())
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}

	return lastErr
}

func validateRetryConfig(config RetryConfig) error {
	if config.MaxAttempts <= 0 {
		return fmt.Errorf("%w: max attempts must be greater than zero", ErrInvalidRetryConfig)
	}
	if config.InitialDelay < 0 {
		return fmt.Errorf("%w: initial delay cannot be negative", ErrInvalidRetryConfig)
	}
	if config.InitialDelay == 0 && config.MaxAttempts > 1 {
		return fmt.Errorf("%w: initial delay must be positive when max attempts > 1", ErrInvalidRetryConfig)
	}
	if config.MaxDelay < config.InitialDelay {
		return fmt.Errorf("%w: max delay cannot be less than initial delay", ErrInvalidRetryConfig)
	}
	if config.Jitter < 0 || config.Jitter > 1 {
		return fmt.Errorf("%w: jitter must be between zero and one", ErrInvalidRetryConfig)
	}
	return nil
}

func retryDelay(config RetryConfig, retryNumber int, random float64) time.Duration {
	delay := config.InitialDelay
	for i := 1; i < retryNumber && delay < config.MaxDelay; i++ {
		if delay > config.MaxDelay/2 {
			delay = config.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > config.MaxDelay {
		delay = config.MaxDelay
	}

	if config.Jitter == 0 || delay == 0 {
		return delay
	}

	factor := 1 - config.Jitter + (2 * config.Jitter * random)
	jittered := time.Duration(float64(delay) * factor)
	if jittered > config.MaxDelay {
		return config.MaxDelay
	}
	return jittered
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
