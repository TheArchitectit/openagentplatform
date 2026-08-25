package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/mesh"
)

// testMeshReleaseStore is an in-memory stub of MeshReleaseStore for tests.
type testMeshReleaseStore struct {
	inserted   *mesh.AgentRelease
	insertErr  error
	releases   []*mesh.AgentRelease
	listErr    error
	pinnedVer  string
	pinnedFlag bool
	pinErr     error
}

func (s *testMeshReleaseStore) InsertAgentRelease(_ context.Context, _ string, version, platform, sha256, signature string, pinned bool) (*mesh.AgentRelease, error) {
	if s.insertErr != nil {
		return nil, s.insertErr
	}
	s.inserted = &mesh.AgentRelease{
		OrgID:        "org-1",
		Version:      version,
		Platform:     platform,
		BinarySHA256: sha256,
		Signature:    signature,
		Pinned:       pinned,
	}
	return s.inserted, nil
}

func (s *testMeshReleaseStore) ListAgentReleases(_ context.Context, _ string, _ bool) ([]*mesh.AgentRelease, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.releases, nil
}

func (s *testMeshReleaseStore) PinAgentRelease(_ context.Context, _ string, version string, pinned bool) error {
	if s.pinErr != nil {
		return s.pinErr
	}
	s.pinnedVer = version
	s.pinnedFlag = pinned
	return nil
}

func newReleasesTestServer(store MeshReleaseStore) *Server {
	return &Server{meshReleaseStore: store}
}

func adminCtx() context.Context {
	return auth.WithUser(context.Background(), &auth.SessionClaims{
		OrgID:            "org-1",
		Role:             auth.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-1"},
	})
}

func TestHandleMeshReleaseCreate_Success(t *testing.T) {
	store := &testMeshReleaseStore{}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).
		Post("/agents/{id}/releases", srv.handleMeshReleaseCreate)

	body, _ := json.Marshal(map[string]any{
		"version":   "2.0.0",
		"platform":  "linux/amd64",
		"sha256":    "abc123",
		"signature": "sig-base64",
		"pinned":    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases", bytes.NewReader(body))
	req = req.WithContext(adminCtx())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if store.inserted == nil {
		t.Fatal("InsertAgentRelease not called")
	}
	if store.inserted.Version != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", store.inserted.Version)
	}
}

func TestHandleMeshReleaseCreate_RBAC(t *testing.T) {
	store := &testMeshReleaseStore{}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).
		Post("/agents/{id}/releases", srv.handleMeshReleaseCreate)

	body, _ := json.Marshal(map[string]string{"version": "2.0.0", "platform": "linux", "sha256": "a", "signature": "s"})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases", bytes.NewReader(body))
	// Viewer role should be rejected at the RBAC middleware layer.
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{
		OrgID:            "org-1",
		Role:             auth.RoleViewer,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-1"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestHandleMeshReleaseCreate_BadRequest(t *testing.T) {
	store := &testMeshReleaseStore{}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).
		Post("/agents/{id}/releases", srv.handleMeshReleaseCreate)

	// Missing required fields.
	body, _ := json.Marshal(map[string]string{"version": "2.0.0"})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases", bytes.NewReader(body))
	req = req.WithContext(adminCtx())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing fields", w.Code)
	}
}

func TestHandleMeshReleaseList_Success(t *testing.T) {
	store := &testMeshReleaseStore{releases: []*mesh.AgentRelease{
		{Version: "2.0.0", Platform: "linux/amd64", Pinned: true},
	}}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).
		Get("/agents/{id}/releases", srv.handleMeshReleaseList)

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-1/releases", nil)
	req = req.WithContext(adminCtx())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out []*mesh.AgentRelease
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d releases, want 1", len(out))
	}
}

func TestHandleMeshReleasePin_Success(t *testing.T) {
	store := &testMeshReleaseStore{}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin)).
		Post("/agents/{id}/releases/{version}/pin", srv.handleMeshReleasePin)

	body, _ := json.Marshal(map[string]bool{"pinned": true})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases/2.0.0/pin", bytes.NewReader(body))
	req = req.WithContext(adminCtx())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if store.pinnedVer != "2.0.0" || !store.pinnedFlag {
		t.Errorf("pin not recorded: ver=%q pinned=%v", store.pinnedVer, store.pinnedFlag)
	}
}

func TestHandleMeshReleasePin_AdminOnly(t *testing.T) {
	store := &testMeshReleaseStore{}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin)).
		Post("/agents/{id}/releases/{version}/pin", srv.handleMeshReleasePin)

	body, _ := json.Marshal(map[string]bool{"pinned": true})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases/2.0.0/pin", bytes.NewReader(body))
	// Technician should be rejected — pinning is admin-only.
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{
		OrgID:            "org-1",
		Role:             auth.RoleTechnician,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-1"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for technician", w.Code)
	}
}

func TestHandleMeshReleasePin_NotFound(t *testing.T) {
	store := &testMeshReleaseStore{pinErr: mesh.ErrReleaseNotFound}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin)).
		Post("/agents/{id}/releases/{version}/pin", srv.handleMeshReleasePin)

	body, _ := json.Marshal(map[string]bool{"pinned": true})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases/9.9.9/pin", bytes.NewReader(body))
	req = req.WithContext(adminCtx())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for missing release", w.Code)
	}
}

func TestHandleMeshReleasePin_NoAuth(t *testing.T) {
	store := &testMeshReleaseStore{}
	srv := newReleasesTestServer(store)
	r := chi.NewRouter()
	r.With(auth.RequireRole(auth.RoleAdmin)).
		Post("/agents/{id}/releases/{version}/pin", srv.handleMeshReleasePin)

	body, _ := json.Marshal(map[string]bool{"pinned": true})
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-1/releases/2.0.0/pin", bytes.NewReader(body))
	// No auth context.

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// Ensure errors.Is works for the store error returned in handlers.
func TestReleaseStoreErrorIs(t *testing.T) {
	if !errors.Is(mesh.ErrReleaseNotFound, mesh.ErrReleaseNotFound) {
		t.Fatal("sanity")
	}
}
