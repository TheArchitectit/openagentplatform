package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

func actorFromContext(r *http.Request) string {
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		if claims.Subject != "" {
			return claims.Subject
		}
	}
	return "unknown"
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

// recordLogin writes a "login" audit event for a successful OIDC callback.
// Failures are logged but do not block the response.
func (s *Server) recordLogin(r *http.Request, claims *auth.Claims) {
	if s.audit == nil || claims == nil {
		return
	}
	// Use a detached context so the audit write survives the request
	// being cancelled by the browser navigating away.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      claims.Subject,
		Action:       string(audit.EventLogin),
		ResourceType: "session",
		ResourceID:   claims.Subject,
		Details: map[string]any{
			"email": claims.Email,
			"role":  auth.MapGroupsToRole(claims.Groups),
		},
		Outcome:   audit.OutcomeSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		OrgID:     claims.OrgID,
		SiteID:    claims.SiteID,
	})
	if err != nil {
		s.log.Error("audit: login record failed", "err", err)
	}
}

// recordLogout writes a "logout" audit event. We try to attribute the event
// to the authenticated user, but fall back to "unknown" if the session has
// already been invalidated.
func (s *Server) recordLogout(r *http.Request) {
	if s.audit == nil {
		return
	}
	actorID := ""
	orgID := ""
	siteID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		actorID = claims.Subject
		orgID = claims.OrgID
		siteID = claims.SiteID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actorID,
		Action:       string(audit.EventLogout),
		ResourceType: "session",
		ResourceID:   actorID,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
		SiteID:       siteID,
	})
	if err != nil {
		s.log.Error("audit: logout record failed", "err", err)
	}
}

// clientIP is duplicated from the audit middleware so the auth handlers
// (which run before middleware-injected request IDs) can still attribute
// the event to a client. chi's RealIP middleware sets X-Forwarded-For /
// X-Real-IP, so we honour those here too.
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if comma := strings.Index(h, ","); comma >= 0 {
			return strings.TrimSpace(h[:comma])
		}
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return strings.TrimSpace(h)
	}
	return r.RemoteAddr
}

// orgContextMiddleware ensures every authenticated request carries an OrgID
// in its session claims. If no org context is present, the request is
// rejected with 400. This enforces multi-tenant isolation: every API call
// must be scoped to the caller's organization.
func orgContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.UserFromContext(r.Context())
		if !ok || claims == nil {
			// No claims means the request is unauthenticated; the auth
			// middleware should have already rejected it.
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if claims.OrgID == "" {
			http.Error(w, `{"error":"org context required"}`, http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps http.ResponseWriter so we can capture the status
// code for metrics emission.  The default http.ResponseWriter does not
// expose the status once it has been written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// routeLabel returns the chi route pattern for the current request, or
// "unmatched" when the request did not match a registered route.  This is
// what we expose as the "path" label on api_requests_total so we avoid
// high-cardinality URL explosions.
func routeLabel(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

// metricsMiddleware records request count and duration for every request
// handled by the API.  It should be installed near the top of the
// middleware stack so it captures all responses, including 401s and
// 500s.  The /metrics endpoint itself is excluded to keep the scrape
// from polluting the request rate.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't count scrapes of the metrics endpoint itself.
		if r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/api/v1/metrics") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		path := routeLabel(r)
		status := strconv.Itoa(rec.status)
		telemetry.RecordAPIRequest(r.Method, path, status)
		telemetry.ObserveHTTPRequestDuration(r.Method, path, time.Since(start).Seconds())
	})
}
