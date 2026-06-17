package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/config"
)

// TestHealthEndpointReturns200 verifies that the chi Heartbeat middleware
// registered at /healthz answers 200 OK to GET requests without any
// authentication or database connection.
func TestHealthEndpointReturns200(t *testing.T) {
	cfg := &config.Config{
		HTTPPort:        "8080",
		Env:             "development",
		LogLevel:        "error",
		SessionIssuer:   "test",
		SessionAudience: "test",
		CookieDomain:    "localhost",
		CookieSecure:    false,
	}

	// NewServer will try to dial Postgres and NATS at construction
	// time. We can't run a full server here without infrastructure,
	// so we build the bare router manually using buildRouter via the
	// Server's router. NewServer tolerates a nil pool and a nil
	// publisher, so this should still succeed.
	s := NewServer(cfg, newDiscardLogger(), nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("/healthz returned %d; want 200", rr.Code)
	}
}

// TestRootReturnsServiceBanner verifies that GET / answers with 200
// and the service banner JSON. This guards against regressions in the
// top-level route registration.
func TestRootReturnsServiceBanner(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("/ returned %d; want 200", rr.Code)
	}
	body := rr.Body.String()
	if body == "" {
		t.Error("/ returned empty body")
	}
}