package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecurityHeadersMiddleware verifies the browser security headers are set
// on every response. Previously none of these were emitted (no nosniff, no
// frame protection, no referrer policy, no HSTS), leaving the admin console
// frameable and JSON responses MIME-sniffable.
func TestSecurityHeadersMiddleware(t *testing.T) {
	h := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("sets base headers on plaintext request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want DENY", got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("Referrer-Policy = %q, want no-referrer", got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("Content-Security-Policy = %q, want frame-ancestors 'none'", got)
		}
		// No TLS -> no HSTS.
		if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("HSTS should be absent on plaintext, got %q", got)
		}
	})

	t.Run("sets HSTS only on TLS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		req.TLS = &tls.ConnectionState{} // simulate a TLS connection
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
			t.Error("expected HSTS header on TLS request, got empty")
		}
	})
}
