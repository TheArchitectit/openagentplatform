package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/remote"
)

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
