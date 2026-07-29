package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthMiddlewareFailClosed verifies that when the gateway is constructed
// with RequireAuth=true, the AuthMiddleware rejects requests without valid
// credentials (401) instead of proceeding with a nil identity. This is the
// regression guard for the missing-auth finding where the gateway was built
// with the zero-value Config{} (RequireAuth=false) and every authorize()
// check short-circuited on a nil identity.
func TestAuthMiddlewareFailClosed(t *testing.T) {
	auth := NewAuthenticator(Config{RequireAuth: true})

	// Terminal handler that fails the test if reached without auth.
	protected := AuthMiddleware(auth, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		if id == nil {
			t.Error("reached protected handler with nil identity")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/a2a/", nil)
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated: expected 401, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects malformed bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/a2a/", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-token")
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("bad token: expected 401, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("accepts valid token and populates identity", func(t *testing.T) {
		auth.AddStaticToken("valid-token", &Identity{
			Subject: "user-1",
			Method:  AuthBearer,
			Scopes:  []string{PermRead, PermSend, PermAdmin},
		})
		req := httptest.NewRequest(http.MethodPost, "/a2a/", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("valid token: expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})
}

// TestAuthorizeRespectsRequireAuth verifies the gateway's authorize() helper
// now enforces an identity once RequireAuth is true (previously it returned
// nil for any nil-identity request when RequireAuth was false).
func TestAuthorizeRespectsRequireAuth(t *testing.T) {
	t.Run("nil identity denied when RequireAuth true", func(t *testing.T) {
		gw := &Gateway{config: Config{RequireAuth: true}}
		if err := gw.authorize(nil, PermRead); err == nil {
			t.Error("expected authorize(nil) to fail when RequireAuth=true")
		}
	})
	t.Run("nil identity allowed when RequireAuth false (legacy)", func(t *testing.T) {
		gw := &Gateway{config: Config{RequireAuth: false}}
		if err := gw.authorize(nil, PermRead); err != nil {
			t.Errorf("expected authorize(nil) to pass when RequireAuth=false, got %v", err)
		}
	})
	t.Run("identity with matching scope allowed", func(t *testing.T) {
		gw := &Gateway{config: Config{RequireAuth: true}}
		id := &Identity{Scopes: []string{PermRead}}
		if err := gw.authorize(id, PermRead); err != nil {
			t.Errorf("expected authorize(read) to pass, got %v", err)
		}
	})
	t.Run("identity without matching scope denied", func(t *testing.T) {
		gw := &Gateway{config: Config{RequireAuth: true}}
		id := &Identity{Scopes: []string{PermRead}}
		if err := gw.authorize(id, PermAdmin); err == nil {
			t.Error("expected authorize(admin) to fail for read-only identity")
		}
	})
}
