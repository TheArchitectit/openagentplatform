// Package remote provides server-side shell session management for
// remote endpoint access (SSH/WinRM proxy via NATS).
//
// A ShellSession is created when a user requests a remote shell. The
// session is associated with a target agent and bridges WebSocket
// frames from the user's browser to NATS subjects that the agent
// subscribes to. All data is base64-encoded on the wire so binary
// terminal escape sequences survive JSON encoding.
package remote

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// Protocol identifies the remote shell transport.
type Protocol string

const (
	ProtocolSSH  Protocol = "ssh"
	ProtocolWinRM Protocol = "winrm"
)

// SessionStatus is the lifecycle state of a shell session.
type SessionStatus string

const (
	StatusActive  SessionStatus = "active"
	StatusClosing SessionStatus = "closing"
	StatusClosed  SessionStatus = "closed"
)

// TerminalSize describes the user's terminal dimensions.
type TerminalSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Defaults applied when a session is created without explicit sizing.
const (
	defaultCols = 80
	defaultRows = 24
)

// Limits. These can be overridden in ShellManager config.
const (
	DefaultMaxSessionsPerUser = 10
	DefaultMaxSessionsPerAgent = 5
	DefaultIdleTimeout         = 30 * time.Minute
	DefaultInputRatePerSec     = 4096 // bytes/sec
)

// ShellSession is one user's live remote shell to one agent.
type ShellSession struct {
	ID           string        `json:"id"`
	AgentID      string        `json:"agent_id"`
	UserID       string        `json:"user_id"`
	Protocol     Protocol      `json:"protocol"`
	TerminalSize TerminalSize  `json:"terminal_size"`
	StartedAt    time.Time     `json:"started_at"`
	LastActivity time.Time     `json:"last_activity"`
	Status       SessionStatus `json:"status"`

	// Subjects derived from agent_id + session_id. Exposed for tests
	// and for the WebSocket bridge.
	StdinSubject  string `json:"-"`
	StdoutSubject string `json:"-"`
	ResizeSubject string `json:"-"`
	CloseSubject  string `json:"-"`
}

// StdinPayload is the wire format for keystrokes sent to the agent.
type StdinPayload struct {
	SessionID string `json:"session_id"`
	Data      string `json:"data"` // base64
}

