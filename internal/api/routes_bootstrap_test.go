package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/bootstrap"
	"github.com/openagentplatform/openagentplatform/internal/config"
)

// configWithBootstrap builds a minimal Config carrying only the bootstrap
// token — the handler reads nothing else from cfg.
func configWithBootstrap(token string) *config.Config {
	return &config.Config{BootstrapToken: token}
}

// openBootstrapDB returns a live *sql.DB against OAP_TEST_PG_DSN (schema at
// least 004), skipping when unset.
func openBootstrapDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("OAP_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OAP_TEST_PG_DSN not set; skipping live bootstrap handler tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Start uninitialized.
	for _, q := range []string{
		`DELETE FROM user_org_bindings`,
		`DELETE FROM app_state`,
		`DELETE FROM tenants WHERE name LIKE 'Handler Test %' OR slug LIKE 'ht-%'`,
	} {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("clean: %v", err)
		}
	}
	return db
}

// bootstrapRouter mirrors how registerRoutes mounts the endpoint: session
// verifier, but NO orgContextMiddleware (spec §14.1).
func bootstrapRouter(t *testing.T, s *Server, sm *auth.SessionMinter) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Use(auth.VerifierMiddleware(sm, nil, sessionCookieName))
		r.Post("/bootstrap", s.handleBootstrap)
	})
	return r
}

func postBootstrap(t *testing.T, h http.Handler, token, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"token":"` + token + `","org_name":"Handler Test Org","org_slug":"ht-org"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newSessionMinter(t *testing.T) *auth.SessionMinter {
	t.Helper()
	sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
	if err != nil {
		t.Fatalf("NewSessionMinter: %v", err)
	}
	return sm
}

func sessionFor(t *testing.T, sm *auth.SessionMinter, c *auth.Claims) string {
	t.Helper()
	tok, err := sm.Mint(c)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	return tok
}

// TestBootstrapHandler_NoSessionIs401 — the verifier rejects anonymous callers.
func TestBootstrapHandler_NoSessionIs401(t *testing.T) {
	sm := newSessionMinter(t)
	s := &Server{log: slog.Default(), cfg: configWithBootstrap("t0ken")}
	rec := postBootstrap(t, bootstrapRouter(t, s, sm), "t0ken", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d, want 401", rec.Code)
	}
}

// TestBootstrapHandler_DisabledWhenNoToken — cfg.BootstrapToken empty → 404.
func TestBootstrapHandler_DisabledWhenNoToken(t *testing.T) {
	sm := newSessionMinter(t)
	s := &Server{log: slog.Default(), cfg: configWithBootstrap("")}
	rec := postBootstrap(t, bootstrapRouter(t, s, sm), "whatever",
		sessionFor(t, sm, &auth.Claims{Subject: "sub-a", Email: "a@example.com"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled: got %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bootstrap_disabled") {
		t.Fatalf("body = %s, want bootstrap_disabled", rec.Body.String())
	}
}

// TestBootstrapHandler_StoreUnavailableIs503 — wired but no DB handle.
func TestBootstrapHandler_StoreUnavailableIs503(t *testing.T) {
	sm := newSessionMinter(t)
	s := &Server{log: slog.Default(), cfg: configWithBootstrap("t0ken")}
	rec := postBootstrap(t, bootstrapRouter(t, s, sm), "t0ken",
		sessionFor(t, sm, &auth.Claims{Subject: "sub-b", Email: "b@example.com"}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no store: got %d, want 503", rec.Code)
	}
}

// TestBootstrapHandler_FullFlowLiveDB — happy path + wrong token + second
// claim, against the real schema (004 tables present).
func TestBootstrapHandler_FullFlowLiveDB(t *testing.T) {
	db := openBootstrapDB(t)
	sm := newSessionMinter(t)
	s := &Server{log: slog.Default(), cfg: configWithBootstrap("s3cret"), bootstrapStore: bootstrap.NewStore(db)}
	h := bootstrapRouter(t, s, sm)
	cookie := sessionFor(t, sm, &auth.Claims{Subject: "sub-owner", Email: "owner@example.com", Groups: []string{"oap-admins"}})

	// Wrong token → 401, nothing created.
	if rec := postBootstrap(t, h, "nope", cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d (%s), want 401", rec.Code, rec.Body.String())
	}

	// Correct token → 201 with org_id + slug.
	rec := postBootstrap(t, h, "s3cret", cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("claim: got %d (%s), want 201", rec.Code, rec.Body.String())
	}
	var out struct {
		Status  string `json:"status"`
		OrgID   string `json:"org_id"`
		OrgSlug string `json:"org_slug"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != "initialized" || out.OrgID == "" || out.OrgSlug != "ht-org" {
		t.Fatalf("response = %+v", out)
	}

	// Second caller is locked out permanently → 403.
	cookie2 := sessionFor(t, sm, &auth.Claims{Subject: "sub-late", Email: "late@example.com"})
	if rec := postBootstrap(t, h, "s3cret", cookie2); rec.Code != http.StatusForbidden {
		t.Fatalf("late claim: got %d, want 403", rec.Code)
	}

	// Login-time resolution: the owner's session now resolves to the org.
	bctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	claims := &auth.Claims{Subject: "sub-owner", Email: "owner@example.com"}
	orgID, role := s.resolveOrgForSubject(bctx, claims.Subject, claims)
	if orgID != out.OrgID {
		t.Fatalf("resolveOrgForSubject owner = %q, want %q", orgID, out.OrgID)
	}
	if role != "admin" {
		t.Fatalf("owner role = %q, want admin", role)
	}
	// An unbound subject in a single-org DB auto-binds (spec §14.4c).
	lateClaims := &auth.Claims{Subject: "sub-late", Email: "late@example.com", Groups: []string{"oap-viewers"}}
	lateOrg, lateRole := s.resolveOrgForSubject(bctx, lateClaims.Subject, lateClaims)
	if lateOrg != out.OrgID || lateRole != "" {
		t.Fatalf("auto-bind late = (%q,%q), want (%q,\"\")", lateOrg, lateRole, out.OrgID)
	}
	// After auto-bind, the binding row carries the mapped role (viewer).
	if _, role, err := s.bootstrapStore.Binding(bctx, "sub-late"); err != nil || role != "viewer" {
		t.Fatalf("late binding role = %q (err %v), want viewer", role, err)
	}

	// ID-token org claim passes through untouched (§14.4a).
	if orgID, role := s.resolveOrgForSubject(bctx, "irrelevant", &auth.Claims{Subject: "x", OrgID: "idp-org"}); orgID != "idp-org" || role != "" {
		t.Fatalf("passthrough = (%q,%q), want (idp-org,\"\")", orgID, role)
	}
}

func TestBootstrapHandler_InvalidJSONIs400(t *testing.T) {
	db := openBootstrapDB(t) // store required: nil store short-circuits to 503 first
	sm := newSessionMinter(t)
	s := &Server{log: slog.Default(), cfg: configWithBootstrap("t0ken"), bootstrapStore: bootstrap.NewStore(db)}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", io.LimitReader(strings.NewReader("{not json"), 9))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionFor(t, sm, &auth.Claims{Subject: "sub-c", Email: "c@example.com"})})
	rec := httptest.NewRecorder()
	bootstrapRouter(t, s, sm).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rec.Code)
	}
}
