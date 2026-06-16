package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

const sessionCookieName = "oap_session"

// registerRoutes wires up the public auth flow and the protected API.
func (s *Server) registerRoutes(r chi.Router) {
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"openagentplatform","version":"0.1.0"}`))
	})

	// Public auth endpoints.
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", s.handleLogin)
		r.Get("/callback", s.handleCallback)
		r.Post("/logout", s.handleLogout)
		r.Get("/me", s.handleMe)
	})

	// Protected API.
	r.Group(func(r chi.Router) {
		r.Use(auth.VerifierMiddleware(s.sessionMinter, s.oidcVerifier, sessionCookieName))
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/health", s.healthz)

			r.Route("/agents", func(r chi.Router) {
				r.Get("/", s.listAgents)
				r.Post("/", s.createAgent)
			})

			r.Route("/sites", func(r chi.Router) {
				r.Get("/", s.listSites)
			})

			r.Route("/checks", func(r chi.Router) {
				r.Get("/", s.listChecks)
			})

			r.Route("/alerts", func(r chi.Router) {
				r.Get("/", s.listAlerts)
			})
		})
	})
}

// handleLogin redirects the user-agent to the OIDC provider's auth endpoint.
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

func (s *Server) listChecks(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

func (s *Server) listAlerts(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

// bearerOrCookie extracts a token from Authorization header or session cookie.
func bearerOrCookie(r *http.Request, cookieName string) string {
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// oidcAuthURL builds the authorization URL against the OIDC issuer.
func (s *Server) oidcAuthURL(state string) string {
	u, _ := url.Parse(s.cfg.OIDCIssuerURL + "/auth")
	q := u.Query()
	q.Set("client_id", s.cfg.OIDCClientID)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile groups")
	q.Set("redirect_uri", s.cfg.OIDCRedirectURL)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

// exchangeCode performs the OIDC token exchange using client credentials.
func (s *Server) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {s.cfg.OIDCClientID},
		"client_secret": {s.cfg.OIDCClientSecret},
		"redirect_uri":  {s.cfg.OIDCRedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.OIDCIssuerURL+"/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = http.NoBody

	// Use the stdlib client with a form body.
	client := &http.Client{Timeout: 10 * time.Second}
	req.Body = nil
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.OIDCIssuerURL+"/token", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// Encode form into body using a buffer-backed reader.
	req.Body = formBody(form)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", httpErr{Status: resp.StatusCode, URL: req.URL.String()}
	}
	var tokenResp struct {
		IDToken string `json:"id_token"`
		Token   string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	if tokenResp.IDToken == "" {
		return "", errors.New("oidc: empty id_token in token response")
	}
	return tokenResp.IDToken, nil
}

type httpErr struct {
	Status int
	URL    string
}

func (e httpErr) Error() string {
	return "oidc: token endpoint returned status " + itoa(e.Status)
}

func itoa(i int) string {
	// minimal alloc-free path
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// randomState returns a random URL-safe string.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := randRead(b); err != nil {
		return "", err
	}
	return base64URL(b), nil
}
