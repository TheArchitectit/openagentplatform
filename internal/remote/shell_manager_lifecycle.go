package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CreateSession records a new shell session, enforces limits, and
// returns it. It publishes the StartRequest to the agent so the agent
// spawns its side of the session; without this publish the browser's
// WebSocket connects but no process ever runs.
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
		ID:            id,
		AgentID:       agentID,
		UserID:        userID,
		Protocol:      proto,
		TerminalSize:  size,
		StartedAt:     time.Now().UTC(),
		LastActivity:  time.Now().UTC(),
		Status:        StatusActive,
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

	// Tell the agent to spawn the remote process. Best-effort: if NATS
	// is down the session is still created; the WS bridge will surface
	// the lack of output and the idle reaper will collect it.
	if m.nc != nil {
		start := shellStartRequest{
			SessionID: s.ID,
			UserID:    userID,
			Protocol:  string(proto),
			Cols:      size.Cols,
			Rows:      size.Rows,
		}
		if m.resolveHost != nil {
			if host := m.resolveHost(agentID); host != "" {
				// The agent-side StartRequest carries the dial target in
				// Username@host via Command; send "user@host" when a
				// credential username is resolvable, else just the host.
				start.Command = host
			}
		}
		payload, _ := json.Marshal(start)
		if err := m.nc.Publish(shellStartSubject(agentID), payload); err != nil {
			m.log.Warn("shell: publish start failed", "session_id", id, "err", err)
		}
	}

	m.log.Info("shell session created",
		"session_id", id,
		"agent_id", agentID,
		"user_id", userID,
		"protocol", string(proto),
	)
	return s, nil
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
