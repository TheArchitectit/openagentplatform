package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakeAlertRuleStore implements the alert-rule subset of alerts.Store for
// tenant tests. It rejects updates/deletes whose org does not match the
// rule's owner, mirroring the SQL-level org predicate.
type fakeAlertRuleStore struct {
	alerts.Store

	rules map[string]string // rule id -> owning org

	updatedOrg string
	deletedOrg string
	created    *models.AlertRule
}

func (f *fakeAlertRuleStore) GetAlertRules(_ context.Context, orgID string) ([]models.AlertRule, error) {
	out := []models.AlertRule{}
	for id, org := range f.rules {
		if orgID == "" || org == orgID {
			out = append(out, models.AlertRule{ID: id, OrgID: org})
		}
	}
	return out, nil
}

func (f *fakeAlertRuleStore) CreateAlertRule(_ context.Context, r *models.AlertRule) error {
	f.created = r
	return nil
}

func (f *fakeAlertRuleStore) UpdateAlertRule(_ context.Context, orgID string, r *models.AlertRule) error {
	f.updatedOrg = orgID
	owner, ok := f.rules[r.ID]
	if orgID == "" || !ok || owner != orgID {
		return alerts.ErrAlertRuleNotFound
	}
	return nil
}

func (f *fakeAlertRuleStore) DeleteAlertRule(_ context.Context, orgID, id string) error {
	f.deletedOrg = orgID
	owner, ok := f.rules[id]
	if orgID == "" || !ok || owner != orgID {
		return alerts.ErrAlertRuleNotFound
	}
	return nil
}

func alertRulesRouter(s *Server) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v1/alert-rules", s.listAlertRules)
	r.Post("/api/v1/alert-rules", s.createAlertRule)
	r.Put("/api/v1/alert-rules/{id}", s.updateAlertRule)
	r.Delete("/api/v1/alert-rules/{id}", s.deleteAlertRule)
	return r
}

// TestAlertRulesTenantScope exercises the four rule endpoints against a
// two-org rule set and asserts that the caller's org (from the session
// claims) is the only scope that matters.
func TestAlertRulesTenantScope(t *testing.T) {
	store := &fakeAlertRuleStore{rules: map[string]string{
		"rule-A": "org-A",
		"rule-B": "org-B",
	}}
	s, sm := newTenantTestServer(t, &fakeAgentStore{agents: map[string]string{}}, nil)
	s.alertStore = store
	router := alertRulesRouter(s)

	t.Run("list ignores client org_id parameter", func(t *testing.T) {
		rec := doWithAuth(t, sm, "org-A", http.MethodGet, "/api/v1/alert-rules?org_id=org-B", "", router)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var rules []models.AlertRule
		if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, r := range rules {
			if r.OrgID != "org-A" {
				t.Errorf("leaked rule %s owned by %s into org-A listing", r.ID, r.OrgID)
			}
		}
	})

	t.Run("create forces the caller's org", func(t *testing.T) {
		body := `{"id":"rule-new","org_id":"org-B","name":"injected"}`
		rec := doWithAuth(t, sm, "org-A", http.MethodPost, "/api/v1/alert-rules", body, router)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (body=%s)", rec.Code, rec.Body.String())
		}
		if store.created == nil || store.created.OrgID != "org-A" {
			t.Errorf("created rule org = %+v, want forced org-A", store.created)
		}
	})

	t.Run("update of a foreign rule returns 404 and never mutates", func(t *testing.T) {
		body := `{"name":"taken-over","enabled":true}`
		rec := doWithAuth(t, sm, "org-A", http.MethodPut, "/api/v1/alert-rules/rule-B", body, router)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-org update, got %d", rec.Code)
		}
		if store.updatedOrg != "org-A" {
			t.Errorf("store received org %q, want org-A", store.updatedOrg)
		}
	})

	t.Run("delete of a foreign rule returns 404", func(t *testing.T) {
		rec := doWithAuth(t, sm, "org-A", http.MethodDelete, "/api/v1/alert-rules/rule-B", "", router)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-org delete, got %d", rec.Code)
		}
	})

	t.Run("update of an owned rule succeeds", func(t *testing.T) {
		body := `{"name":"mine","enabled":true}`
		rec := doWithAuth(t, sm, "org-A", http.MethodPut, "/api/v1/alert-rules/rule-A", body, router)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for same-org update, got %d (body=%s)", rec.Code, rec.Body.String())
		}
	})
}

// fakeAlertTransitionStore implements the alert subset of alerts.Store for
// lifecycle-transition tests.
type fakeAlertTransitionStore struct {
	alerts.Store

	alerts map[string]string // alert id -> owning org

	gotOrg string
}

func (f *fakeAlertTransitionStore) GetAlert(_ context.Context, orgID, id string) (*models.Alert, error) {
	f.gotOrg = orgID
	owner, ok := f.alerts[id]
	if orgID == "" || !ok || owner != orgID {
		return nil, alerts.ErrAlertNotFound
	}
	return &models.Alert{ID: id, OrgID: owner, State: "open"}, nil
}

func (f *fakeAlertTransitionStore) UpdateAlertState(_ context.Context, _ *models.Alert) error {
	return nil
}

func (f *fakeAlertTransitionStore) InsertStateTransition(_ context.Context, _ *models.AlertStateMachine) error {
	return nil
}

// TestAlertTransitionsScopedToOrg verifies lifecycle transitions pass the
// caller's org into the store lookup so foreign alerts resolve to 404.
func TestAlertTransitionsScopedToOrg(t *testing.T) {
	store := &fakeAlertTransitionStore{alerts: map[string]string{
		"alert-A": "org-A",
		"alert-B": "org-B",
	}}
	s, sm := newTenantTestServer(t, &fakeAgentStore{agents: map[string]string{}}, nil)
	engine := alerts.New(alerts.Config{Store: store})
	s.alertStore = store
	s.alertEngine = engine

	r := chi.NewRouter()
	r.Post("/api/v1/alerts/{id}/acknowledge", s.acknowledgeAlert)
	r.Post("/api/v1/alerts/{id}/close", s.closeAlert)

	rec := doWithAuth(t, sm, "org-A", http.MethodPost, "/api/v1/alerts/alert-B/acknowledge", "", r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org acknowledge: expected 404, got %d", rec.Code)
	}
	if store.gotOrg != "org-A" {
		t.Errorf("engine looked up with org %q, want org-A", store.gotOrg)
	}

	rec = doWithAuth(t, sm, "org-A", http.MethodPost, "/api/v1/alerts/alert-A/close", "", r)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-org close: expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}
