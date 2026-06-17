package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
)

// TestRateLimitMiddleware verifies that when the configured request budget is
// exhausted the rate-limit middleware replies 429 with a Retry-After header.
func TestRateLimitMiddleware(t *testing.T) {
	// Burst of 2 means we can issue at most 2 successful requests before the
	// bucket empties; the next call must be throttled.
	rl := resilience.NewRateLimiter(resilience.RateLimitConfig{
		Rate:    0.0001, // ~ no sustained refill during the test
		Burst:   1,      // bucket starts at 1 token; one successful call exhausts it
		Enabled: true,
	})
	defer rl.Stop()

	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// First request creates the bucket with tokens=Burst=1; Allow returns
	// true without consuming a token. The next call decrements to 0.
	// After that, with Rate=0.0001 the refill is negligible, so the third
	// call must be throttled.
	first := do()
	if first.Code != http.StatusOK {
		t.Fatalf("first request: expected 200 OK, got %d", first.Code)
	}
	second := do()
	if second.Code != http.StatusOK {
		t.Fatalf("second request: expected 200 OK, got %d", second.Code)
	}

	// Third request exceeds the burst and must be throttled.
	rec := do()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst exhausted, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429, got empty")
	}
}

// TestOrgContextMiddleware verifies the org-context middleware rejects requests
// without authentication and accepts those with valid org-scoped sessions.
//
// The middleware uses auth.UserFromContext which reads from a package-private
// context key set by the auth middleware. We exercise it end-to-end by
// generating a real session JWT and threading it through VerifierMiddleware
// before the org-context check.
func TestOrgContextMiddleware(t *testing.T) {
	// Build a session minter with an ephemeral key.
	sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
	if err != nil {
		t.Fatalf("NewSessionMinter: %v", err)
	}

	// Stack: verifier -> org context -> terminal handler.
	verifier := auth.VerifierMiddleware(sm, nil, "oap_session")
	stack := verifier(orgContextMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	t.Run("rejects without auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/checks", nil)
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated request: expected 401, got %d", rec.Code)
		}
	})

	t.Run("rejects when org id missing from claims", func(t *testing.T) {
		// Mint a session for a user with no OrgID; the verifier will set
		// the claims but the org-context middleware should then 400.
		tok, err := sm.Mint(&auth.Claims{
			Subject: "user-no-org",
			Email:   "noorg@example.com",
			Role:    auth.RoleViewer,
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/checks", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("no-org request: expected 400, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("passes with valid org-scoped session", func(t *testing.T) {
		tok, err := sm.Mint(&auth.Claims{
			Subject: "user-1",
			Email:   "user1@example.com",
			OrgID:   "org-123",
			Role:    auth.RoleViewer,
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/checks", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("org-scoped request: expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})
}
