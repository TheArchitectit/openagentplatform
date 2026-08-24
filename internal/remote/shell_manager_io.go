package remote

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

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

// shellStartSubject mirrors pkg/agent/shell.StartRequestSubject. The
// agent package cannot be imported here (it would drag the agent-side
// exec code into the server binary), so the subject shape is pinned by
// a test in this package instead.
func shellStartSubject(agentID string) string {
	return "oap.agents." + agentID + ".shell.start"
}

// shellStartRequest mirrors the JSON shape of
// pkg/agent/shell.StartRequest (session_id, user_id, protocol, cols,
// rows, optional username/command).
type shellStartRequest struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Protocol  string `json:"protocol"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	// Command carries the resolved SSH dial target (hostname or IP)
	// from agent inventory; empty lets the agent use its local default.
	Command string `json:"command,omitempty"`
}

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
