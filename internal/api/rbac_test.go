package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// TestRequireOrgAccess verifies RequireOrgAccess grants access when the
// session's OrgID matches the requested org and denies it otherwise.
func TestRequireOrgAccess(t *testing.T) {
	t.Run("denies without session claims", func(t *testing.T) {
		if RequireOrgAccess(context.Background(), "org-1") {
			t.Error("expected deny when no claims in context")
		}
	})

	t.Run("denies when requested org id is empty", func(t *testing.T) {
		if RequireOrgAccess(context.Background(), "") {
			t.Error("expected deny when requested org id is empty")
		}
	})

	t.Run("end-to-end via RequireOrgAccessHTTP", func(t *testing.T) {
		sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
		if err != nil {
			t.Fatalf("NewSessionMinter: %v", err)
		}
		verifier := auth.VerifierMiddleware(sm, nil, "oap_session")

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !RequireOrgAccess(r.Context(), "org-good") {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
		})

		stack := verifier(handler)

		// 1. No token → 401 from verifier.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/org-check", nil)
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("no-token: expected 401, got %d", rec.Code)
		}

		// 2. Token for a different org → 403 from RequireOrgAccess.
		tok, err := sm.Mint(&auth.Claims{
			Subject: "user-1",
			Email:   "u1@example.com",
			OrgID:   "org-other",
			Groups:  []string{"oap-viewers"},
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		req = httptest.NewRequest(http.MethodGet, "/api/v1/org-check", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec = httptest.NewRecorder()
		stack.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("wrong-org: expected 403, got %d (body=%s)", rec.Code, rec.Body.String())
		}

		// 3. Token for the matching org → 200.
		tok, err = sm.Mint(&auth.Claims{
			Subject: "user-1",
			Email:   "u1@example.com",
			OrgID:   "org-good",
			Groups:  []string{"oap-viewers"},
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		req = httptest.NewRequest(http.MethodGet, "/api/v1/org-check", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec = httptest.NewRecorder()
		stack.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("matching-org: expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})
}

// TestRequireRole verifies RequireRole grants access when the session's role
// matches one of the allowed roles and denies it otherwise.
//
// The session role is derived from the user's OIDC groups (see
// auth.MapGroupsToRole) — claims.Role set by the caller is overwritten
// during Mint — so the test passes group names and asserts against the
// resulting role constants.
func TestRequireRole(t *testing.T) {
	sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
	if err != nil {
		t.Fatalf("NewSessionMinter: %v", err)
	}
	verifier := auth.VerifierMiddleware(sm, nil, "oap_session")

	makeHandler := func(allowed ...string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !RequireRole(r.Context(), allowed...) {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	}

	mintFor := func(t *testing.T, groups []string) string {
		t.Helper()
		tok, err := sm.Mint(&auth.Claims{
			Subject: "user-role",
			Email:   "role@example.com",
			OrgID:   "org-1",
			Groups:  groups,
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		return tok
	}

	runCase := func(t *testing.T, groups []string, allowed []string, want int) {
		t.Helper()
		tok := mintFor(t, groups)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/role-check", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		verifier(makeHandler(allowed...)).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("groups=%v allowed=%v: got %d, want %d (body=%s)", groups, allowed, rec.Code, want, rec.Body.String())
		}
	}

	t.Run("admin allowed when admin is in list", func(t *testing.T) {
		runCase(t, []string{"oap-admins"}, []string{auth.RoleAdmin}, http.StatusOK)
	})
	t.Run("admin denied when only viewer is allowed", func(t *testing.T) {
		runCase(t, []string{"oap-admins"}, []string{auth.RoleViewer}, http.StatusForbidden)
	})
	t.Run("technician allowed in multi-role list", func(t *testing.T) {
		runCase(t, []string{"oap-technicians"}, []string{auth.RoleAdmin, auth.RoleTechnician}, http.StatusOK)
	})
	t.Run("operator denied when none of the allowed roles match", func(t *testing.T) {
		runCase(t, []string{"oap-operators"}, []string{auth.RoleAdmin, auth.RoleTechnician}, http.StatusForbidden)
	})
}
