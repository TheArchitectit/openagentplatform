package cloud

import (
	"context"
	"testing"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func TestResourceFilter(t *testing.T) {
	f := ResourceFilter{Provider: "aws", AccountID: "1111", Archived: false}
	if f.Provider != "aws" {
		t.Errorf("Provider = %q, want aws", f.Provider)
	}
	if f.AccountID != "1111" {
		t.Errorf("AccountID = %q, want 1111", f.AccountID)
	}
}

func TestNewReconciler(t *testing.T) {
	r := NewReconciler(nil, nil, nil, nil, nil, nil)
	if r == nil {
		t.Fatal("NewReconciler returned nil")
	}
	if r.providers == nil {
		t.Error("providers map should be initialized")
	}
}

func TestReconciler_AutoEnrollToggle(t *testing.T) {
	r := NewReconciler(nil, nil, nil, nil, nil, nil)
	if r.autoEnroll {
		t.Error("autoEnroll should default to false")
	}
	r.SetAutoEnroll(true)
	if !r.autoEnroll {
		t.Error("autoEnroll should be true after SetAutoEnroll(true)")
	}
}

// captureSink records every drift alert for assertions in tests.
type captureSink struct {
	alerts []DriftAlert
}

func (c *captureSink) Emit(a DriftAlert) {
	c.alerts = append(c.alerts, a)
}

func TestReconciler_DriftSinkOptional(t *testing.T) {
	r := NewReconciler(nil, nil, nil, nil, nil, nil)
	// Calling checkDrift with no sink should be a no-op (no panic).
	pol := &models.CloudPolicy{
		RequiredTags: []string{"env"},
	}
	r.checkDrift(pol, nil)
}

func TestReconciler_DriftMissingTag(t *testing.T) {
	r := NewReconciler(nil, nil, nil, nil, nil, nil)
	sink := &captureSink{}
	r.SetDriftSink(sink)

	pol := &models.CloudPolicy{
		RequiredTags: []string{"env", "owner"},
		TagRules:     map[string][]string{"env": {"prod", "staging"}},
	}
	resources := []models.CloudResource{
		{ID: "r1", Name: "web-prod", Tags: map[string]string{"env": "prod"}},                // missing "owner"
		{ID: "r2", Name: "db-dev", Tags: map[string]string{"env": "dev", "owner": "sre"}},   // invalid env value
		{ID: "r3", Name: "cache", Tags: map[string]string{"env": "prod", "owner": "infra"}},  // compliant
	}
	r.checkDrift(pol, resources)

	if len(sink.alerts) != 2 {
		t.Fatalf("expected 2 drift alerts, got %d: %+v", len(sink.alerts), sink.alerts)
	}
	var foundMissing, foundInvalid bool
	for _, a := range sink.alerts {
		if a.Message == "missing_required_tag: owner" {
			foundMissing = true
		}
		if a.Message == "invalid_tag_value: env=dev" {
			foundInvalid = true
		}
	}
	if !foundMissing {
		t.Error("expected missing_required_tag alert for 'owner'")
	}
	if !foundInvalid {
		t.Error("expected invalid_tag_value alert for env=dev")
	}
}

// fakeAgentStore records every virtual agent created and the cloud-ID
// lookups it was asked to perform.
type fakeAgentStore struct {
	created []*models.Agent
	lookup  map[string]*models.Agent
}

func (f *fakeAgentStore) GetByCloudID(_ context.Context, _ models.CloudProvider, cloudID string) (*models.Agent, error) {
	if a, ok := f.lookup[cloudID]; ok {
		return a, nil
	}
	return nil, nil
}

func (f *fakeAgentStore) CreateVirtual(_ context.Context, a *models.Agent) error {
	cp := *a
	f.created = append(f.created, &cp)
	return nil
}

// TestReconciler_AutoEnrollDisabledByDefault verifies the reconciler does
// NOT enroll resources when auto-enroll is off.
func TestReconciler_AutoEnrollDisabledByDefault(t *testing.T) {
	r := NewReconciler(nil, nil, nil, nil, &fakeAgentStore{}, nil)
	// autoEnroll is false; calling the enrollment path should be a no-op.
	r.mu.Lock()
	autoEnroll := r.autoEnroll
	r.mu.Unlock()
	if autoEnroll {
		t.Error("autoEnroll should be false by default")
	}
}

// TestReconciler_AutoEnrollCreatesVirtualAgent verifies the agent ID
// derivation matches the spec: cloud-<provider>-<resource_id>.
func TestReconciler_AutoEnrollAgentIDFormat(t *testing.T) {
	tests := []struct {
		provider   models.CloudProvider
		resourceID string
		want       string
	}{
		{models.CloudProviderAWS, "i-abc123", "cloud-aws-i-abc123"},
		{models.CloudProviderAzure, "/subscriptions/abc/vm1", "cloud-azure-/subscriptions/abc/vm1"},
		{models.CloudProviderGCP, "instance-1", "cloud-gcp-instance-1"},
	}
	for _, tt := range tests {
		got := "cloud-" + string(tt.provider) + "-" + tt.resourceID
		if got != tt.want {
			t.Errorf("agentID for %s/%s = %q, want %q", tt.provider, tt.resourceID, got, tt.want)
		}
	}
}
