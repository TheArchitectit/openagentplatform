package cloud

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// In-memory store fakes for end-to-end ReconcileOrg tests.
//
// These model real store semantics closely enough to catch idempotency
// violations: Upsert overwrites by resource_id, Archive is a transition
// (re-archiving an archived row is a no-op and is NOT counted), and
// CreateVirtual registers the agent for later GetByCloudID lookups.

type memAccountStore struct {
	mu       sync.Mutex
	accounts []*models.CloudAccount
}

func (m *memAccountStore) Create(_ context.Context, a *models.CloudAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *a
	m.accounts = append(m.accounts, &cp)
	return nil
}

func (m *memAccountStore) Get(_ context.Context, id string) (*models.CloudAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.accounts {
		if a.ID == id {
			cp := *a
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("account %q not found", id)
}

func (m *memAccountStore) ListByOrg(_ context.Context, orgID string) ([]*models.CloudAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.CloudAccount
	for _, a := range m.accounts {
		if a.OrgID == orgID {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memAccountStore) Delete(_ context.Context, id string) error { return nil }

type memResourceStore struct {
	mu           sync.Mutex
	resources    map[string]*models.CloudResource // keyed by ResourceID
	archiveCalls int
	upsertCalls  int
}

func newMemResourceStore() *memResourceStore {
	return &memResourceStore{resources: make(map[string]*models.CloudResource)}
}

func (m *memResourceStore) Upsert(_ context.Context, r *models.CloudResource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls++
	cp := *r
	if existing, ok := m.resources[r.ResourceID]; ok {
		cp.ID = existing.ID // stable row identity across upserts
	} else if cp.ID == "" {
		cp.ID = r.ResourceID
	}
	m.resources[r.ResourceID] = &cp
	return nil
}

func (m *memResourceStore) Get(_ context.Context, id string) (*models.CloudResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.resources[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, fmt.Errorf("resource %q not found", id)
}

func (m *memResourceStore) ListByOrg(_ context.Context, orgID string, f ResourceFilter) ([]*models.CloudResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.CloudResource
	for _, r := range m.resources {
		if r.OrgID != orgID {
			continue
		}
		if f.Provider != "" && r.Provider != models.CloudProvider(f.Provider) {
			continue
		}
		if f.AccountID != "" && r.AccountID != f.AccountID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memResourceStore) Archive(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.resources[id]
	if !ok {
		return fmt.Errorf("resource %q not found", id)
	}
	if r.ArchivedAt != nil {
		return nil // already archived; not a transition
	}
	now := time.Now()
	r.ArchivedAt = &now
	m.archiveCalls++
	return nil
}

func (m *memResourceStore) UpdateStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.resources[id]; ok {
		r.Status = status
	}
	return nil
}

func (m *memResourceStore) ListDrift(_ context.Context, orgID string) ([]*models.CloudResource, error) {
	return m.ListByOrg(context.Background(), orgID, ResourceFilter{})
}

type memPolicyStore struct {
	mu       sync.Mutex
	policies []*models.CloudPolicy
}

func (m *memPolicyStore) Create(_ context.Context, p *models.CloudPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.policies = append(m.policies, &cp)
	return nil
}

func (m *memPolicyStore) Get(_ context.Context, id string) (*models.CloudPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.policies {
		if p.ID == id {
			cp := *p
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("policy %q not found", id)
}

func (m *memPolicyStore) ListByOrg(_ context.Context, orgID string) ([]*models.CloudPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.CloudPolicy
	for _, p := range m.policies {
		if p.OrgID == orgID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memPolicyStore) Update(_ context.Context, p *models.CloudPolicy) error { return nil }
func (m *memPolicyStore) Delete(_ context.Context, id string) error             { return nil }

type memCostStore struct {
	mu        sync.Mutex
	snapshots []*models.CostSnapshot
}

func (m *memCostStore) Insert(_ context.Context, c *models.CostSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	m.snapshots = append(m.snapshots, &cp)
	return nil
}

func (m *memCostStore) GetLatest(_ context.Context, orgID, provider, accountID string) (*models.CostSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.snapshots) - 1; i >= 0; i-- {
		s := m.snapshots[i]
		if s.OrgID == orgID && string(s.Provider) == provider && s.AccountID == accountID {
			cp := *s
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("no snapshots for %s/%s/%s", orgID, provider, accountID)
}

func (m *memCostStore) ListByOrg(_ context.Context, orgID string) ([]*models.CostSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.CostSnapshot
	for _, s := range m.snapshots {
		if s.OrgID == orgID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

// fakeProvider is the in-memory ProviderClient fixture: it serves a
// caller-mutable resource inventory so tests can simulate drift between
// reconcile passes, and counts API calls.
type fakeProvider struct {
	mu        sync.Mutex
	provName  string
	resources []models.CloudResource
	cost      CostInfo
	listErr   error
	listCalls int
}

func (f *fakeProvider) Name() string { return f.provName }

func (f *fakeProvider) ListAccounts(_ context.Context, _ string) ([]CloudAccountInfo, error) {
	return nil, nil
}

func (f *fakeProvider) ListResources(_ context.Context, _, _, _ string) ([]models.CloudResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]models.CloudResource, len(f.resources))
	for i := range f.resources {
		out[i] = f.resources[i]
	}
	return out, nil
}

func (f *fakeProvider) GetCost(_ context.Context, _, _, period string) (CostInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ci := f.cost
	ci.BillingPeriod = period
	return ci, nil
}

func (f *fakeProvider) setResources(rs []models.CloudResource) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resources = rs
}

// recordingAgentStore wraps fakeAgentStore semantics: GetByCloudID finds
// agents it previously created (like a real store), so duplicate
// enrollment attempts within or across reconcile passes are visible.
type recordingAgentStore struct {
	mu      sync.Mutex
	lookup  map[string]*models.Agent
	created []*models.Agent
}

func newRecordingAgentStore() *recordingAgentStore {
	return &recordingAgentStore{lookup: make(map[string]*models.Agent)}
}

func (r *recordingAgentStore) GetByCloudID(_ context.Context, _ models.CloudProvider, cloudID string) (*models.Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.lookup[cloudID]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("agent for %q not found", cloudID)
}

func (r *recordingAgentStore) CreateVirtual(_ context.Context, a *models.Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *a
	r.created = append(r.created, &cp)
	r.lookup[cloudAgentKey(a)] = &cp
	return nil
}

// cloudAgentKey derives the cloud resource id from the reconciler's agent
// shape (ID "cloud-<provider>-<resourceID>", platform "cloud/<provider>"),
// mirroring how a real store would key GetByCloudID lookups.
func cloudAgentKey(a *models.Agent) string {
	provider := strings.TrimPrefix(a.Platform, "cloud/")
	return strings.TrimPrefix(a.ID, "cloud-"+provider+"-")
}

// driftFixture builds the canonical org/account/policy/provider triple
// used by the digest 09-03/09-04 tests: three resources — one compliant,
// one missing a required tag, one with an invalid tag value.
type driftFixture struct {
	accounts   *memAccountStore
	resources  *memResourceStore
	policies   *memPolicyStore
	costs      *memCostStore
	agents     *recordingAgentStore
	provider   *fakeProvider
	sink       *captureSink
	reconciler *Reconciler
}

func newDriftFixture(t *testing.T) *driftFixture {
	t.Helper()
	f := &driftFixture{
		accounts:  &memAccountStore{},
		resources: newMemResourceStore(),
		policies:  &memPolicyStore{},
		costs:     &memCostStore{},
		agents:    newRecordingAgentStore(),
		provider: &fakeProvider{
			provName: "aws",
			cost:     CostInfo{TotalCostUSD: 42.5, ServiceCosts: map[string]float64{"ec2": 30, "s3": 12.5}},
		},
		sink: &captureSink{},
	}
	f.provider.setResources([]models.CloudResource{
		{ResourceID: "i-ok", Provider: models.CloudProviderAWS, ResourceType: "instance", Name: "web-prod", Region: "us-east-1",
			Tags: map[string]string{"env": "prod", "owner": "sre"}},
		{ResourceID: "i-missing", Provider: models.CloudProviderAWS, ResourceType: "instance", Name: "api-01", Region: "us-east-1",
			Tags: map[string]string{"env": "prod"}}, // missing "owner"
		{ResourceID: "i-invalid", Provider: models.CloudProviderAWS, ResourceType: "instance", Name: "db-dev", Region: "us-east-1",
			Tags: map[string]string{"env": "dev", "owner": "sre"}}, // env not in allowlist
	})
	_ = f.accounts.Create(context.Background(), &models.CloudAccount{
		ID: "acct-1", OrgID: "org-1", Provider: models.CloudProviderAWS,
		AccountID: "111122223333", Enabled: true,
	})
	_ = f.policies.Create(context.Background(), &models.CloudPolicy{
		ID: "pol-1", OrgID: "org-1", Provider: models.CloudProviderAWS,
		AccountID: "111122223333", Enabled: true,
		RequiredTags: []string{"env", "owner"},
		TagRules:     map[string][]string{"env": {"prod", "staging"}},
	})
	f.reconciler = NewReconciler(f.accounts, f.resources, f.policies, f.costs, f.agents, testLogger())
	f.reconciler.RegisterProvider(f.provider)
	f.reconciler.SetDriftSink(f.sink)
	return f
}

// driftCount classifies captured alerts by message prefix.
func driftCount(sink *captureSink, prefix string) int {
	n := 0
	for _, a := range sink.alerts {
		if len(a.Message) >= len(prefix) && a.Message[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

// TestReconcileOrg_DriftFixture is the digest 09-03 drift fixture: a full
// ReconcileOrg pass over the canonical three-resource inventory must
// upsert every resource, emit exactly one missing-tag alert and one
// invalid-tag-value alert, and persist the cost snapshot.
func TestReconcileOrg_DriftFixture(t *testing.T) {
	f := newDriftFixture(t)

	if err := f.reconciler.ReconcileOrg(context.Background(), "org-1"); err != nil {
		t.Fatalf("ReconcileOrg: %v", err)
	}

	// All three resources landed in the store, stamped with org + account.
	for _, rid := range []string{"i-ok", "i-missing", "i-invalid"} {
		r, err := f.resources.Get(context.Background(), rid)
		if err != nil {
			t.Fatalf("resource %s not persisted: %v", rid, err)
		}
		if r.OrgID != "org-1" || r.AccountID != "111122223333" {
			t.Errorf("resource %s: org=%q account=%q", rid, r.OrgID, r.AccountID)
		}
	}

	// Drift: i-missing lacks "owner"; i-invalid has env=dev (allowlist is
	// prod|staging); i-ok is compliant and must not alert.
	if got := driftCount(f.sink, "missing_required_tag: owner"); got != 1 {
		t.Errorf("missing_required_tag alerts = %d, want 1", got)
	}
	if got := driftCount(f.sink, "invalid_tag_value: env=dev"); got != 1 {
		t.Errorf("invalid_tag_value alerts = %d, want 1", got)
	}
	if got := len(f.sink.alerts); got != 2 {
		t.Errorf("total drift alerts = %d, want 2 (compliant resource must not alert): %+v", got, f.sink.alerts)
	}

	// Cost snapshot captured.
	snaps, _ := f.costs.ListByOrg(context.Background(), "org-1")
	if len(snaps) != 1 {
		t.Fatalf("cost snapshots = %d, want 1", len(snaps))
	}
	if snaps[0].TotalCostUSD != 42.5 {
		t.Errorf("snapshot total = %v, want 42.5", snaps[0].TotalCostUSD)
	}
}

// TestReconcileOrg_ReconcileTwiceIdempotent is the digest 09-04 test:
// running the reconciler twice over an unchanged inventory must not
// duplicate resources, agents, or cost snapshots — and removing a
// resource from the provider must archive it exactly once, with later
// passes not re-archiving.
func TestReconcileOrg_ReconcileTwiceIdempotent(t *testing.T) {
	f := newDriftFixture(t)
	f.reconciler.SetAutoEnroll(true)
	ctx := context.Background()

	if err := f.reconciler.ReconcileOrg(ctx, "org-1"); err != nil {
		t.Fatalf("reconcile #1: %v", err)
	}
	// Second pass over the SAME inventory.
	if err := f.reconciler.ReconcileOrg(ctx, "org-1"); err != nil {
		t.Fatalf("reconcile #2: %v", err)
	}

	// Resources: still exactly three rows, no duplicates.
	if n := len(f.resources.resources); n != 3 {
		t.Errorf("resource rows after two passes = %d, want 3", n)
	}
	// Agents: exactly one virtual agent per resource — the second pass
	// must be a no-op.
	if n := len(f.agents.created); n != 3 {
		t.Errorf("virtual agents created = %d, want 3 (second pass must not re-enroll)", n)
	}
	// Cost snapshot: the second pass must not insert a second snapshot
	// for the same billing period.
	snaps, _ := f.costs.ListByOrg(ctx, "org-1")
	if n := len(snaps); n != 1 {
		t.Errorf("cost snapshots after two passes = %d, want 1 (re-run must be idempotent)", n)
	}

	// Drift between passes: i-missing disappears from the provider.
	f.provider.setResources([]models.CloudResource{
		{ResourceID: "i-ok", Provider: models.CloudProviderAWS, ResourceType: "instance", Name: "web-prod", Region: "us-east-1",
			Tags: map[string]string{"env": "prod", "owner": "sre"}},
		{ResourceID: "i-invalid", Provider: models.CloudProviderAWS, ResourceType: "instance", Name: "db-dev", Region: "us-east-1",
			Tags: map[string]string{"env": "dev", "owner": "sre"}},
	})
	if err := f.reconciler.ReconcileOrg(ctx, "org-1"); err != nil {
		t.Fatalf("reconcile #3 (post-drift): %v", err)
	}
	if f.resources.resources["i-missing"].ArchivedAt == nil {
		t.Error("disappeared resource i-missing was not archived")
	}
	if f.resources.archiveCalls != 1 {
		t.Errorf("archive calls = %d, want exactly 1", f.resources.archiveCalls)
	}

	// Fourth pass over the post-drift inventory: the archived row must
	// not be re-archived and counts must stay stable.
	if err := f.reconciler.ReconcileOrg(ctx, "org-1"); err != nil {
		t.Fatalf("reconcile #4: %v", err)
	}
	if f.resources.archiveCalls != 1 {
		t.Errorf("archive calls after fourth pass = %d, want 1 (no re-archiving)", f.resources.archiveCalls)
	}
	if n := len(f.agents.created); n != 3 {
		t.Errorf("virtual agents after fourth pass = %d, want 3", n)
	}
}
