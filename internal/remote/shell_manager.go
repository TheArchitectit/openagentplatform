package remote

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// HostResolver returns the dial target (hostname or IP) for an agent.
// Wired from cmd/server against the agent inventory so the start
// request carries a real SSH host rather than the session-ID
// placeholder the agent's default command builder previously received.
type HostResolver func(agentID string) string

// ShellManager owns the live session table, admission limits, and the
// per-session input-rate reaper. Session lifecycle (create/kill/reap)
// and wire I/O (stdin/resize publish) live in
// shell_manager_lifecycle.go and shell_manager_io.go respectively.
type ShellManager struct {
	cfg    ShellManagerConfig
	nc     *nats.Conn
	log    *slog.Logger
	onStop ShutdownFn
	// resolveHost returns the SSH dial target for an agent. May be nil;
	// the start payload then carries an empty host and the agent falls
	// back to its local convention.
	resolveHost HostResolver

	mu       sync.RWMutex
	sessions map[string]*ShellSession
	// per-user and per-agent counts are derived from the sessions map
	// but cached for cheap admission control. Both are guarded by mu.
	byUser  map[string]int
	byAgent map[string]int

	// per-session rate-limiter state.
	rateMu  sync.Mutex
	rlState map[string]*rateBucket

	stop    chan struct{}
	stopped sync.Once
}

// rateBucket tracks an input-rate sliding window per session.
type rateBucket struct {
	mu       sync.Mutex
	window   []byte
	resetAt  time.Time
	bytesIn  int
	limitBps int
}

func newRateBucket(limitBps int) *rateBucket {
	return &rateBucket{
		window:   make([]byte, 0, limitBps),
		resetAt:  time.Now().Add(time.Second),
		limitBps: limitBps,
	}
}

// allow reports whether n more bytes can be added within the current
// one-second window.
func (r *rateBucket) allow(n int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if now.After(r.resetAt) {
		r.bytesIn = 0
		r.resetAt = now.Add(time.Second)
	}
	if r.bytesIn+n > r.limitBps {
		return false
	}
	r.bytesIn += n
	return true
}

// NATSPublisher is the subset of *nats.Conn the manager needs.
type NATSPublisher interface {
	Publish(subj string, data []byte) error
	Subscribe(subj string, cb nats.MsgHandler) (*nats.Subscription, error)
}

// NewShellManager constructs a manager. natsConn may be nil for tests;
// in that case CreateSession still works but Start() must be skipped
// (callers using nil should not call Run).
func NewShellManager(cfg ShellManagerConfig, natsConn NATSPublisher, log *slog.Logger) *ShellManager {
	if log == nil {
		log = slog.Default()
	}
	if cfg.MaxSessionsPerUser <= 0 {
		cfg.MaxSessionsPerUser = DefaultMaxSessionsPerUser
	}
	if cfg.MaxSessionsPerAgent <= 0 {
		cfg.MaxSessionsPerAgent = DefaultMaxSessionsPerAgent
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.InputRatePerSec <= 0 {
		cfg.InputRatePerSec = DefaultInputRatePerSec
	}
	m := &ShellManager{
		cfg:      cfg,
		nc:       nil,
		log:      log,
		sessions: make(map[string]*ShellSession),
		byUser:   make(map[string]int),
		byAgent:  make(map[string]int),
		rlState:  make(map[string]*rateBucket),
		stop:     make(chan struct{}),
	}
	if c, ok := natsConn.(*nats.Conn); ok {
		m.nc = c
	}
	return m
}

// SetShutdownHook registers a callback fired when a session is
// forcibly closed (idle or admin-killed). The bridge uses this to
// tear down the user's WebSocket.
func (m *ShellManager) SetShutdownHook(fn ShutdownFn) { m.onStop = fn }

// SetHostResolver wires the agent-to-host lookup used when building
// start requests.
func (m *ShellManager) SetHostResolver(r HostResolver) { m.resolveHost = r }

// Get returns a snapshot of the session, or nil if not found.
func (m *ShellManager) Get(id string) *ShellSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// List returns sessions visible to the caller. Admin callers see all
// sessions; non-admin callers see only their own.
func (m *ShellManager) List(userID string, admin bool) []*ShellSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ShellSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		if admin || s.UserID == userID {
			out = append(out, s)
		}
	}
	return out
}

// Touch records activity for the session (used to extend the idle
// window and update LastActivity for status queries).
func (m *ShellManager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.LastActivity = time.Now().UTC()
	}
}

// AllowInput enforces the per-session input rate limit.
func (m *ShellManager) AllowInput(id string, n int) bool {
	m.rateMu.Lock()
	rb, ok := m.rlState[id]
	m.rateMu.Unlock()
	if !ok {
		return true
	}
	return rb.allow(n)
}

// Errors returned by the manager.
var (
	ErrSessionNotFound  = errors.New("remote: session not found")
	ErrSessionForbidden = errors.New("remote: session belongs to another user")
)
