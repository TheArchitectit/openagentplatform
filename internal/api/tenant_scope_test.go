package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakeAgentStore implements the agentStore interface for tenant-guard
// tests. GetAgent is behavioural; every other method panics via the
// embedded nil interface if a test accidentally hits it.
type fakeAgentStore struct {
	agentStore

	agents map[string]string // agent id -> org id

	// lastResultsOrgID records the org filter passed to
	// ListCheckResultsPaged so tests can assert scoping.
	lastResultsOrgID string
}

func (f *fakeAgentStore) GetAgent(_ context.Context, orgID, id string) (*models.Agent, error) {
	agentOrg, ok := f.agents[id]
	if !ok || (orgID != "" && agentOrg != orgID) {
		return nil, ErrAgentNotFound
	}
	return &models.Agent{ID: id, OrgID: agentOrg}, nil
}

func (f *fakeAgentStore) ListCheckResultsPaged(_ context.Context, orgID, _, _, _, _ string, _, _ int) ([]models.CheckResult, int, error) {
	f.lastResultsOrgID = orgID
	return []models.CheckResult{}, 0, nil
}

// fakeScriptStore implements the scriptStore subset used by run tests.
type fakeScriptStore struct {
	scriptStore

	script *models.ScriptDefinition
	runs   []*models.ScriptRun
}

func (f *fakeScriptStore) GetScript(_ context.Context, _, _ string) (*models.ScriptDefinition, error) {
	return f.script, nil
}

func (f *fakeScriptStore) InsertScriptRun(_ context.Context, run *models.ScriptRun) error {
	f.runs = append(f.runs, run)
	return nil
}

// newTestPool returns a non-nil pool that is never dialed: pgxpool.New
// creates the pool lazily, and the fakes below never touch it. It only
// satisfies the db-unavailable guards in handlers under test.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://oap:oap@127.0.0.1:1/oap_test")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newTenantTestServer wires a Server with fake stores and a real session
// minter so requests flow through the production auth middleware.
func newTenantTestServer(t *testing.T, agents *fakeAgentStore, scripts *fakeScriptStore) (*Server, *auth.SessionMinter) {
	t.Helper()
	s := NewServer(&config.Config{}, newDiscardLogger(), newTestPool(t), nil, nil)
	s.setAgentStore(agents)
	if scripts != nil {
		s.SetScriptStore(scripts)
	}
	sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
	if err != nil {
		t.Fatalf("NewSessionMinter: %v", err)
	}
	s.sessionMinter = sm
	return s, sm
}

// doWithAuth runs the request through the real verifier + org-context
// middleware chain so handlers see the same context they would in
// production, then hands off to next (usually a chi router).
func doWithAuth(t *testing.T, sm *auth.SessionMinter, orgID, method, target, body string, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	tok, err := sm.Mint(&auth.Claims{
		Subject: "user-1",
		Email:   "user1@example.com",
		OrgID:   orgID,
		Role:    auth.RoleTechnician,
	})
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	stack := auth.VerifierMiddleware(sm, nil, "oap_session")(orgContextMiddleware(next))
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)
	return rec
}

// TestFilterAgentsInOrg rejects agents that do not belong to the caller's
// org and fails closed when org context is missing entirely.
func TestFilterAgentsInOrg(t *testing.T) {
	agents := &fakeAgentStore{agents: map[string]string{
		"agent-A1": "org-A",
		"agent-A2": "org-A",
		"agent-B1": "org-B", // foreign org
	}}
	s := NewServer(&config.Config{}, newDiscardLogger(), nil, nil, nil)
	s.setAgentStore(agents)
	ctx := context.Background()

	allowed, rejected := s.filterAgentsInOrg(ctx, "org-A", []string{"agent-A1", "agent-B1", "missing"})
	if len(allowed) != 1 || allowed[0] != "agent-A1" {
		t.Errorf("allowed = %v, want [agent-A1]", allowed)
	}
	if len(rejected) != 2 {
		t.Errorf("rejected = %v, want 2 entries (foreign + missing)", rejected)
	}

	// Fail closed: without org context every agent is rejected, even
	// ones that exist.
	allowed, rejected = s.filterAgentsInOrg(ctx, "", []string{"agent-A1", "agent-B1"})
	if len(allowed) != 0 || len(rejected) != 2 {
		t.Errorf("empty org: allowed = %v, rejected = %v; want everything rejected", allowed, rejected)
	}
}

// scriptRunRouter mounts the real run-script route so the chi URL param
// is populated exactly as in production.
func scriptRunRouter(s *Server) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v1/scripts/{id}/run", s.handleRunScript)
	return r
}

// TestRunScriptRejectsForeignAgents verifies that handleRunScript only
// enqueues runs for agents in the caller's org, returns the rejected IDs,
// and refuses outright when no valid agents remain.
func TestRunScriptRejectsForeignAgents(t *testing.T) {
	agents := &fakeAgentStore{agents: map[string]string{
		"agent-A1": "org-A",
		"agent-B1": "org-B",
	}}
	scripts := &fakeScriptStore{script: &models.ScriptDefinition{
		ID:      "script-1",
		OrgID:   "org-A",
		Enabled: true,
	}}
	s, sm := newTenantTestServer(t, agents, scripts)
	router := scriptRunRouter(s)

	rec := doWithAuth(t, sm, "org-A", http.MethodPost,
		"/api/v1/scripts/script-1/run", `{"agent_ids":["agent-A1","agent-B1"]}`, router)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RunIDs         []string `json:"run_ids"`
		RejectedAgents []string `json:"rejected_agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.RunIDs) != 1 {
		t.Errorf("run_ids = %v, want exactly the in-org agent's run", resp.RunIDs)
	}
	if len(resp.RejectedAgents) != 1 || resp.RejectedAgents[0] != "agent-B1" {
		t.Errorf("rejected_agents = %v, want [agent-B1]", resp.RejectedAgents)
	}
	if len(scripts.runs) != 1 || scripts.runs[0].AgentID != "agent-A1" {
		t.Errorf("persisted runs = %+v, want one run for agent-A1 only", scripts.runs)
	}

	// All-foreign targeting must be refused outright.
	rec = doWithAuth(t, sm, "org-A", http.MethodPost,
		"/api/v1/scripts/script-1/run", `{"agent_ids":["agent-B1"]}`, router)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("all-foreign run: expected 403, got %d", rec.Code)
	}
	if len(scripts.runs) != 1 {
		t.Errorf("foreign run inserted a row: %d runs persisted", len(scripts.runs))
	}
}

// TestListAllCheckResultsScopedToOrg verifies the org filter from the
// session claims reaches the store. orgContextMiddleware already 400s
// requests without an org, so the handler must never see an empty org in
// production; the fail-closed path in the store/handler is defense in
// depth covered by TestFilterAgentsInOrg.
func TestListAllCheckResultsScopedToOrg(t *testing.T) {
	agents := &fakeAgentStore{agents: map[string]string{}}
	s, sm := newTenantTestServer(t, agents, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/check-results", s.handleListAllCheckResults)

	doWithAuth(t, sm, "org-A", http.MethodGet, "/api/v1/check-results", "", router)
	if agents.lastResultsOrgID != "org-A" {
		t.Errorf("store received org filter %q, want org-A", agents.lastResultsOrgID)
	}
}
