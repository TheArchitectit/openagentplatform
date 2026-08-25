package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/mesh"
)

// testMeshAdmission is a minimal stub that records calls for assertion.
type testMeshAdmission struct {
	lastOrgID      string
	lastOperatorID string
	lastAgentID    string
	lastPurpose    string
	grant          *mesh.SessionGrant
	err            error
	sessions       []*mesh.MeshSession
	closeErr       error
}

func (t *testMeshAdmission) RequestSession(_ context.Context, orgID, operatorID, agentID, purpose string) (*mesh.SessionGrant, error) {
	t.lastOrgID = orgID
	t.lastOperatorID = operatorID
	t.lastAgentID = agentID
	t.lastPurpose = purpose
	return t.grant, t.err
}

func (t *testMeshAdmission) CloseSession(_ context.Context, orgID, sessionID string) error {
	t.lastOrgID = orgID
	return t.closeErr
}

func (t *testMeshAdmission) ListSessions(_ context.Context, orgID, operatorID string) ([]*mesh.MeshSession, error) {
	return t.sessions, nil
}

func newMeshTestServer(adm *testMeshAdmission) *Server {
	return &Server{
		log:           nil,
		meshAdmission: adm,
	}
}

func TestHandleMeshSessionCreate_Success(t *testing.T) {
	adm := &testMeshAdmission{
		grant: &mesh.SessionGrant{
			SessionID:               "sess-1",
			AgentID:                 "agent-1",
			AgentMeshIP:             "10.0.0.3/32",
			OperatorMeshIP:          "10.0.0.100/32",
			OperatorPrivateKeyWGB64: "privkeybase64==",
			AgentPublicKeyWGB64:     "pubkeybase64==",
			SSHCertPEM:              "cert-content",
			SSHCAPublicKeyPEM:       "ca-pub",
		},
	}
	srv := newMeshTestServer(adm)
	r := chi.NewRouter()
	r.Post("/mesh/session", srv.handleMeshSessionCreate)

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1", "purpose": "vnc"})
	req := httptest.NewRequest(http.MethodPost, "/mesh/session", bytes.NewReader(body))
	// Inject authenticated user — org comes from SESSION, not body.
	req = req.WithContext(auth.WithUser(req.Context(), &auth.SessionClaims{
		OrgID:  "org-from-session",
		Role:   auth.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-42"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Verify org-scoping: org MUST come from the session, never from the body.
	if adm.lastOrgID != "org-from-session" {
		t.Errorf("org_id = %q, want %q (from session, not body)", adm.lastOrgID, "org-from-session")
	}
	if adm.lastOperatorID != "op-42" {
		t.Errorf("operator_id = %q, want %q", adm.lastOperatorID, "op-42")
	}
	if adm.lastAgentID != "agent-1" {
		t.Errorf("agent_id = %q, want agent-1", adm.lastAgentID)
	}
	if adm.lastPurpose != "vnc" {
		t.Errorf("purpose = %q, want vnc", adm.lastPurpose)
	}
	// Verify the grant is returned.
	var grant mesh.SessionGrant
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if grant.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want sess-1", grant.SessionID)
	}
}

func TestHandleMeshSessionCreate_RBAC(t *testing.T) {
	adm := &testMeshAdmission{}
	srv := newMeshTestServer(adm)
	r := chi.NewRouter()
	r.Post("/mesh/session", srv.handleMeshSessionCreate)

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1", "purpose": "vnc"})
	req := httptest.NewRequest(http.MethodPost, "/mesh/session", bytes.NewReader(body))
	// Viewer role should be rejected.
	req = req.WithContext(auth.WithUser(req.Context(), &auth.SessionClaims{
		OrgID: "org-1",
		Role:  auth.RoleViewer,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-99"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for viewer role", w.Code)
	}
}

func TestHandleMeshSessionCreate_NoAuth(t *testing.T) {
	adm := &testMeshAdmission{}
	srv := newMeshTestServer(adm)
	r := chi.NewRouter()
	r.Post("/mesh/session", srv.handleMeshSessionCreate)

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1", "purpose": "vnc"})
	req := httptest.NewRequest(http.MethodPost, "/mesh/session", bytes.NewReader(body))
	// No user in context.

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for no auth", w.Code)
	}
}

func TestHandleMeshSessionList(t *testing.T) {
	adm := &testMeshAdmission{
		sessions: []*mesh.MeshSession{
			{SessionID: "s1", OrgID: "org-1", OperatorID: "op-1"},
		},
	}
	srv := newMeshTestServer(adm)
	r := chi.NewRouter()
	r.Get("/mesh/session", srv.handleMeshSessionList)

	req := httptest.NewRequest(http.MethodGet, "/mesh/session", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.SessionClaims{
		OrgID: "org-1",
		Role:  auth.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-1"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleMeshSessionClose(t *testing.T) {
	adm := &testMeshAdmission{}
	srv := newMeshTestServer(adm)
	r := chi.NewRouter()
	r.Post("/mesh/session/{id}/close", srv.handleMeshSessionClose)

	req := httptest.NewRequest(http.MethodPost, "/mesh/session/sess-1/close", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.SessionClaims{
		OrgID: "org-1",
		Role:  auth.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "op-1"},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if adm.lastOrgID != "org-1" {
		t.Errorf("close org_id = %q, want org-1", adm.lastOrgID)
	}
}
