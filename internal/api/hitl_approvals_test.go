package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// newHITLServer builds an API Server with a live (memstore-backed) HITL
// engine for handler tests.
func newHITLServer() (*Server, *hitl.ApprovalManager) {
	mgr := hitl.NewApprovalManager(hitl.DefaultApprovalTypes())
	mgr.SetStore(hitl.NewMemStore())
	return &Server{hitlManager: mgr}, mgr
}

// hitlRequest builds a request optionally carrying session claims.
func hitlRequest(method, target, body string, claims *auth.SessionClaims) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if claims != nil {
		r = r.WithContext(auth.WithUser(r.Context(), claims))
	}
	return r
}

func adminClaims() *auth.SessionClaims {
	return &auth.SessionClaims{Email: "ops@example.com", Role: auth.RoleAdmin}
}

// withID attaches a chi route context carrying the given id param.
func withID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// TestHITLNotConfigured — every handler returns 503 when the engine is unwired.
func TestHITLNotConfigured(t *testing.T) {
	s := &Server{}
	handlers := []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		method string
		body   string
		withID bool
	}{
		{"list", s.handleListApprovals, http.MethodGet, "", false},
		{"get", s.handleGetApproval, http.MethodGet, "", true},
		{"create", s.handleCreateApproval, http.MethodPost, `{"action_type":"patch_deploy","requester_agent_id":"a1"}`, false},
		{"approve", s.handleApproveApproval, http.MethodPost, "", true},
		{"reject", s.handleRejectApproval, http.MethodPost, `{"reason":"no"}`, true},
	}
	for _, h := range handlers {
		rec := httptest.NewRecorder()
		r := hitlRequest(h.method, "/approvals", h.body, adminClaims())
		if h.withID {
			r = withID(r, "x")
		}
		h.call(rec, r)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503, got %d", h.name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "hitl_not_configured") {
			t.Errorf("%s: expected hitl_not_configured, got %s", h.name, rec.Body.String())
		}
	}
}

