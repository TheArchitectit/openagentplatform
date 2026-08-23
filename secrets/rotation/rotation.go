// Package rotation provides automatic secret rotation scheduling and
// lifecycle management for the OAP secret subsystem.
package rotation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/secrets"
)

// RotationPolicy defines when and how a secret should be rotated.
type RotationPolicy struct {
	// Path is the secret path to rotate.
	Path string
	// Backend is the backend type that stores the secret.
	Backend string
	// MaxAge is the maximum age before rotation is forced. Zero means no age limit.
	MaxAge time.Duration
	// Interval is the fixed rotation interval. Zero means use MaxAge only.
	Interval time.Duration
	// Enabled toggles this policy on or off.
	Enabled bool
}

// RotationRecord tracks the history of rotations for a secret path.
type RotationRecord struct {
	Path      string
	LastRotated time.Time
	RotationCount int
	LastError    error
}

// RotationEvent is emitted after each rotation attempt.
type RotationEvent struct {
	Path      string
	Backend   string
	Success   bool
	Error     error
	Timestamp time.Time
	OldVersion int
	NewVersion int
}

// RotationHandler is called before and after rotation for a specific path.
type RotationHandler interface {
	// PreRotation is called before the rotation is attempted.
	// Return a non-nil error to abort the rotation.
	PreRotation(ctx context.Context, path string) error
	// PostRotation is called after the rotation completes (success or failure).
	PostRotation(ctx context.Context, path string, event RotationEvent)
}

// RotationScheduler periodically evaluates all registered policies and
// triggers rotations when conditions are met.
type RotationScheduler struct {
	mu       sync.RWMutex
	policies map[string]*RotationPolicy
	records  map[string]*RotationRecord
	handlers map[string][]RotationHandler
	backend  secrets.SecretBackend
	logger   *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// NewScheduler creates a new RotationScheduler bound to the given backend.
func NewScheduler(backend secrets.SecretBackend, logger *slog.Logger) *RotationScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RotationScheduler{
		policies: make(map[string]*RotationPolicy),
		records:  make(map[string]*RotationRecord),
		handlers: make(map[string][]RotationHandler),
		backend:  backend,
		logger:   logger,
	}
}

// RegisterPolicy adds or replaces a rotation policy for a secret path.
func (rs *RotationScheduler) RegisterPolicy(policy RotationPolicy) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.policies[policy.Path] = &policy
	if _, ok := rs.records[policy.Path]; !ok {
		rs.records[policy.Path] = &RotationRecord{
			Path:         policy.Path,
			LastRotated:  time.Now(),
			RotationCount: 0,
		}
	}
}

// UnregisterPolicy removes the policy for a secret path.
func (rs *RotationScheduler) UnregisterPolicy(path string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.policies, path)
	delete(rs.records, path)
	delete(rs.handlers, path)
}

// AddHandler attaches a lifecycle handler to a specific secret path.
func (rs *RotationScheduler) AddHandler(path string, handler RotationHandler) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.handlers[path] = append(rs.handlers[path], handler)
}

// Record returns the current rotation record for a path.
func (rs *RotationScheduler) Record(path string) *RotationRecord {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	r, ok := rs.records[path]
	if !ok {
		return nil
	}
	// Return a copy.
	cp := *r
	return &cp
}

// Policies returns a snapshot of all registered policies.
func (rs *RotationScheduler) Policies() []RotationPolicy {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	out := make([]RotationPolicy, 0, len(rs.policies))
	for _, p := range rs.policies {
		out = append(out, *p)
	}
	return out
}

// NeedsRotation checks if the given path needs rotation based on its policy.
func (rs *RotationScheduler) NeedsRotation(path string) bool {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.needsRotationLocked(path)
}

// RotateNow forces an immediate rotation for the given path, executing
// pre/post handlers. Returns the resulting RotationEvent.
func (rs *RotationScheduler) RotateNow(ctx context.Context, path string) RotationEvent {
	rs.mu.RLock()
	policy, hasPolicy := rs.policies[path]
	handlers := rs.handlers[path]
	record := rs.records[path]
	rs.mu.RUnlock()

	event := RotationEvent{
		Path:    path,
		Timestamp: time.Now(),
	}

	if !hasPolicy {
		event.Error = fmt.Errorf("no policy registered for %s", path)
		return event
	}
	event.Backend = policy.Backend

	// Get current version for tracking.
	if record != nil {
		// We don't know old version without backend access to Get, so track count.
	}

	// Run pre-rotation handlers.
	for _, h := range handlers {
		if err := h.PreRotation(ctx, path); err != nil {
			event.Error = fmt.Errorf("pre-rotation hook: %w", err)
			rs.emitPostHandlers(ctx, handlers, event)
			return event
		}
	}

	// Perform rotation via the backend.
	ver, err := rs.backend.Rotate(ctx, path, secrets.RotateOptions{})
	if err != nil {
		event.Error = fmt.Errorf("backend rotate: %w", err)
		event.Success = false
		rs.emitPostHandlers(ctx, handlers, event)
		return event
	}

	event.Success = true
	event.NewVersion = ver.Version

	// Update record.
	rs.mu.Lock()
	if record != nil {
		record.LastRotated = time.Now()
		record.RotationCount++
		record.LastError = nil
	}
	rs.mu.Unlock()

	rs.emitPostHandlers(ctx, handlers, event)
	return event
}

// emitPostHandlers calls PostRotation on all handlers for a path.
func (rs *RotationScheduler) emitPostHandlers(ctx context.Context, handlers []RotationHandler, event RotationEvent) {
	for _, h := range handlers {
		h.PostRotation(ctx, event.Path, event)
	}
}

// Start begins the background rotation loop that checks policies at the
// given interval. Call Stop to terminate.
func (rs *RotationScheduler) Start(ctx context.Context, checkInterval time.Duration) {
	ctx, cancel := context.WithCancel(ctx)
	rs.mu.Lock()
	rs.cancel = cancel
	rs.done = make(chan struct{})
	rs.mu.Unlock()

	go func() {
		defer close(rs.done)
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rs.checkAll(ctx)
			}
		}
	}()
}

// Stop signals the background loop to exit and waits for it.
func (rs *RotationScheduler) Stop() {
	rs.mu.RLock()
	cancel := rs.cancel
	done := rs.done
	rs.mu.RUnlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// checkAll evaluates all policies and rotates any that are due.
func (rs *RotationScheduler) checkAll(ctx context.Context) {
	// Collect paths that need rotation.
	var paths []string
	rs.mu.RLock()
	for path := range rs.policies {
		if rs.needsRotationLocked(path) {
			paths = append(paths, path)
		}
	}
	rs.mu.RUnlock()

	for _, path := range paths {
		event := rs.RotateNow(ctx, path)
		if event.Error != nil {
			rs.logger.Warn("rotation failed", "path", path, "error", event.Error)
		} else {
			rs.logger.Info("rotation succeeded", "path", path, "version", event.NewVersion)
		}
	}
}

// needsRotationLocked checks rotation needs while holding the read lock.
// Must be called with rs.mu.RLock held.
func (rs *RotationScheduler) needsRotationLocked(path string) bool {
	policy, ok := rs.policies[path]
	if !ok || !policy.Enabled {
		return false
	}
	record, ok := rs.records[path]
	if !ok {
		return true
	}
	now := time.Now()
	if policy.MaxAge > 0 && now.Sub(record.LastRotated) >= policy.MaxAge {
		return true
	}
	if policy.Interval > 0 && now.Sub(record.LastRotated) >= policy.Interval {
		return true
	}
	return false
}
