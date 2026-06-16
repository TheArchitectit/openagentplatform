package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/remote"
)

// PermissionRemoteShell is the RBAC permission required to create a
// remote shell session. Admin/operator roles are expected to grant
// this.
const PermissionRemoteShell = "remote:shell"

// RemoteHandler bundles the dependencies the remote-shell API needs.
// It is wired into Server via SetRemoteHandler.
type RemoteHandler struct {
	Manager   *remote.ShellManager
	CredStore *remote.CredentialStore
	Resolver  *remote.Resolver
	Logger    *slog.Logger
	// BaseURL is the public URL the WebSocket client should connect
	// to. It is read by HandleCreateShellSession to build ws_url.
	BaseURL string
	// SessionMinter is used to verify session JWTs in the WebSocket
	// upgrade path. If nil the handler refuses to accept WS upgrades
	// (except in the dev fallback).
	SessionMinter *auth.SessionMinter
	// CookieName is the session cookie to read from on the upgrade.
	CookieName string
	// NATSConn is the connection used to subscribe to per-session
	// stdout subjects. May be nil in dev/test mode.
	NATSConn NATSConn
}

// NATSConn is the subset of *nats.Conn used by the shell bridge.
type NATSConn interface {
	Subscribe(subj string, cb natsMsgHandler) (NATSSub, error)
	Publish(subj string, data []byte) error
}

// natsMsgHandler matches nats.MsgHandler.
type natsMsgHandler func(*natsMsg)

// NATSSub is the subset of *nats.Subscription used here.
type NATSSub interface {
	Unsubscribe() error
}

// NewRemoteHandler constructs a handler with safe defaults.
func NewRemoteHandler(log *slog.Logger) *RemoteHandler {
	return &RemoteHandler{
		Logger:     log,
		CookieName: "oap_session",
	}
}

// SetRemoteHandler wires the remote-handler dependencies into the
// server. The handler may be nil — endpoints will then return 503.
func (s *Server) SetRemoteHandler(h *RemoteHandler) {
	s.remote = h
}

// HandleListShellSessions returns the sessions visible to the caller.
func (h *RemoteHandler) HandleListShellSessions(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Manager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "shell_manager_not_configured")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	admin := isAdminRole(claims.Role)
	sessions := h.Manager.List(claims.Subject, admin)
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// HandleCreateShellSession creates a new shell session. Body
// specifies protocol + (optional) terminal size. Returns the
// session_id and a ws_url the browser can connect to.
func (h *RemoteHandler) HandleCreateShellSession(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Manager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "shell_manager_not_configured")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !hasRemoteShellPermission(claims.Role) {
		writeJSONError(w, http.StatusForbidden, "remote_shell_forbidden")
		return
	}
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		writeJSONError(w, http.StatusBadRequest, "agent_id_required")
		return
	}

	var body struct {
		Protocol     string `json:"protocol"`
		TerminalCols int    `json:"terminal_cols"`
		TerminalRows int    `json:"terminal_rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	proto := remote.Protocol(body.Protocol)
	if proto != remote.ProtocolSSH && proto != remote.ProtocolWinRM {
		writeJSONError(w, http.StatusBadRequest, "protocol_must_be_ssh_or_winrm")
		return
	}

	sess, err := h.Manager.CreateSession(agentID, claims.Subject, proto,
		remote.TerminalSize{Cols: body.TerminalCols, Rows: body.TerminalRows})
	if err != nil {
		writeJSONError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	wsURL := h.BaseURL + "/api/v1/shell/" + sess.ID + "/ws"

	h.Logger.Info("shell session created via API",
		"session_id", sess.ID,
		"agent_id", agentID,
		"user_id", claims.Subject,
		"protocol", string(proto),
	)

	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id": sess.ID,
		"agent_id":   agentID,
		"protocol":   string(proto),
		"ws_url":     wsURL,
		"started_at": sess.StartedAt,
	})
}

// HandleGetShellSession returns status + metadata for one session.
func (h *RemoteHandler) HandleGetShellSession(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Manager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "shell_manager_not_configured")
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	id := chi.URLParam(r, "session_id")
	sess := h.Manager.Get(id)
	if sess == nil {
		writeJSONError(w, http.StatusNotFound, "session_not_found")
		return
	}
	if claims != nil && !isAdminRole(claims.Role) && sess.UserID != claims.Subject {
		writeJSONError(w, http.StatusForbidden, "session_forbidden")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// HandleKillShellSession force-kills a session.
func (h *RemoteHandler) HandleKillShellSession(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Manager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "shell_manager_not_configured")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "session_id")
	admin := isAdminRole(claims.Role)
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "killed_by_user"
	}
	if err := h.Manager.Kill(id, claims.Subject, admin, reason); err != nil {
		switch {
		case errors.Is(err, remote.ErrSessionNotFound):
			writeJSONError(w, http.StatusNotFound, "session_not_found")
		case errors.Is(err, remote.ErrSessionForbidden):
			writeJSONError(w, http.StatusForbidden, "session_forbidden")
		default:
			writeJSONError(w, http.StatusInternalServerError, "kill_failed")
		}
		return
	}
	go h.recordAudit(r.Context(), audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      claims.Subject,
		Action:       "shell.kill",
		ResourceType: "shell_session",
		ResourceID:   id,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        claims.OrgID,
		SiteID:       claims.SiteID,
	})
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"killed"}`))
}

