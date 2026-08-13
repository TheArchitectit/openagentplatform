// Package shell implements the agent-side handler for remote shell
// sessions. The server publishes keystrokes to per-session NATS
// subjects; this package subscribes, launches the requested
// protocol (ssh or winrm), pipes I/O, and forwards output back.
package shell

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Run subscribes to the start + close subjects and blocks until ctx
// is cancelled. Returns the start subscription (callers should
// Unsubscribe on shutdown).
func (h *Handler) Run(ctx context.Context) error {
	if h.nc == nil {
		return errors.New("shell: nats not configured")
	}

	startSub, err := h.nc.Subscribe(StartRequestSubject(h.cfg.AgentID), func(m *nats.Msg) {
		var req StartRequest
		if err := json.Unmarshal(m.Data, &req); err != nil {
			h.log.Warn("shell: bad start payload", "err", err)
			return
		}
		if req.SessionID == "" {
			h.log.Warn("shell: start without session_id")
			return
		}
		h.handleStart(ctx, req)
	})
	if err != nil {
		return fmt.Errorf("shell: subscribe start: %w", err)
	}
	h.startSub = startSub

	closeSub, err := h.nc.Subscribe(CloseSubject(h.cfg.AgentID, "*"), func(m *nats.Msg) {
		var p ClosePayload
		if err := json.Unmarshal(m.Data, &p); err != nil {
			return
		}
		h.killSession(p.SessionID, p.Reason)
	})
	if err != nil {
		_ = startSub.Unsubscribe()
		return fmt.Errorf("shell: subscribe close: %w", err)
	}
	h.closeSub = closeSub

	h.log.Info("shell handler running",
		"agent_id", h.cfg.AgentID,
		"start_subject", StartRequestSubject(h.cfg.AgentID),
		"max_concurrent", h.cfg.MaxConcurrentShells,
	)

	<-ctx.Done()
	h.stop()
	return nil
}

// stop is idempotent and tears down all in-flight sessions.
func (h *Handler) stop() {
	h.stopOnce.Do(func() {
		close(h.stopped)
		if h.startSub != nil {
			_ = h.startSub.Unsubscribe()
		}
		if h.closeSub != nil {
			_ = h.closeSub.Unsubscribe()
		}
		h.mu.Lock()
		for id, s := range h.sessions {
			if s.stdinSub != nil {
				_ = s.stdinSub.Unsubscribe()
			}
			if s.resizeSub != nil {
				_ = s.resizeSub.Unsubscribe()
			}
			s.cancel()
			delete(h.sessions, id)
		}
		h.mu.Unlock()
	})
}

// handleStart launches the requested protocol for a session. It
// publishes stdout/stderr frames back to the server.
func (h *Handler) handleStart(parent context.Context, req StartRequest) {
	select {
	case h.sem <- struct{}{}:
		// acquired slot
	default:
		h.log.Warn("shell: max concurrent reached, rejecting",
			"session_id", req.SessionID,
		)
		h.publishClose(req.SessionID, "agent_busy")
		return
	}

	_, cancel := context.WithTimeout(parent, h.cfg.IdleTimeout+time.Hour)
	cmd, err := h.cfg.CommandBuilder(req)
	if err != nil {
		cancel()
		<-h.sem
		h.log.Warn("shell: command builder failed", "err", err)
		h.publishClose(req.SessionID, "command_build_failed")
		return
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		<-h.sem
		h.log.Warn("shell: stdin pipe failed", "err", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		<-h.sem
		h.log.Warn("shell: stdout pipe failed", "err", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		<-h.sem
		h.log.Warn("shell: stderr pipe failed", "err", err)
		return
	}

	if err := cmd.Start(); err != nil {
		cancel()
		<-h.sem
		h.log.Warn("shell: process start failed", "err", err)
		h.publishClose(req.SessionID, "start_failed")
		return
	}

	run := &sessionRun{
		id:      req.SessionID,
		cancel:  cancel,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		created: time.Now().UTC(),
	}
	h.mu.Lock()
	h.sessions[req.SessionID] = run
	h.mu.Unlock()

	h.log.Info("shell: session started",
		"session_id", req.SessionID,
		"protocol", string(req.Protocol),
		"pid", cmd.Process.Pid,
	)

	// Subscribe to stdin + resize for this session.
	stdinSub, err := h.nc.Subscribe(StdinSubject(h.cfg.AgentID, req.SessionID), func(m *nats.Msg) {
		var p StdinPayload
		if err := json.Unmarshal(m.Data, &p); err != nil {
			return
		}
		data, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			return
		}
		_, _ = stdin.Write(data)
	})
	if err != nil {
		h.log.Warn("shell: stdin subscribe failed", "err", err)
	}

	resizeSub, err := h.nc.Subscribe(ResizeSubject(h.cfg.AgentID, req.SessionID), func(m *nats.Msg) {
		var p ResizePayload
		if err := json.Unmarshal(m.Data, &p); err != nil {
			return
		}
		// On Unix we send SIGWINCH to the child process group; on
		// Windows we'd call SetConsoleScreenBufferSize, which the
		// process itself owns. Best-effort: just log.
		h.handleResize(run, p.Cols, p.Rows)
	})
	if err != nil {
		h.log.Warn("shell: resize subscribe failed", "err", err)
	}

	run.stdinSub = stdinSub
	run.resizeSub = resizeSub

	// Pump stdout + stderr back to the server.
	go h.pumpStream(req.SessionID, "stdout", stdout)
	go h.pumpStream(req.SessionID, "stderr", stderr)

	// Watch for process exit.
	go func() {
		err := cmd.Wait()
		h.log.Info("shell: session ended",
			"session_id", req.SessionID,
			"err", err,
		)
		if stdinSub != nil {
			_ = stdinSub.Unsubscribe()
		}
		if resizeSub != nil {
			_ = resizeSub.Unsubscribe()
		}
		h.mu.Lock()
		delete(h.sessions, req.SessionID)
		h.mu.Unlock()
		<-h.sem
		h.publishClose(req.SessionID, "process_exit")
		cancel()
	}()
}
