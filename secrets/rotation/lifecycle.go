package rotation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// LifecyclePhase identifies the phase of a rotation lifecycle event.
type LifecyclePhase string

const (
	PhasePreValidation  LifecyclePhase = "pre_validation"
	PhasePreRotation    LifecyclePhase = "pre_rotation"
	PhaseRotation       LifecyclePhase = "rotation"
	PhasePostRotation   LifecyclePhase = "post_rotation"
	PhasePostValidation LifecyclePhase = "post_validation"
)

// LifecycleEvent is emitted during lifecycle phase transitions.
type LifecycleEvent struct {
	Path      string
	Phase     LifecyclePhase
	Timestamp time.Time
	Error     error
	Details   map[string]any
}

// LifecycleCallback is a function that handles a lifecycle event.
type LifecycleCallback func(ctx context.Context, event LifecycleEvent) error

// LifecycleManager orchestrates pre/post rotation hooks with ordering
// guarantees and error handling.
type LifecycleManager struct {
	mu        sync.RWMutex
	callbacks map[LifecyclePhase][]lifecycleEntry
	logger    *slog.Logger
}

type lifecycleEntry struct {
	path     string // empty means global
	priority int    // lower = runs first
	callback LifecycleCallback
}

// NewLifecycleManager creates a new LifecycleManager.
func NewLifecycleManager(logger *slog.Logger) *LifecycleManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &LifecycleManager{
		callbacks: make(map[LifecyclePhase][]lifecycleEntry),
		logger:    logger,
	}
}

// Register adds a callback for a specific phase and path.
// Priority controls execution order (lower runs first).
func (lm *LifecycleManager) Register(phase LifecyclePhase, path string, priority int, cb LifecycleCallback) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.callbacks[phase] = append(lm.callbacks[phase], lifecycleEntry{
		path:     path,
		priority: priority,
		callback: cb,
	})

	// Sort by priority for this phase.
	entries := lm.callbacks[phase]
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].priority < entries[j-1].priority; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// Unregister removes all callbacks for a phase and path combination.
func (lm *LifecycleManager) Unregister(phase LifecyclePhase, path string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entries := lm.callbacks[phase]
	filtered := entries[:0]
	for _, e := range entries {
		if e.path != path {
			filtered = append(filtered, e)
		}
	}
	lm.callbacks[phase] = filtered
}

// Run executes all callbacks for a given phase and path.
// Global callbacks (empty path) always run. Path-specific callbacks run
// if they match. Returns the first error encountered.
func (lm *LifecycleManager) Run(ctx context.Context, phase LifecyclePhase, path string, details map[string]any) error {
	lm.mu.RLock()
	entries := make([]lifecycleEntry, len(lm.callbacks[phase]))
	copy(entries, lm.callbacks[phase])
	lm.mu.RUnlock()

	event := LifecycleEvent{
		Path:      path,
		Phase:     phase,
		Timestamp: time.Now(),
		Details:   details,
	}

	for _, entry := range entries {
		// Run if entry is global (empty path) or matches the specific path.
		if entry.path != "" && entry.path != path {
			continue
		}

		if err := entry.callback(ctx, event); err != nil {
			return fmt.Errorf("lifecycle %s for %s: %w", phase, path, err)
		}
	}
	return nil
}

// RunChain executes the full lifecycle chain: pre_validation -> pre_rotation
// -> rotation -> post_rotation -> post_validation.
// Stops at the first error and returns it.
func (lm *LifecycleManager) RunChain(ctx context.Context, path string, rotationFn func() error) error {
	phases := []LifecyclePhase{
		PhasePreValidation,
		PhasePreRotation,
		PhaseRotation,
		PhasePostRotation,
		PhasePostValidation,
	}

	for _, phase := range phases {
		if phase == PhaseRotation {
			if err := rotationFn(); err != nil {
				// Run post hooks with error context.
				lm.runPostPhase(ctx, path, PhasePostRotation, err)
				lm.runPostPhase(ctx, path, PhasePostValidation, err)
				return err
			}
		} else {
			if err := lm.Run(ctx, phase, path, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// runPostPhase runs callbacks for a phase with an error event.
func (lm *LifecycleManager) runPostPhase(ctx context.Context, path string, phase LifecyclePhase, rotErr error) {
	lm.mu.RLock()
	entries := make([]lifecycleEntry, len(lm.callbacks[phase]))
	copy(entries, lm.callbacks[phase])
	lm.mu.RUnlock()

	event := LifecycleEvent{
		Path:      path,
		Phase:     phase,
		Timestamp: time.Now(),
		Error:     rotErr,
	}

	for _, entry := range entries {
		if entry.path != "" && entry.path != path {
			continue
		}
		if err := entry.callback(ctx, event); err != nil {
			lm.logger.Warn("lifecycle post-hook failed",
				"phase", phase, "path", path, "error", err)
		}
	}
}

// CallbackCount returns the number of registered callbacks for a phase.
func (lm *LifecycleManager) CallbackCount(phase LifecyclePhase) int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return len(lm.callbacks[phase])
}

// Clear removes all registered callbacks.
func (lm *LifecycleManager) Clear() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.callbacks = make(map[LifecyclePhase][]lifecycleEntry)
}
