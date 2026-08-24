package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"
	"github.com/gorilla/websocket"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/remote"
)

type shellBridge struct {
	handler   *RemoteHandler
	session   *remote.ShellSession
	conn      *websocket.Conn
	wsOut     chan wsOutMsg
	natsSub   NATSSub
	recorder  *remote.SessionRecorder
	closeOnce sync.Once
	closed    chan struct{}
}

// wsOutMsg is a message heading from NATS to the WebSocket.
type wsOutMsg struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

func newShellBridge(h *RemoteHandler, sess *remote.ShellSession, conn *websocket.Conn) *shellBridge {
	return &shellBridge{
		handler: h,
		session: sess,
		conn:    conn,
		wsOut:   make(chan wsOutMsg, 128),
		closed:  make(chan struct{}),
	}
}

// run subscribes to the agent's stdout subject, then pumps the
// read/write loops. The function returns when the user disconnects
// or the session is killed.
func (b *shellBridge) run() {
	defer b.shutdown("ws_close")

	// Subscribe to stdout if we have a NATS connection.
	if b.handler.NATSConn != nil {
		sub, err := b.handler.NATSConn.Subscribe(b.session.StdoutSubject, func(m *natsMsg) {
			var p remote.StdinPayload
			if err := decodeNATSMsg(m, &p); err != nil {
				return
			}
			if b.recorder != nil {
				if raw, decErr := base64.StdEncoding.DecodeString(p.Data); decErr == nil {
					b.recorder.RecordOutput(raw)
				}
			}
			select {
			case b.wsOut <- wsOutMsg{Type: "stdout", Data: p.Data}:
			default:
				// Drop on backpressure rather than block.
			}
		})
		if err != nil {
			b.handler.Logger.Warn("shell: subscribe stdout failed", "err", err)
		} else {
			b.natsSub = sub
		}
	}

	// Attach the live recorder when a factory is wired. Recording is
	// best-effort: failure to attach logs but does not block the session.
	if b.handler.RecorderFactory != nil {
		if rec, ok := b.handler.RecorderFactory(b.session.ID); ok && rec != nil {
			b.recorder = rec
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := b.recorder.Close(ctx, time.Now().UTC()); err != nil {
					b.handler.Logger.Warn("shell: recorder close failed", "session_id", b.session.ID, "err", err)
				}
			}()
		} else {
			b.handler.Logger.Warn("shell: recorder attach failed, session not recorded", "session_id", b.session.ID)
		}
	}

	// Greet the client.
	_ = b.conn.WriteJSON(map[string]any{
		"type": "hello",
		"data": map[string]any{
			"session_id": b.session.ID,
			"protocol":   string(b.session.Protocol),
		},
	})

	go b.writeLoop()
	b.readLoop()
}

// readLoop consumes frames from the browser and publishes them to
// the agent. Supports stdin, resize, and ping frames.
func (b *shellBridge) readLoop() {
	b.conn.SetReadLimit(64 * 1024)
	_ = b.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	b.conn.SetPongHandler(func(string) error {
		_ = b.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	for {
		_, raw, err := b.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "stdin":
			if msg.Data == "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				continue
			}
			if b.recorder != nil {
				b.recorder.RecordInput(data)
			}
			if b.handler.Manager != nil {
				_, _ = b.handler.Manager.PublishStdin(context.Background(), b.session.ID, data)
			}
		case "resize":
			if b.recorder != nil {
				b.recorder.RecordResize(msg.Cols, msg.Rows)
			}
			if b.handler.Manager != nil {
				_ = b.handler.Manager.PublishResize(context.Background(), b.session.ID, msg.Cols, msg.Rows)
			}
		case "ping":
			_ = b.conn.WriteJSON(map[string]any{"type": "pong"})
		}
	}
}

// writeLoop drains wsOut onto the WebSocket and emits app pings.
func (b *shellBridge) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case m := <-b.wsOut:
			_ = b.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := b.conn.WriteJSON(m); err != nil {
				b.shutdown("ws_write_failed")
				return
			}
		case <-ticker.C:
			_ = b.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := b.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				b.shutdown("ws_ping_failed")
				return
			}
		case <-b.closed:
			return
		}
	}
}

// shutdown is idempotent.
func (b *shellBridge) shutdown(reason string) {
	b.closeOnce.Do(func() {
		close(b.closed)
		if b.natsSub != nil {
			_ = b.natsSub.Unsubscribe()
		}
		_ = b.conn.Close()
		if b.handler.Manager != nil {
			b.handler.Manager.CloseByAgent(b.session.ID)
		}
		if b.handler.CredStore != nil {
			b.handler.CredStore.RotateOnClose(b.session.AgentID)
		}
		if b.handler.Logger != nil {
			b.handler.Logger.Info("shell bridge closed",
				"session_id", b.session.ID,
				"reason", reason,
			)
		}
	})
}

// recordAudit writes a detached audit event. It logs directly since
// the audit service is not injected into the handler today; a future
// change will thread the service through.
func (h *RemoteHandler) recordAudit(parent context.Context, ev audit.EventInput) {
	if h == nil || h.Logger == nil {
		return
	}
	h.Logger.Info("audit",
		"action", ev.Action,
		"resource_type", ev.ResourceType,
		"resource_id", ev.ResourceID,
		"actor_id", ev.ActorID,
		"outcome", ev.Outcome,
	)
}

// --- helpers ----------------------------------------------------------

// isAdminRole returns true if the role grants admin powers.
func isAdminRole(role string) bool {
	switch role {
	case "admin", "owner", "superadmin":
		return true
	}
	return false
}

// hasRemoteShellPermission returns true if the role may create shell
// sessions. Admins and operators do; viewers and reporters do not.
func hasRemoteShellPermission(role string) bool {
	if isAdminRole(role) {
		return true
	}
	if role == "operator" {
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// verifyWSUser parses the supplied token into SessionClaims via the
// configured SessionMinter. If no SessionMinter is configured, all requests
// are rejected (fail-closed). Previously this synthesised an admin identity
// for any non-empty token when no minter was present — a dev shortcut that
// would grant full admin shell access the moment the remote-shell endpoint
// was wired up.
func (h *RemoteHandler) verifyWSUser(tok string) (*auth.SessionClaims, bool) {
	if h.SessionMinter == nil {
		return nil, false
	}
	c, err := h.SessionMinter.Parse(tok)
	if err != nil || c == nil {
		return nil, false
	}
	return c, true
}
