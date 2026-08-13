package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

func actorFromContext(r *http.Request) string {
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		if claims.Subject != "" {
			return claims.Subject
		}
	}
	return "unknown"
}

func orgIDFromContext(r *http.Request) string {
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		return claims.OrgID
	}
	return ""
}

// uuidNew returns a new UUID v4 string. Wrapped here so callers don't
// need to import the uuid package directly.
func uuidNew() string {
	return uuid.New().String()
}

// --- Remote shell route shims -----------------------------------------
//
// These methods all forward to the *RemoteHandler if one is
// configured; otherwise they return 503. Keeping them on Server
// preserves the existing pattern (every other route in this file
// is a method on Server).

func (s *Server) handleRemoteListSessions(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleListShellSessions(w, r)
}

func (s *Server) handleRemoteGetSession(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleGetShellSession(w, r)
}

func (s *Server) handleRemoteKillSession(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleKillShellSession(w, r)
}

func (s *Server) handleRemoteCreateSession(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleCreateShellSession(w, r)
}

func (s *Server) handleRemoteStoreCredential(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleStoreCredential(w, r)
}

func (s *Server) handleRemoteListCredentials(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleListCredentials(w, r)
}

func (s *Server) handleRemoteDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleDeleteCredential(w, r)
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
// exchangeCode performs the OIDC token exchange using client credentials.
//
// Only a single request is built and sent. The client_secret is included
// only when configured; the default `oap-server` Dex client is public and
// rejects the secret, and including it for a confidential client is the
// standard PKCE/client-secret flow.
func (s *Server) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"client_id":    {s.cfg.OIDCClientID},
		"redirect_uri": {s.cfg.OIDCRedirectURL},
	}
	if s.cfg.OIDCClientSecret != "" {
		form.Set("client_secret", s.cfg.OIDCClientSecret)
	}
	// Encode the form into the request body via a buffer-backed reader.
	body := formBody(form)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.OIDCIssuerURL+"/token", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
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