// ResizePayload is sent when the terminal is resized.
type ResizePayload struct {
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// ClosePayload is sent to request the agent tear down its process.
type ClosePayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// ShutdownFn is a hook called when a session ends (clean or idle).
// Wired up by the WebSocket bridge so it can close the user's socket.
type ShutdownFn func(s *ShellSession, reason string)

// ShellManagerConfig tunes the limits enforced by the manager.
type ShellManagerConfig struct {
	MaxSessionsPerUser  int
	MaxSessionsPerAgent int
	IdleTimeout         time.Duration
	InputRatePerSec     int
}

// DefaultShellManagerConfig returns the documented defaults.
func DefaultShellManagerConfig() ShellManagerConfig {
	return ShellManagerConfig{
		MaxSessionsPerUser:  DefaultMaxSessionsPerUser,
		MaxSessionsPerAgent: DefaultMaxSessionsPerAgent,
		IdleTimeout:         DefaultIdleTimeout,
		InputRatePerSec:     DefaultInputRatePerSec,
	}
}

// Manager owns the active session table and the rate/idle reapers.
type ShellManager struct {
	cfg    ShellManagerConfig
	nc     *nats.Conn
	log    *slog.Logger
	onStop ShutdownFn

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
		cfg:     cfg,
		nc:      nil,
		log:     log,
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

// CreateSession records a new shell session, enforces limits, and
// returns it. It does not subscribe to NATS subjects; the WebSocket
// bridge does that.
func (m *ShellManager) CreateSession(agentID, userID string, proto Protocol, size TerminalSize) (*ShellSession, error) {
	if agentID == "" {
		return nil, errors.New("remote: agent_id required")
	}
	if userID == "" {
		return nil, errors.New("remote: user_id required")
	}
	if proto != ProtocolSSH && proto != ProtocolWinRM {
		return nil, fmt.Errorf("remote: unsupported protocol %q", proto)
	}
	if size.Cols <= 0 {
		size.Cols = defaultCols
	}
	if size.Rows <= 0 {
		size.Rows = defaultRows
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.byUser[userID] >= m.cfg.MaxSessionsPerUser {
		return nil, fmt.Errorf("remote: max sessions per user reached (%d)", m.cfg.MaxSessionsPerUser)
	}
	if m.byAgent[agentID] >= m.cfg.MaxSessionsPerAgent {
		return nil, fmt.Errorf("remote: max sessions per agent reached (%d)", m.cfg.MaxSessionsPerAgent)
	}

	id := uuid.NewString()
	s := &ShellSession{
		ID:           id,
		AgentID:      agentID,
		UserID:       userID,
		Protocol:     proto,
		TerminalSize: size,
		StartedAt:    time.Now().UTC(),
		LastActivity: time.Now().UTC(),
		Status:       StatusActive,
		StdinSubject:  ShellStdinSubject(agentID, id),
		StdoutSubject: ShellStdoutSubject(agentID, id),
		ResizeSubject: ShellResizeSubject(agentID, id),
		CloseSubject:  ShellCloseSubject(agentID, id),
	}
	m.sessions[id] = s
	m.byUser[userID]++
	m.byAgent[agentID]++

	m.rateMu.Lock()
	m.rlState[id] = newRateBucket(m.cfg.InputRatePerSec)
	m.rateMu.Unlock()

	m.log.Info("shell session created",
		"session_id", id,
		"agent_id", agentID,
		"user_id", userID,
		"protocol", string(proto),
	)
	return s, nil
}

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

// Kill terminates a session. If the session belongs to another user
// the caller must be admin. Reason is recorded in the audit log and
// forwarded to the agent.
func (m *ShellManager) Kill(id, requesterID string, isAdmin bool, reason string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	if !isAdmin && s.UserID != requesterID {
		m.mu.Unlock()
		return ErrSessionForbidden
	}
	delete(m.sessions, id)
	m.byUser[s.UserID]--
	if m.byUser[s.UserID] <= 0 {
		delete(m.byUser, s.UserID)
	}
	m.byAgent[s.AgentID]--
	if m.byAgent[s.AgentID] <= 0 {
		delete(m.byAgent, s.AgentID)
	}
	s.Status = StatusClosed
	m.mu.Unlock()

	m.rateMu.Lock()
	delete(m.rlState, id)
	m.rateMu.Unlock()

	// Tell the agent to tear down. We use a best-effort publish: if
	// NATS is down the session is already gone from our table and
	// the agent will see EOF on its own.
	if m.nc != nil {
		payload, _ := json.Marshal(ClosePayload{SessionID: id, Reason: reason})
		if err := m.nc.Publish(s.CloseSubject, payload); err != nil {
			m.log.Warn("shell: publish close failed", "subject", s.CloseSubject, "err", err)
		}
	}

	if m.onStop != nil {
		m.onStop(s, reason)
	}
	m.log.Info("shell session killed", "session_id", id, "reason", reason)
	return nil
}

// CloseByAgent marks a session closed when the agent signals EOF
// (e.g. SSH process exited). It is idempotent.
func (m *ShellManager) CloseByAgent(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, id)
	m.byUser[s.UserID]--
	if m.byUser[s.UserID] <= 0 {
		delete(m.byUser, s.UserID)
	}
	m.byAgent[s.AgentID]--
	if m.byAgent[s.AgentID] <= 0 {
		delete(m.byAgent, s.AgentID)
	}
	s.Status = StatusClosed
	m.mu.Unlock()

	m.rateMu.Lock()
	delete(m.rlState, id)
	m.rateMu.Unlock()

	if m.onStop != nil {
		m.onStop(s, "agent_eof")
	}
}

// PublishStdin ships user keystrokes to the agent. Returns false if
// the rate limit was exceeded.
func (m *ShellManager) PublishStdin(ctx context.Context, id string, data []byte) (bool, error) {
	s := m.Get(id)
	if s == nil {
		return false, ErrSessionNotFound
	}
	if !m.AllowInput(id, len(data)) {
		return false, nil
	}
	if m.nc == nil {
		return true, nil
	}
	payload, _ := json.Marshal(StdinPayload{SessionID: id, Data: base64.StdEncoding.EncodeToString(data)})
	if err := m.nc.Publish(s.StdinSubject, payload); err != nil {
		return true, fmt.Errorf("nats publish stdin: %w", err)
	}
	m.Touch(id)
	return true, nil
}

// PublishResize ships a terminal-resize event to the agent.
func (m *ShellManager) PublishResize(ctx context.Context, id string, cols, rows int) error {
	s := m.Get(id)
	if s == nil {
		return ErrSessionNotFound
	}
	m.mu.Lock()
	s.TerminalSize = TerminalSize{Cols: cols, Rows: rows}
	s.LastActivity = time.Now().UTC()
	m.mu.Unlock()
	if m.nc == nil {
		return nil
	}
	payload, _ := json.Marshal(ResizePayload{SessionID: id, Cols: cols, Rows: rows})
	if err := m.nc.Publish(s.ResizeSubject, payload); err != nil {
		return fmt.Errorf("nats publish resize: %w", err)
	}
	return nil
}

// Run starts the idle reaper. It blocks until Stop() is called.
func (m *ShellManager) Run(ctx context.Context) {
	tick := time.NewTicker(1 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stop:
			return
		case <-tick.C:
			m.reapIdle()
		}
	}
}

// Stop signals Run to exit and closes all sessions.
func (m *ShellManager) Stop() {
	m.stopped.Do(func() { close(m.stop) })
	m.mu.Lock()
	for id, s := range m.sessions {
		delete(m.sessions, id)
		m.byUser[s.UserID]--
		m.byAgent[s.AgentID]--
		s.Status = StatusClosed
		_ = id
	}
	m.mu.Unlock()
}

// reapIdle closes sessions that have been inactive past IdleTimeout.
func (m *ShellManager) reapIdle() {
	cutoff := time.Now().Add(-m.cfg.IdleTimeout)
	var stale []string
	m.mu.RLock()
	for id, s := range m.sessions {
		if s.LastActivity.Before(cutoff) {
			stale = append(stale, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range stale {
		if err := m.Kill(id, "", true, "idle_timeout"); err != nil && !errors.Is(err, ErrSessionNotFound) {
			m.log.Warn("shell: idle reap failed", "session_id", id, "err", err)
		}
	}
}

// Errors returned by the manager.
var (
	ErrSessionNotFound  = errors.New("remote: session not found")
	ErrSessionForbidden = errors.New("remote: session belongs to another user")
)

// --- Subject builders -------------------------------------------------

// ShellStdinSubject returns the NATS subject the agent listens on for
// user keystrokes. agentID and sessionID are taken as-is (no
// escaping); the API is responsible for validating them upstream.
func ShellStdinSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.stdin", agentID, sessionID)
}

// ShellStdoutSubject is the subject the agent publishes terminal
// output on. The server subscribes and forwards frames to the user.
func ShellStdoutSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.stdout", agentID, sessionID)
}

// ShellResizeSubject carries terminal-resize events.
func ShellResizeSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.resize", agentID, sessionID)
}

// ShellCloseSubject carries close requests in either direction.
func ShellCloseSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.close", agentID, sessionID)
}

// RandomID returns a hex-encoded random ID for use as a one-time
// credential token. It is exposed here so tests don't need to import
// crypto/rand.
func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// rand.Read on linux only fails if the pool is broken; fall
		// back to a UUID-derived value so callers always get something
		// usable even in that unlikely case.
		return uuid.NewString()
	}
	return hex.EncodeToString(b)
}
