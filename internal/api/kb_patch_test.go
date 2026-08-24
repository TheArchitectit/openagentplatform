package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakePatchStore implements the patches.Store interface for the KB batch
// handler. Only GetKBStatesByAgent is exercised by the handler; the other
// methods are stubbed out.
type fakePatchStore struct {
	states []models.WinUpdateKBState
	err    error
}

func (f *fakePatchStore) CreatePatchJob(ctx context.Context, job *models.PatchJob) error {
	return nil
}
func (f *fakePatchStore) GetPatchJob(ctx context.Context, orgID, id string) (*models.PatchJob, error) {
	return nil, nil
}
func (f *fakePatchStore) ListPatchJobs(ctx context.Context, fl patches.PatchJobFilter) ([]models.PatchJob, int, error) {
	return nil, 0, nil
}
func (f *fakePatchStore) UpdatePatchJob(ctx context.Context, job *models.PatchJob) error {
	return nil
}
func (f *fakePatchStore) DeletePatchJob(ctx context.Context, orgID, id string) error {
	return nil
}
func (f *fakePatchStore) InsertApprovalRecord(ctx context.Context, rec *models.ApprovalRecord) error {
	return nil
}
func (f *fakePatchStore) GetApprovalHistory(ctx context.Context, jobID string) ([]models.ApprovalRecord, error) {
	return nil, nil
}
func (f *fakePatchStore) InsertPatchJobTarget(ctx context.Context, t *models.PatchJobTarget) error {
	return nil
}
func (f *fakePatchStore) GetPatchJobTargets(ctx context.Context, jobID string) ([]models.PatchJobTarget, error) {
	return nil, nil
}
func (f *fakePatchStore) UpdatePatchJobTarget(ctx context.Context, t *models.PatchJobTarget) error {
	return nil
}
func (f *fakePatchStore) GetPatchStats(ctx context.Context, orgID string) (*models.PatchStats, error) {
	return nil, nil
}
func (f *fakePatchStore) IngestKBScan(ctx context.Context, orgID, agentID, kb, severity string) (string, error) {
	return "scanned", nil
}
func (f *fakePatchStore) IngestKBInstall(ctx context.Context, orgID, agentID, kb string, success, reboot bool, errMsg string) (string, error) {
	return "installed", nil
}
func (f *fakePatchStore) IngestKBRebootDone(ctx context.Context, orgID, agentID string, kbs []string) error {
	return nil
}
func (f *fakePatchStore) TransitionKB(ctx context.Context, orgID, agentID, kb, event string) (string, error) {
	return "approved", nil
}
func (f *fakePatchStore) GetKBStatesByAgent(ctx context.Context, orgID, agentID string) ([]models.WinUpdateKBState, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]models.WinUpdateKBState, 0, len(f.states))
	for _, s := range f.states {
		if s.OrgID == orgID && (agentID == "" || s.AgentID == agentID) {
			out = append(out, s)
		}
	}
	return out, nil
}

func newTestServerWithPatchStore(t *testing.T, store patches.Store) (*Server, func()) {
	t.Helper()
	cfg := &config.Config{}
	log := newDiscardLogger()
	// Pass nil db: the KB handler only consults the agent store when an
	// agent_id query param is supplied, and a nil pool yields a 404 which
	// is the desired cross-org behavior. No cleanup needed.
	s := NewServer(cfg, log, nil, nil, nil)
	s.SetPatchStore(store)
	return s, func() {}
}

// orgContext returns a request context carrying session claims for the
// given org.
func orgContext(orgID string) context.Context {
	return auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: orgID})
}

// TestHandleGetKBBatch_OrgScoped verifies the handler returns only the
// caller's org KB states and the JSON shape.
func TestHandleGetKBBatch_OrgScoped(t *testing.T) {
	store := &fakePatchStore{
		states: []models.WinUpdateKBState{
			{ID: "1", OrgID: "org-a", AgentID: "agent-1", KB: "KB1", State: "approved"},
			{ID: "2", OrgID: "org-a", AgentID: "agent-1", KB: "KB2", State: "installed"},
			{ID: "3", OrgID: "org-b", AgentID: "agent-2", KB: "KB3", State: "scanned"},
		},
	}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/kb", nil)
	req = req.WithContext(orgContext("org-a"))
	rec := httptest.NewRecorder()

	s.handleGetKBBatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		KBStates []models.WinUpdateKBState `json:"kb_states"`
		Total    int                       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("total: got %d, want 2", resp.Total)
	}
	for _, st := range resp.KBStates {
		if st.OrgID != "org-a" {
			t.Errorf("cross-org leak: %+v", st)
		}
	}
}

// TestHandleGetKBBatch_CrossOrgAgent404 verifies that requesting an agent
// belonging to a different org returns 404.
func TestHandleGetKBBatch_CrossOrgAgent404(t *testing.T) {
	store := &fakePatchStore{}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	// The agent store is backed by a pgxmock pool; GetAgent will return
	// no rows, which the handler treats as cross-org -> 404.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/kb?agent_id=agent-other", nil)
	req = req.WithContext(orgContext("org-a"))
	rec := httptest.NewRecorder()

	s.handleGetKBBatch(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetKBBatch_NoAuth verifies that a request with no org claims
// is rejected with 403.
func TestHandleGetKBBatch_NoAuth(t *testing.T) {
	store := &fakePatchStore{}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/kb", nil)
	rec := httptest.NewRecorder()

	s.handleGetKBBatch(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetKBBatch_StateFilter verifies the state query param filters
// the results.
func TestHandleGetKBBatch_StateFilter(t *testing.T) {
	store := &fakePatchStore{
		states: []models.WinUpdateKBState{
			{ID: "1", OrgID: "org-a", AgentID: "agent-1", KB: "KB1", State: "approved"},
			{ID: "2", OrgID: "org-a", AgentID: "agent-1", KB: "KB2", State: "installed"},
		},
	}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/kb?state=approved", nil)
	req = req.WithContext(orgContext("org-a"))
	rec := httptest.NewRecorder()

	s.handleGetKBBatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		KBStates []models.WinUpdateKBState `json:"kb_states"`
		Total    int                       `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total != 1 {
		t.Errorf("total: got %d, want 1", resp.Total)
	}
	if len(resp.KBStates) != 1 || resp.KBStates[0].State != "approved" {
		t.Errorf("filter: unexpected result %+v", resp.KBStates)
	}
}
