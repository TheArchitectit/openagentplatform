package cloud

import (
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