// TestHITLCreateGetListApproveReject — the R1 happy path end to end.
func TestHITLCreateGetListApproveReject(t *testing.T) {
	s, _ := newHITLServer()

	// R1.1 create.
	rec := httptest.NewRecorder()
	s.handleCreateApproval(rec, hitlRequest(http.MethodPost, "/api/v1/a2a/approvals",
		`{"action_type":"patch_deploy","requester_agent_id":"agent-7","urgency":"high","payload":{"target":"ws-01"},"task_id":"t-1"}`, adminClaims()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created hitl.ApprovalRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create response: %v", err)
	}
	if created.ID == "" || created.Status != hitl.StatusPending || created.ActionType != "patch_deploy" {
		t.Fatalf("create response wrong: %+v", created)
	}
	if created.Urgency != "high" || created.TaskID != "t-1" || created.ExpiresAt.IsZero() {
		t.Errorf("create fields wrong: %+v", created)
	}

	// Default urgency when omitted.
	rec = httptest.NewRecorder()
	s.handleCreateApproval(rec, hitlRequest(http.MethodPost, "/api/v1/a2a/approvals",
		`{"action_type":"script_execute","requester_agent_id":"agent-8"}`, adminClaims()))
	var created2 hitl.ApprovalRequest
	_ = json.Unmarshal(rec.Body.Bytes(), &created2)
	if rec.Code != http.StatusCreated || created2.Urgency != "medium" {
		t.Errorf("default urgency: expected 201+medium, got %d+%s", rec.Code, created2.Urgency)
	}

	// R1.2 list pending (both items).
	rec = httptest.NewRecorder()
	s.handleListApprovals(rec, hitlRequest(http.MethodGet, "/api/v1/a2a/approvals", "", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var listOut struct {
		Approvals []hitl.ApprovalRequest `json:"approvals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil {
		t.Fatalf("list response: %v", err)
	}
	if len(listOut.Approvals) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(listOut.Approvals))
	}

	// Status filter + invalid filter.
	rec = httptest.NewRecorder()
	s.handleListApprovals(rec, hitlRequest(http.MethodGet, "/api/v1/a2a/approvals?status=approved", "", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &listOut); err != nil || len(listOut.Approvals) != 0 {
		t.Errorf("approved filter: expected empty list, got %v (err %v)", listOut.Approvals, err)
	}
	rec = httptest.NewRecorder()
	s.handleListApprovals(rec, hitlRequest(http.MethodGet, "/api/v1/a2a/approvals?status=bogus", "", nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_status") {
		t.Errorf("invalid status: expected 400 invalid_status, got %d %s", rec.Code, rec.Body.String())
	}

	// R1.3 get by id.
	rec = httptest.NewRecorder()
	s.handleGetApproval(rec, withID(hitlRequest(http.MethodGet, "/api/v1/a2a/approvals/"+created.ID, "", nil), created.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleGetApproval(rec, withID(hitlRequest(http.MethodGet, "/api/v1/a2a/approvals/none", "", nil), "no-such-id"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get missing: expected 404, got %d", rec.Code)
	}

	// R1.4 approve (empty body legal, actor recorded from claims).
	rec = httptest.NewRecorder()
	s.handleApproveApproval(rec, withID(hitlRequest(http.MethodPost, "/api/v1/a2a/approvals/"+created.ID+"/approve",
		`{"comment":"looks good"}`, adminClaims()), created.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var approved hitl.ApprovalRequest
	_ = json.Unmarshal(rec.Body.Bytes(), &approved)
	if approved.Status != hitl.StatusApproved || approved.DecidedBy != "ops@example.com" || approved.DecisionNote != "looks good" {
		t.Errorf("approve result wrong: %+v", approved)
	}
	// Second decision → 409.
	rec = httptest.NewRecorder()
	s.handleApproveApproval(rec, withID(hitlRequest(http.MethodPost, "/api/v1/a2a/approvals/"+created.ID+"/approve", "", adminClaims()), created.ID))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already_decided") {
		t.Errorf("double approve: expected 409 already_decided, got %d %s", rec.Code, rec.Body.String())
	}

	// R1.5 reject: missing reason → 400 first.
	rec = httptest.NewRecorder()
	s.handleRejectApproval(rec, withID(hitlRequest(http.MethodPost, "/api/v1/a2a/approvals/"+created2.ID+"/reject", `{}`, adminClaims()), created2.ID))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reason_required") {
		t.Errorf("reject no reason: expected 400 reason_required, got %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleRejectApproval(rec, withID(hitlRequest(http.MethodPost, "/api/v1/a2a/approvals/"+created2.ID+"/reject",
		`{"reason":"too risky"}`, adminClaims()), created2.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var rejected hitl.ApprovalRequest
	_ = json.Unmarshal(rec.Body.Bytes(), &rejected)
	if rejected.Status != hitl.StatusRejected || rejected.DecisionNote != "too risky" {
		t.Errorf("reject result wrong: %+v", rejected)
	}
}

// TestHITLCreateScopesOrg verifies the request's OrgID is taken from the
// authenticated user's claims so notification fan-out (R2.1) stays scoped.
func TestHITLCreateScopesOrg(t *testing.T) {
	s, mgr := newHITLServer()
	claims := &auth.SessionClaims{Email: "ops@example.com", Role: auth.RoleAdmin, OrgID: "org-9"}
	rec := httptest.NewRecorder()
	s.handleCreateApproval(rec, hitlRequest(http.MethodPost, "/api/v1/a2a/approvals",
		`{"action_type":"config_change","requester_agent_id":"agent-1"}`, claims))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created hitl.ApprovalRequest
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.OrgID != "org-9" {
		t.Errorf("OrgID = %q, want org-9", created.OrgID)
	}
	if got, _ := mgr.GetRequest(created.ID); got.OrgID != "org-9" {
		t.Errorf("stored OrgID = %q, want org-9", got.OrgID)
	}
}

// TestHITLCreateValidation — 400s for malformed create bodies.
func TestHITLCreateValidation(t *testing.T) {
	s, _ := newHITLServer()

	rec := httptest.NewRecorder()
	s.handleCreateApproval(rec, hitlRequest(http.MethodPost, "/api/v1/a2a/approvals", `{"action_type":"patch_deploy"}`, adminClaims()))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "action_type_and_requester_agent_id_required") {
		t.Errorf("missing requester: expected 400, got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleCreateApproval(rec, hitlRequest(http.MethodPost, "/api/v1/a2a/approvals", `{not json`, adminClaims()))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_json") {
		t.Errorf("bad json: expected 400 invalid_json, got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleCreateApproval(rec, hitlRequest(http.MethodPost, "/api/v1/a2a/approvals",
		`{"action_type":"not_a_real_type","requester_agent_id":"a"}`, adminClaims()))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown action type") {
		t.Errorf("unknown type: expected 400, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestHITLDecisionErrors — approve/reject on unknown ids → 404.
func TestHITLDecisionErrors(t *testing.T) {
	s, _ := newHITLServer()
	ghost := uuid.NewString()

	rec := httptest.NewRecorder()
	s.handleApproveApproval(rec, withID(hitlRequest(http.MethodPost, "/approve", "", adminClaims()), ghost))
	if rec.Code != http.StatusNotFound {
		t.Errorf("approve missing: expected 404, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleRejectApproval(rec, withID(hitlRequest(http.MethodPost, "/reject", `{"reason":"x"}`, adminClaims()), ghost))
	if rec.Code != http.StatusNotFound {
		t.Errorf("reject missing: expected 404, got %d", rec.Code)
	}
}

// TestHITLRouteMountAndRoleGate — registers the routes under a chi router
// exactly as routes_sub.go does and verifies mutating endpoints are
// admin/technician-gated while reads are open to any authenticated user.
func TestHITLRouteMountAndRoleGate(t *testing.T) {
	s, mgr := newHITLServer()
	// Seed one pending request through the engine directly.
	seed, err := mgr.CreateRequest("seed-1", "secret_access", "agent-x", "low", "", nil)
	if err != nil || seed == nil {
		t.Fatalf("seed create: %v", err)
	}

	// A fake auth middleware injects claims based on a test header,
	// simulating what VerifierMiddleware does in production.
	authed := chi.NewRouter()
	authed.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
			switch rq.Header.Get("X-Test-Role") {
			case "admin":
				rq = rq.WithContext(auth.WithUser(rq.Context(), &auth.SessionClaims{Email: "admin@x", Role: auth.RoleAdmin}))
			case "viewer":
				rq = rq.WithContext(auth.WithUser(rq.Context(), &auth.SessionClaims{Email: "viewer@x", Role: auth.RoleViewer}))
			}
			next.ServeHTTP(w, rq)
		})
	})
	RegisterHITLRoutes(authed, s)
	tsAuthed := httptest.NewServer(authed)
	defer tsAuthed.Close()

	// Viewer reads OK.
	resp, err := http.Get(tsAuthed.URL + "/approvals")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer GET: expected 200, got %d", resp.StatusCode)
	}

	// Viewer writes → 403.
	req, _ := http.NewRequest(http.MethodPost, tsAuthed.URL+"/approvals/seed-1/approve", strings.NewReader(`{}`))
	req.Header.Set("X-Test-Role", "viewer")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer approve: expected 403, got %d", resp.StatusCode)
	}

	// Admin write succeeds.
	req, _ = http.NewRequest(http.MethodPost, tsAuthed.URL+"/approvals/seed-1/approve", strings.NewReader(`{"comment":"ok"}`))
	req.Header.Set("X-Test-Role", "admin")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin approve: expected 200, got %d", resp.StatusCode)
	}
	if got, _ := mgr.GetRequest("seed-1"); got.Status != hitl.StatusApproved {
		t.Errorf("expected approved via router, got %s", got.Status)
	}

	// Unauthenticated write → 401.
	req, _ = http.NewRequest(http.MethodPost, tsAuthed.URL+"/approvals", strings.NewReader(`{"action_type":"policy_change","requester_agent_id":"a"}`))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon POST: expected 401, got %d", resp.StatusCode)
	}
}
