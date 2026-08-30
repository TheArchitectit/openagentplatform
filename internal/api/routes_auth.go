package api

import (
	"encoding/json"
	"net/http"
	"time"
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

	// Redirect back to the web UI. Relative default ("/") resolves against
	// the browser's origin, so this works direct-to-server and through the
	// web nginx proxy alike.
	http.Redirect(w, r, s.cfg.PostLoginRedirectURL, http.StatusFound)
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
