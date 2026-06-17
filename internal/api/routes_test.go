package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/config"
)

// routesToSmoke is a set of paths that the public route table must
// accept without panicking. Each is dispatched against a freshly
// constructed router with a nil DB pool and nil publisher; we only
// care that the routing layer doesn't panic, not that the handlers
// return 200 (auth, DB, etc. are not available in unit-test context).
//
// Add new paths here as they are introduced so future refactors keep
// the full route table covered.
var routesToSmoke = []struct {
	name   string
	method string
	path   string
}{
	{"root", http.MethodGet, "/"},
	{"healthz", http.MethodGet, "/healthz"},
	{"auth_login", http.MethodGet, "/auth/login"},
	{"auth_callback", http.MethodGet, "/auth/callback"},
	{"auth_logout", http.MethodPost, "/auth/logout"},
	{"auth_session", http.MethodGet, "/auth/session"},
	{"api_agents", http.MethodGet, "/api/v1/agents"},
	{"api_sites", http.MethodGet, "/api/v1/sites"},
	{"api_checks", http.MethodGet, "/api/v1/checks"},
	{"api_alerts", http.MethodGet, "/api/v1/alerts"},
	{"api_alert_rules", http.MethodGet, "/api/v1/alert-rules"},
	{"api_alert_preferences", http.MethodGet, "/api/v1/alert-preferences"},
	{"api_policies", http.MethodGet, "/api/v1/policies"},
	{"api_compliance", http.MethodGet, "/api/v1/compliance"},
	{"api_patches", http.MethodGet, "/api/v1/patches"},
	{"api_scripts", http.MethodGet, "/api/v1/scripts"},
	{"api_sessions", http.MethodGet, "/api/v1/sessions"},
	{"api_remote_shell", http.MethodGet, "/api/v1/remote-shell"},
	{"api_inventory", http.MethodGet, "/api/v1/inventory"},
	{"api_deployments", http.MethodGet, "/api/v1/deployments"},
	{"ws", http.MethodGet, "/ws"},
}

// TestRoutesDoNotPanic walks every known route and verifies that the
// router dispatches it without panicking. Authentication and database
// dependencies are nil; we don't assert on the response status code
// (handlers may legitimately 401/503 in this context), only that
// routing does not blow up.
func TestRoutesDoNotPanic(t *testing.T) {
	cfg := &config.Config{
		HTTPPort:        "8080",
		Env:             "development",
		LogLevel:        "error",
		SessionIssuer:   "test",
		SessionAudience: "test",
		CookieDomain:    "localhost",
		CookieSecure:    false,
	}
	s := NewServer(cfg, newDiscardLogger(), nil, nil, nil)
	handler := s.Router()

	for _, tc := range routesToSmoke {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("route %s %s panicked: %v", tc.method, tc.path, r)
				}
			}()

			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			// We do not assert on rr.Code here; auth/DB may be nil.
			// The router must produce *some* non-panic response.
			if rr.Code == 0 {
				t.Errorf("route %s %s produced empty response code", tc.method, tc.path)
			}
		})
	}
}