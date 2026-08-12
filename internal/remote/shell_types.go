package remote

import (
	"time"
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
