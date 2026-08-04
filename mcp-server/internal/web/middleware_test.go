package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/thearchitectit/guardrail-mcp/internal/config"
)

// newAuthTestServer builds an echo instance with APIKeyAuth and an /ide/*
// route, ready for assertions about which key type may reach /ide endpoints.
func newAuthTestServer(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	cfg := &config.Config{
		MCPAPIKey: "secret-mcp-key",
		IDEAPIKey: "secret-ide-key",
	}
	ok := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	// Group everything (including /ide) behind the APIKeyAuth middleware.
	g := e.Group("", APIKeyAuth(cfg))
	g.GET("/ide/tools", ok)
	g.GET("/mcp/tools", ok)
	return e
}

// TestAPIKeyAuthIDERequiresIDEKey verifies the /ide/* guard rejects the MCP
// key and accepts the IDE key. Previously the guard condition was always
// false (dead code), so the MCP key could reach every /ide/* endpoint.
func TestAPIKeyAuthIDERequiresIDEKey(t *testing.T) {
	e := newAuthTestServer(t)

	// MCP key must NOT reach /ide/* (this is the regression).
	req := httptest.NewRequest(http.MethodGet, "/ide/tools", nil)
	req.Header.Set("Authorization", "Bearer secret-mcp-key")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("MCP key on /ide/*: expected 403, got %d", rec.Code)
	}

	// IDE key reaches /ide/*.
	req = httptest.NewRequest(http.MethodGet, "/ide/tools", nil)
	req.Header.Set("Authorization", "Bearer secret-ide-key")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("IDE key on /ide/*: expected 200, got %d", rec.Code)
	}
}

// TestAPIKeyAuthMCPReachable verifies the MCP key still works on its own
// surface (regression guard against over-restricting).
func TestAPIKeyAuthMCPReachable(t *testing.T) {
	e := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/mcp/tools", nil)
	req.Header.Set("Authorization", "Bearer secret-mcp-key")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("MCP key on /mcp/*: expected 200, got %d", rec.Code)
	}
}

// TestAPIKeyAuthRejectsBadKey verifies an unknown key is rejected.
func TestAPIKeyAuthRejectsBadKey(t *testing.T) {
	e := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/mcp/tools", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad key: expected 401, got %d", rec.Code)
	}
}