// HandleStoreCredential creates or updates a stored credential.
func (h *RemoteHandler) HandleStoreCredential(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.CredStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "credential_store_not_configured")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || !isAdminRole(claims.Role) {
		writeJSONError(w, http.StatusForbidden, "admin_required")
		return
	}
	var body struct {
		Username   string `json:"username"`
		Type       string `json:"type"`
		AgentID    string `json:"agent_id,omitempty"`
		SiteID     string `json:"site_id,omitempty"`
		OrgDefault bool   `json:"org_default,omitempty"`
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if body.Username == "" || body.Credential == "" {
		writeJSONError(w, http.StatusBadRequest, "username_and_credential_required")
		return
	}
	ct := remote.CredentialType(body.Type)
	if ct == "" {
		ct = remote.CredentialPassword
	}
	c := &remote.RemoteCredential{
		Username:   body.Username,
		Type:       ct,
		AgentID:    body.AgentID,
		SiteID:     body.SiteID,
		OrgDefault: body.OrgDefault,
	}
	plaintext := []byte(body.Credential)
	stored, err := h.CredStore.Store(c, plaintext)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store_failed")
		return
	}
	masked := *stored
	masked.EncryptedData = ""
	writeJSON(w, http.StatusCreated, masked)
}

// HandleListCredentials returns masked credentials.
func (h *RemoteHandler) HandleListCredentials(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.CredStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "credential_store_not_configured")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || !isAdminRole(claims.Role) {
		writeJSONError(w, http.StatusForbidden, "admin_required")
		return
	}
	creds := h.CredStore.List()
	writeJSON(w, http.StatusOK, map[string]any{"credentials": creds})
}

// HandleDeleteCredential removes a credential by ID.
func (h *RemoteHandler) HandleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.CredStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "credential_store_not_configured")
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || !isAdminRole(claims.Role) {
		writeJSONError(w, http.StatusForbidden, "admin_required")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.CredStore.Delete(id); err != nil {
		if errors.Is(err, remote.ErrCredentialNotFound) {
			writeJSONError(w, http.StatusNotFound, "credential_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "delete_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleShellWebSocket upgrades the HTTP request to a WebSocket and
// bridges it to NATS subjects for the requested session.
func (h *RemoteHandler) HandleShellWebSocket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Manager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "shell_manager_not_configured")
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	sess := h.Manager.Get(sessionID)
	if sess == nil {
		writeJSONError(w, http.StatusNotFound, "session_not_found")
		return
	}

	tok := bearerOrCookie(r, h.CookieName)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	claims, ok := h.verifyWSUser(tok)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	if !isAdminRole(claims.Role) && sess.UserID != claims.Subject {
		writeJSONError(w, http.StatusForbidden, "session_forbidden")
		return
	}
	if !hasRemoteShellPermission(claims.Role) {
		writeJSONError(w, http.StatusForbidden, "remote_shell_forbidden")
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.Logger.Warn("ws upgrade failed for shell", "err", err, "session_id", sessionID)
		return
	}

	bridge := newShellBridge(h, sess, conn)
	bridge.run()
}

// shellBridge owns the NATS subscription and the user-facing
// WebSocket connection for one session.
type shellBridge struct {
	handler    *RemoteHandler
	session    *remote.ShellSession
	conn       *websocket.Conn
	wsOut      chan wsOutMsg
	natsSub    NATSSub
	closeOnce  sync.Once
	closed     chan struct{}
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
			if b.handler.Manager != nil {
				_, _ = b.handler.Manager.PublishStdin(context.Background(), b.session.ID, data)
			}
		case "resize":
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

// verifyWSUser parses the supplied token into SessionClaims. If a
// SessionMinter is configured we use it; otherwise we accept any
// non-empty token and synthesise a dev user (insecure, dev only).
func (h *RemoteHandler) verifyWSUser(tok string) (*auth.SessionClaims, bool) {
	if h.SessionMinter == nil {
		if tok == "" {
			return nil, false
		}
		return &auth.SessionClaims{
			Role:             "admin",
			RegisteredClaims: devClaims("dev-user"),
		}, true
	}
	c, err := h.SessionMinter.Parse(tok)
	if err != nil || c == nil {
		return nil, false
	}
	return c, true
}
