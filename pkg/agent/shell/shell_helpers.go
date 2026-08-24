// Package shell implements the agent-side handler for remote shell
// sessions. The server publishes keystrokes to per-session NATS
// subjects; this package subscribes, launches the requested
// protocol (ssh or winrm), pipes I/O, and forwards output back.
package shell

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// StartRequestSubject is the subject the server publishes to when a
// new shell session is requested.
func StartRequestSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.start", agentID)
}

// Protocol matches the server-side value.
type Protocol string

const (
	ProtocolSSH   Protocol = "ssh"
	ProtocolWinRM Protocol = "winrm"
)

// Defaults. MaxConcurrentShells caps the number of in-flight
// processes the agent will run. IdleTimeout is the inactivity
// period after which the agent self-terminates a session.
const (
	DefaultMaxConcurrentShells = 5
	DefaultIdleTimeout         = 30 * time.Minute
)

// NATSPublisher is the subset of *nats.Conn we need.
type NATSPublisher interface {
	Subscribe(subj string, cb nats.MsgHandler) (*nats.Subscription, error)
	Publish(subj string, data []byte) error
	QueueSubscribe(subj, queue string, cb nats.MsgHandler) (*nats.Subscription, error)
}

// StdinPayload mirrors the server-side shape.
type StdinPayload struct {
	SessionID string `json:"session_id"`
	Data      string `json:"data"`
}

// ResizePayload mirrors the server-side shape.
type ResizePayload struct {
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// ClosePayload mirrors the server-side shape.
type ClosePayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// StartRequest announces a new shell session. The server
// publishes this; the agent responds by spawning the process.
type StartRequest struct {
	SessionID string   `json:"session_id"`
	UserID    string   `json:"user_id"`
	Protocol  Protocol `json:"protocol"`
	Cols      int      `json:"cols"`
	Rows      int      `json:"rows"`
	Username  string   `json:"username,omitempty"`
	Command   string   `json:"command,omitempty"`
}

// HandlerConfig configures the agent-side shell handler.
type HandlerConfig struct {
	AgentID             string
	MaxConcurrentShells int
	IdleTimeout         time.Duration
	// CommandBuilder returns the exec.Cmd to run for a given
	// protocol + session start request. Defaults to a sensible ssh
	// / winrm invocation. Tests can override.
	CommandBuilder func(req StartRequest) (*exec.Cmd, error)
}

// Handler is the long-lived shell session manager on the agent.
type Handler struct {
	cfg HandlerConfig
	nc  NATSPublisher
	log *slog.Logger

	mu       sync.Mutex
	sessions map[string]*sessionRun
	sem      chan struct{}

	startSub *nats.Subscription
	closeSub *nats.Subscription
	stopOnce sync.Once
	stopped  chan struct{}
}

// sessionRun tracks a single in-flight shell process.
type sessionRun struct {
	id        string
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	created   time.Time
	stdinSub  *nats.Subscription
	resizeSub *nats.Subscription
}

// NewHandler builds a Handler. nc may be nil; in that case Run
// returns an error and the handler is unusable.
func NewHandler(cfg HandlerConfig, nc NATSPublisher, log *slog.Logger) *Handler {
	if cfg.MaxConcurrentShells <= 0 {
		cfg.MaxConcurrentShells = DefaultMaxConcurrentShells
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.CommandBuilder == nil {
		cfg.CommandBuilder = defaultCommandBuilder
	}
	return &Handler{
		cfg:      cfg,
		nc:       nc,
		log:      log,
		sessions: make(map[string]*sessionRun),
		sem:      make(chan struct{}, cfg.MaxConcurrentShells),
		stopped:  make(chan struct{}),
	}
}

// Subject builders mirror the server's naming.
func StdinSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.stdin", agentID, sessionID)
}

func StdoutSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.stdout", agentID, sessionID)
}

func ResizeSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.resize", agentID, sessionID)
}

func CloseSubject(agentID, sessionID string) string {
	return fmt.Sprintf("oap.agents.%s.shell.%s.close", agentID, sessionID)
}

// defaultCommandBuilder returns the exec.Cmd for a given protocol.
// SSH is invoked as `ssh -tt -o BatchMode=yes user@host`; the dial
// target comes from req.Command (the server resolves it from agent
// inventory). When Command is empty we fall back to localhost — the
// agent runs on the target host, so a local shell is the sane default.
func defaultCommandBuilder(req StartRequest) (*exec.Cmd, error) {
	switch req.Protocol {
	case ProtocolSSH:
		user := req.Username
		if user == "" {
			user = "oap"
		}
		host := req.Command
		if host == "" {
			host = "localhost"
		}
		args := []string{"-tt", "-o", "BatchMode=yes"}
		if req.Cols > 0 && req.Rows > 0 {
			args = append(args, "-o", "RequestTTY=yes")
		}
		args = append(args, user+"@"+host)
		return exec.Command("ssh", args...), nil
	case ProtocolWinRM:
		// On Windows hosts we shell out to powershell + winrs; on
		// other OSes we just exec powershell as a no-op stub. The
		// real implementation will use a WinRM library once
		// credentials are available.
		return exec.Command("powershell", "-NoProfile", "-Command", "Read-Host"), nil
	default:
		return nil, fmt.Errorf("shell: unsupported protocol %q", req.Protocol)
	}
}

// SessionCount returns the number of in-flight sessions. Useful for
// /healthz or operator dashboards.
func (h *Handler) SessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// NewSessionID returns a fresh session ID. Exposed for tests.
func NewSessionID() string { return uuid.NewString() }

// killSession force-kills an in-flight process. Idempotent.
func (h *Handler) killSession(id, reason string) {
	h.mu.Lock()
	s, ok := h.sessions[id]
	if ok {
		delete(h.sessions, id)
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	if s.stdinSub != nil {
		_ = s.stdinSub.Unsubscribe()
	}
	if s.resizeSub != nil {
		_ = s.resizeSub.Unsubscribe()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	h.log.Info("shell: session killed", "session_id", id, "reason", reason)
}

// handleResize updates the terminal size for the session. On
// Unix we send SIGWINCH. On Windows the resize is best-effort
// (we don't own the console).
func (h *Handler) handleResize(s *sessionRun, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	// We don't currently pass a PTY between server and agent; the
	// resize request is recorded for the session log and forwarded
	// to the child process via SIGWINCH on Unix. Wire this up if
	// the agent is running the shell directly (i.e. not via
	// ssh/winrm). For now we just log.
	h.log.Debug("shell: resize",
		"session_id", s.id,
		"cols", cols,
		"rows", rows,
	)
}

// pumpStream reads line-by-line from r and publishes each line
// (base64) to the server's stdout subject. The server forwards
// to the WebSocket.
func (h *Handler) pumpStream(sessionID, stream string, r io.ReadCloser) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		encoded := base64.StdEncoding.EncodeToString(scanner.Bytes())
		payload, _ := json.Marshal(StdinPayload{
			SessionID: sessionID,
			Data:      encoded,
		})
		if err := h.nc.Publish(StdoutSubject(h.cfg.AgentID, sessionID), payload); err != nil {
			h.log.Warn("shell: publish output failed", "err", err)
		}
	}
}

// publishClose tells the server the session is gone.
func (h *Handler) publishClose(sessionID, reason string) {
	payload, _ := json.Marshal(ClosePayload{SessionID: sessionID, Reason: reason})
	if err := h.nc.Publish(CloseSubject(h.cfg.AgentID, sessionID), payload); err != nil {
		h.log.Debug("shell: publish close failed", "err", err)
	}
}
