package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidcVerifier == nil {
		http.Error(w, `{"error":"oidc_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	state, err := randomState()
	if err != nil {
		http.Error(w, `{"error":"state_generation_failed"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oap_oauth_state",
		Value:    state,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	authURL := s.oidcAuthURL(state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback exchanges the OIDC code for an ID token, verifies it, mints
// an internal session JWT, and sets the session cookie.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidcVerifier == nil {
		http.Error(w, `{"error":"oidc_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	stateCookie, err := r.Cookie("oap_oauth_state")
	if err != nil || stateCookie.Value == "" {
		http.Error(w, `{"error":"missing_state_cookie"}`, http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, `{"error":"state_mismatch"}`, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing_code"}`, http.StatusBadRequest)
		return
	}

	idToken, err := s.exchangeCode(r.Context(), code)
	if err != nil {
		s.log.Error("oidc code exchange failed", "err", err)
		http.Error(w, `{"error":"code_exchange_failed"}`, http.StatusBadGateway)
		return
	}

	claims, err := s.oidcVerifier.Verify(r.Context(), idToken)
	if err != nil {
		s.log.Error("oidc verify failed", "err", err)
		http.Error(w, `{"error":"id_token_invalid"}`, http.StatusUnauthorized)
		return
	}

	sessionTok, err := s.sessionMinter.Mint(claims)
	if err != nil {
		s.log.Error("session mint failed", "err", err)
		http.Error(w, `{"error":"session_mint_failed"}`, http.StatusInternalServerError)
		return
	}

	s.recordLogin(r, claims)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionTok,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((1 * time.Hour).Seconds()),
	})
	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "oap_oauth_state",
		Value:    "",
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	// Redirect back to the web UI.
	http.Redirect(w, r, s.cfg.OIDCRedirectURL, http.StatusFound)
}

// handleLogout clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.recordLogout(r)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"logged_out"}`))
}

// handleMe returns the authenticated user from the session.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	// Allow either middleware-authenticated requests or direct cookie reads
	// for the browser flow.
	sm := s.sessionMinter
	if sm == nil {
		http.Error(w, `{"error":"session_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	tok := bearerOrCookie(r, sessionCookieName)
	if tok == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	claims, err := sm.Parse(tok)
	if err != nil {
		http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sub":    claims.Subject,
		"email":  claims.Email,
		"name":   claims.Name,
		"role":   claims.Role,
		"org_id": claims.OrgID,
	})
}

func (s *Server) listAgents(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

func (s *Server) createAgent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{}`))
}

func (s *Server) listSites(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
		return
	}
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	alerts, _, err := s.alertStore.ListAlerts(r.Context(), alerts.AlertFilter{OrgID: orgID, Limit: 50})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
		return
	}
	if alerts == nil {
		alerts = []models.Alert{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(alerts)
}

// getAlert returns a single alert by id, including its state history.
func (s *Server) getAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	alert, err := s.alertStore.GetAlert(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get alert failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	history, _ := s.alertStore.GetStateHistory(r.Context(), id)
	notifs, _ := s.alertStore.GetNotificationHistory(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"alert":                alert,
		"state_history":        history,
		"notification_history": notifs,
	})
}

// acknowledgeAlert transitions an alert to acknowledged.
func (s *Server) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	if err := s.alertEngine.Acknowledge(r.Context(), orgID, id, actor); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("acknowledge failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"acknowledged"}`))
}

// snoozeAlert transitions an alert to snoozed with a duration from the body.
func (s *Server) snoozeAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var body struct {
		DurationMinutes int `json:"duration_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if body.DurationMinutes <= 0 {
		http.Error(w, `{"error":"duration_minutes_required"}`, http.StatusBadRequest)
		return
	}
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	duration := time.Duration(body.DurationMinutes) * time.Minute
	if err := s.alertEngine.Snooze(r.Context(), orgID, id, actor, duration); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("snooze failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"snoozed"}`))
}

// resolveAlert transitions an alert to resolved.
func (s *Server) resolveAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	if err := s.alertEngine.Resolve(r.Context(), orgID, id, actor); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("resolve failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"resolved"}`))
}

// closeAlert transitions an alert to closed.
func (s *Server) closeAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	if err := s.alertEngine.Close(r.Context(), orgID, id, actor); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("close failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"closed"}`))
}

// listAlertRules returns all alert rules, optionally filtered by org_id.
func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	orgID := orgIDFromContext(r)
	rules, err := s.alertStore.GetAlertRules(r.Context(), orgID)
	if err != nil {
		s.log.Error("list alert rules failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rules)
}

// createAlertRule creates a new alert rule.
func (s *Server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if rule.ID == "" {
		rule.ID = uuidNew()
	}
	now := time.Now().UTC()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if err := s.alertStore.CreateAlertRule(r.Context(), &rule); err != nil {
		s.log.Error("create alert rule failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rule)
}

// updateAlertRule updates an existing alert rule.
func (s *Server) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	rule.ID = id
	rule.UpdatedAt = time.Now().UTC()
	if err := s.alertStore.UpdateAlertRule(r.Context(), &rule); err != nil {
		if errors.Is(err, alerts.ErrAlertRuleNotFound) {
			http.Error(w, `{"error":"rule_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("update alert rule failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rule)
}

// deleteAlertRule deletes an alert rule by id.
func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := orgIDFromContext(r)
	if err := s.alertStore.DeleteAlertRule(r.Context(), orgID, id); err != nil {
		if errors.Is(err, alerts.ErrAlertRuleNotFound) {
			http.Error(w, `{"error":"rule_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("delete alert rule failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// actorFromContext extracts the actor identifier (user subject or "system")
// from the request context. Returns "unknown" if no auth claims are present.
