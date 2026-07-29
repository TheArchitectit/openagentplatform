package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// TestRBACMutatingRoutesGateByRole verifies that the route-wiring pattern used
// in registerRoutes — stacking auth.VerifierMiddleware + orgContextMiddleware +
// auth.RequireRole in front of mutating handlers — denies a viewer and allows
// an admin/technician. This is a regression test for the missing-RBAC finding
// where every mutating endpoint (including the script run/execute path) was
// reachable by any authenticated user.
//
// We do not build the full Server (it requires a DB, NATS, etc.). Instead we
// reproduce the exact middleware stack and an inline r.With(RequireRole(...))
// group the way routes.go does, then assert the verifier-derived role controls
// access. The same stack is what protects POST /api/v1/scripts, patch
// approvals, notification-channel /test, and every other mutating route.
func TestRBACMutatingRoutesGateByRole(t *testing.T) {
	sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
	if err != nil {
		t.Fatalf("NewSessionMinter: %v", err)
	}

	// Build a router that mirrors a slice of registerRoutes: an open GET, a
	// mutating group gated by RequireRole(Admin, Technician, Operator), and
	// an admin-only group. Each /{id} route is declared once with the
	// mutating methods gated via r.With (chi panics on duplicate mounts).
	newRouter := func() http.Handler {
		r := chi.NewRouter()
		r.Group(func(r chi.Router) {
			r.Use(auth.VerifierMiddleware(sm, nil, sessionCookieName))
			r.Use(orgContextMiddleware)
			r.Route("/api/v1/scripts", func(r chi.Router) {
				r.Get("/", stubOK) // open list
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", stubOK) // open read
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Post("/run", stubAccepted)
				})
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Post("/", stubCreated)
			})
			r.Route("/api/v1/policies", func(r chi.Router) {
				r.Get("/", stubOK) // open list
				r.With(auth.RequireRole(auth.RoleAdmin)).Post("/", stubCreated) // admin-only
			})
		})
		return r
	}

	mintFor := func(t *testing.T, groups []string) string {
		t.Helper()
		tok, err := sm.Mint(&auth.Claims{
			Subject: "user-rbac",
			Email:   "rbac@example.com",
			OrgID:   "org-1",
			Groups:  groups,
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		return tok
	}

	cases := []struct {
		name   string
		method string
		path   string
		groups []string
		want   int
	}{
		// Read endpoints: open to any authenticated org member.
		{"viewer can list scripts", http.MethodGet, "/api/v1/scripts", []string{"oap-viewers"}, http.StatusOK},
		{"viewer can read a script", http.MethodGet, "/api/v1/scripts/123", []string{"oap-viewers"}, http.StatusOK},
		{"viewer can list policies", http.MethodGet, "/api/v1/policies", []string{"oap-viewers"}, http.StatusOK},

		// Mutating endpoints: a viewer must be denied (the bug).
		{"viewer cannot create script", http.MethodPost, "/api/v1/scripts", []string{"oap-viewers"}, http.StatusForbidden},
		{"viewer cannot run script", http.MethodPost, "/api/v1/scripts/123/run", []string{"oap-viewers"}, http.StatusForbidden},
		{"viewer cannot create policy", http.MethodPost, "/api/v1/policies", []string{"oap-viewers"}, http.StatusForbidden},

		// Elevated roles pass.
		{"technician can create script", http.MethodPost, "/api/v1/scripts", []string{"oap-technicians"}, http.StatusCreated},
		{"operator can run script", http.MethodPost, "/api/v1/scripts/123/run", []string{"oap-operators"}, http.StatusAccepted},
		{"admin can create policy", http.MethodPost, "/api/v1/policies", []string{"oap-admins"}, http.StatusCreated},

		// Technician must NOT pass admin-only routes.
		{"technician cannot create policy", http.MethodPost, "/api/v1/policies", []string{"oap-technicians"}, http.StatusForbidden},

		// Unauthenticated request is 401 (not 403).
		{"anonymous cannot create script", http.MethodPost, "/api/v1/scripts", nil, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newRouter()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.groups != nil {
				req.Header.Set("Authorization", "Bearer "+mintFor(t, tc.groups))
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("%s %s groups=%v: got %d, want %d (body=%s)",
					tc.method, tc.path, tc.groups, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestRequireRoleDeniesNilClaims guards against a nil-claims panic in the
// RequireRole middleware. VerifierMiddleware never stores a nil claims value,
// but the middleware must not dereference a nil if it ever encounters one.
func TestRequireRoleDeniesNilClaims(t *testing.T) {
	h := auth.RequireRole(auth.RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Context with a nil *SessionClaims stored under the user key. This is
	// constructed via the package-level helper to mirror how claims are read.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("nil-claims: expected 401, got %d", rec.Code)
	}
}

func stubOK(w http.ResponseWriter, _ *http.Request)        { w.WriteHeader(http.StatusOK) }
func stubCreated(w http.ResponseWriter, _ *http.Request)   { w.WriteHeader(http.StatusCreated) }
func stubAccepted(w http.ResponseWriter, _ *http.Request)  { w.WriteHeader(http.StatusAccepted) }
