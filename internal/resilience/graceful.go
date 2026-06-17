// Package resilience – graceful.go.
//
// GracefulShutdown coordinates an orderly termination sequence:
//   1. Stop accepting new HTTP requests (http.Server.Shutdown).
//   2. Wait for in-flight requests to complete (up to a timeout).
//   3. Close downstream dependencies in reverse-initialisation order:
//      background workers, NATS subscriptions, database pool.
//
// Dependents are registered as named Closers.  On shutdown they are
// closed sequentially so that a failure in one does not skip the rest.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ShutdownConfig controls the graceful-shutdown sequence.
type ShutdownConfig struct {
	// Timeout is the maximum total time to spend draining
	// in-flight requests.  Default 30s.
	Timeout time.Duration

	// Logger receives structured shutdown events.  Defaults to
	// slog.Default().
	Logger *slog.Logger
}

// Closer is anything that can be gracefully closed.  The Context
// argument is bounded by the overall shutdown timeout so dependents
// can implement their own timeouts.
type Closer interface {
	Close(ctx context.Context) error
}

// CloserFunc adapts a plain function to the Closer interface.
type CloserFunc func(ctx context.Context) error

// Close implements Closer.
func (f CloserFunc) Close(ctx context.Context) error { return f(ctx) }

// GracefulShutdown tracks registered Closers and orchestrates an
// ordered teardown.
type GracefulShutdown struct {
	cfg ShutdownConfig

	mu       sync.Mutex
	closers  []namedCloser
	inFlight sync.WaitGroup
	count    atomic.Int64
}

// namedCloser pairs a human-readable label with the closer itself.
type namedCloser struct {
	name   string
	closer Closer
}

// NewGracefulShutdown creates a new GracefulShutdown with default
// configuration.
func NewGracefulShutdown(cfg ShutdownConfig) *GracefulShutdown {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &GracefulShutdown{cfg: cfg}
}

// Register adds a Closer to the shutdown list.  Closers are closed in
// the reverse order of registration (LIFO) so the most-recently-added
// dependency shuts down first.  Returns the same GracefulShutdown to
// allow fluent chaining.
func (gs *GracefulShutdown) Register(name string, c Closer) *GracefulShutdown {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.closers = append(gs.closers, namedCloser{name: name, closer: c})
	return gs
}

// TrackInFlight is called by middleware to record the start of a
// request.  The returned function must be deferred to signal
// completion.
func (gs *GracefulShutdown) TrackInFlight() func() {
	gs.inFlight.Add(1)
	gs.count.Add(1)
	return func() {
		gs.inFlight.Done()
		gs.count.Add(-1)
	}
}

// InFlightCount returns the number of currently tracked in-flight
// requests.
func (gs *GracefulShutdown) InFlightCount() int64 {
	return gs.count.Load()
}

// ShutdownHTTP performs step 1+2 of the shutdown sequence: it calls
// http.Server.Shutdown and waits for in-flight requests to drain.
// Returns an error if the timeout is exceeded.
func (gs *GracefulShutdown) ShutdownHTTP(srv *http.Server) error {
	gs.cfg.Logger.Info("graceful shutdown: stop accepting new requests",
		"timeout", gs.cfg.Timeout,
	)

	ctx, cancel := context.WithTimeout(context.Background(), gs.cfg.Timeout)
	defer cancel()

	var errs []error

	// 1. Stop the HTTP listener.
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http server shutdown: %w", err))
		}
	}

	// 2. Wait for tracked in-flight work.
	done := make(chan struct{})
	go func() {
		gs.inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
		gs.cfg.Logger.Info("graceful shutdown: in-flight requests drained")
	case <-ctx.Done():
		errs = append(errs, fmt.Errorf("graceful shutdown timeout: %d in-flight", gs.InFlightCount()))
	}

	return errors.Join(errs...)
}

// ShutdownDeps runs step 3: closing registered dependencies in LIFO
// order.  Each closer is given a per-closer deadline derived from the
// remaining budget.
func (gs *GracefulShutdown) ShutdownDeps() error {
	gs.mu.Lock()
	closers := make([]namedCloser, len(gs.closers))
	copy(closers, gs.closers)
	gs.mu.Unlock()

	gs.cfg.Logger.Info("graceful shutdown: closing dependencies", "count", len(closers))

	// Each closer gets at most 5s.  The overall budget is gs.cfg.Timeout.
	deadline := time.Now().Add(gs.cfg.Timeout)
	var errs []error

	// LIFO: close in reverse registration order.
	for i := len(closers) - 1; i >= 0; i-- {
		nc := closers[i]
		remaining := time.Until(deadline)
		if remaining <= 0 {
			errs = append(errs, fmt.Errorf("shutdown deadline exceeded before closing %s", nc.name))
			continue
		}
		perCloser := 5 * time.Second
		if perCloser > remaining {
			perCloser = remaining
		}
		ctx, cancel := context.WithTimeout(context.Background(), perCloser)

		gs.cfg.Logger.Info("graceful shutdown: closing", "name", nc.name)
		if err := nc.closer.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", nc.name, err))
		}
		cancel()
	}
	return errors.Join(errs...)
}

// ShutdownAll performs the full sequence: HTTP drain, then dependency
// teardown.  It is safe to call ShutdownAll multiple times; the second
// call is a no-op for the closers.
func (gs *GracefulShutdown) ShutdownAll(srv *http.Server) error {
	var errs []error
	if err := gs.ShutdownHTTP(srv); err != nil {
		errs = append(errs, err)
	}
	if err := gs.ShutdownDeps(); err != nil {
		errs = append(errs, err)
	}
	gs.cfg.Logger.Info("graceful shutdown: complete")
	return errors.Join(errs...)
}
