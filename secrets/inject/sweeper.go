package inject

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

// DefaultSweepInterval is how often the sweeper checks for expired injections.
const DefaultSweepInterval = 10 * time.Second

// trackedInjection is a single injection the sweeper monitors for TTL expiry.
type trackedInjection struct {
	spec     InjectionSpec
	injectedAt time.Time
	path     string
	envKey   string
	pipe     *oneShotPipe
}

// Sweeper periodically scans active injections and cleans up any whose TTL
// has elapsed. Expired file injections are securely deleted, env vars are
// purged, and dynamic-secret leases are revoked.
type Sweeper struct {
	mu      sync.Mutex
	active  map[string]*trackedInjection
	sweeps  time.Duration
	stop    chan struct{}
	done    chan struct{}
	logger  *slog.Logger
	audit   *audit.AuditService
	resolver *resolver.SecretResolver
}

// NewSweeper creates a Sweeper with the default 10-second interval.
func NewSweeper(logger *slog.Logger, auditSvc *audit.AuditService, r *resolver.SecretResolver) *Sweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{
		active:  make(map[string]*trackedInjection),
		sweeps:  DefaultSweepInterval,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		logger:  logger,
		audit:   auditSvc,
		resolver: r,
	}
}

// Track registers an injection for TTL-based cleanup.
func (sw *Sweeper) Track(spec InjectionSpec, path string) {
	if spec.TTL <= 0 {
		return
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()

	id := trackingID(spec, path)
	sw.active[id] = &trackedInjection{
		spec:       spec,
		injectedAt: time.Now(),
		path:       path,
		envKey:     path,
	}
}

// TrackStdin registers a stdin pipe injection for TTL-based cleanup.
func (sw *Sweeper) TrackStdin(spec InjectionSpec, pipe *oneShotPipe) {
	if spec.TTL <= 0 {
		return
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()

	id := trackingID(spec, pipe.path)
	sw.active[id] = &trackedInjection{
		spec:       spec,
		injectedAt: time.Now(),
		path:       pipe.path,
		envKey:     pipe.path,
		pipe:       pipe,
	}
}

// Start launches the background sweep loop. Call Stop to terminate.
func (sw *Sweeper) Start(ctx context.Context) {
	go sw.loop(ctx)
}

// Stop signals the sweep loop to exit and waits for it to finish.
func (sw *Sweeper) Stop() {
	select {
	case <-sw.stop:
		// already stopped
	default:
		close(sw.stop)
	}
	<-sw.done
}

// loop is the background ticker that scans for expired injections.
func (sw *Sweeper) loop(ctx context.Context) {
	defer close(sw.done)

	ticker := time.NewTicker(sw.sweeps)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sw.stop:
			return
		case <-ticker.C:
			sw.sweep(ctx)
		}
	}
}

// sweep scans all tracked injections and cleans up any that have expired.
func (sw *Sweeper) sweep(ctx context.Context) {
	now := time.Now()

	sw.mu.Lock()
	expired := make([]*trackedInjection, 0)
	for id, ti := range sw.active {
		if now.Sub(ti.injectedAt) >= ti.spec.TTL {
			expired = append(expired, ti)
			delete(sw.active, id)
		}
	}
	sw.mu.Unlock()

	for _, ti := range expired {
		sw.expire(ctx, ti)
	}
}

// expire handles cleanup of a single expired injection.
func (sw *Sweeper) expire(ctx context.Context, ti *trackedInjection) {
	spec := ti.spec

	switch spec.Method {
	case MethodEnv:
		if err := os.Unsetenv(ti.envKey); err != nil {
			sw.logger.Warn("sweeper: env unset failed", "key", ti.envKey, "err", err)
		}

	case MethodFile:
		if err := secureUnlink(ti.path); err != nil {
			sw.logger.Warn("sweeper: file unlink failed", "path", ti.path, "err", err)
		}

	case MethodStdin:
		if ti.pipe != nil {
			if ti.pipe.conn != nil {
				ti.pipe.conn.Close()
			}
			for i := range ti.pipe.data {
				ti.pipe.data[i] = 0
			}
			os.Remove(ti.pipe.path)
		}
	}

	// Revoke dynamic-secret leases.
	if spec.LeaseID != "" {
		sw.revokeLease(ctx, spec)
	}

	// Zero the in-memory value.
	for i := range spec.Value {
		spec.Value[i] = 0
	}

	sw.emitRevocation(ctx, spec)
}

// revokeLease revokes a dynamic-secret lease via the originating backend.
func (sw *Sweeper) revokeLease(ctx context.Context, spec InjectionSpec) {
	if sw.resolver == nil {
		return
	}
	be, ok := sw.resolver.BackendFor(spec.LeaseBackend)
	if !ok {
		sw.logger.Warn("sweeper: lease revoke — backend not found", "backend", spec.LeaseBackend)
		return
	}
	if err := be.RevokeLease(ctx, spec.LeaseID); err != nil {
		sw.logger.Warn("sweeper: lease revoke failed", "lease_id", spec.LeaseID, "err", err)
	}
}

// emitRevocation writes an audit event for the TTL-based revocation.
func (sw *Sweeper) emitRevocation(ctx context.Context, spec InjectionSpec) {
	if sw.audit == nil {
		return
	}
	details := map[string]any{
		"method":  string(spec.Method),
		"key":     spec.Key,
		"ttl":     spec.TTL.String(),
		"trigger": "ttl_expiry",
	}
	_, _ = sw.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorSystem,
		Action:       "secret.lease.revoke",
		ResourceType: "secret",
		ResourceID:   spec.URI,
		Details:      details,
		Outcome:      audit.OutcomeSuccess,
	})
}

// ActiveCount returns the number of injections currently being tracked.
// Useful for tests and health checks.
func (sw *Sweeper) ActiveCount() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return len(sw.active)
}

// trackingID generates a unique map key for a tracked injection.
func trackingID(spec InjectionSpec, path string) string {
	return fmt.Sprintf("%s|%s|%s", spec.Method, spec.Key, path)
}
