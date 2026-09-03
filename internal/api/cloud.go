// Cloud control API handlers — /api/v1/cloud.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// mountCloudRoutes registers the /api/v1/cloud route group.
func (s *Server) mountCloudRoutes(r chi.Router) {
	r.Route("/cloud", func(r chi.Router) {
		// Accounts
		r.Get("/accounts", s.listCloudAccounts)
		r.With(auth.RequireRole(auth.RoleAdmin)).Post("/accounts", s.createCloudAccount)
		r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/accounts/{id}", s.deleteCloudAccount)

		// Resources
		r.Get("/resources", s.listCloudResources)
		r.Get("/resources/{id}", s.getCloudResource)
		r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/resources/{id}/enroll", s.enrollCloudResource)
		r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/resources/{id}/ignore", s.ignoreCloudResource)

		// Policies
		r.Get("/policies", s.listCloudPolicies)
		r.With(auth.RequireRole(auth.RoleAdmin)).Post("/policies", s.createCloudPolicy)
		r.With(auth.RequireRole(auth.RoleAdmin)).Put("/policies/{id}", s.updateCloudPolicy)
		r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/policies/{id}", s.deleteCloudPolicy)

		// Costs + drift
		r.Get("/costs", s.listCloudCosts)
		r.Get("/drift", s.listCloudDrift)
	})
}

func (s *Server) cloudStores() *cloudStores {
	return s.cloud
}

// cloudStores bundles the four cloud persistence interfaces the handlers need.
type cloudStores struct {
	accounts cloud.AccountStore
	resources cloud.ResourceStore
	policies  cloud.PolicyStore
	costs    cloud.CostStore
}

// SetCloudStores wires the cloud persistence layer into the server.
func (s *Server) SetCloudStores(stores *cloudStores) {
	s.cloud = stores
}

func (s *Server) listCloudAccounts(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	orgID := tc.OrgID
	accounts, err := s.cloud.accounts.ListByOrg(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if accounts == nil {
		accounts = []*models.CloudAccount{}
	}
	json.NewEncoder(w).Encode(accounts)
}

func (s *Server) createCloudAccount(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var acct models.CloudAccount
	if err := json.NewDecoder(r.Body).Decode(&acct); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	acct.OrgID = tc.OrgID
	if err := s.cloud.accounts.Create(r.Context(), &acct); err != nil {
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(acct)
}

func (s *Server) deleteCloudAccount(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.cloud.accounts.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listCloudResources(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	orgID := tc.OrgID
	resources, err := s.cloud.resources.ListByOrg(r.Context(), orgID, cloud.ResourceFilter{})
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if resources == nil {
		resources = []*models.CloudResource{}
	}
	json.NewEncoder(w).Encode(resources)
}

func (s *Server) getCloudResource(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	res, err := s.cloud.resources.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(res)
}

func (s *Server) enrollCloudResource(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.cloud.resources.UpdateStatus(r.Context(), id, "pending_install"); err != nil {
		http.Error(w, `{"error":"enroll_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ignoreCloudResource(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.cloud.resources.Archive(r.Context(), id); err != nil {
		http.Error(w, `{"error":"ignore_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listCloudPolicies(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	orgID := tc.OrgID
	policies, err := s.cloud.policies.ListByOrg(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if policies == nil {
		policies = []*models.CloudPolicy{}
	}
	json.NewEncoder(w).Encode(policies)
}

func (s *Server) createCloudPolicy(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var pol models.CloudPolicy
	if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	pol.OrgID = tc.OrgID
	if err := s.cloud.policies.Create(r.Context(), &pol); err != nil {
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pol)
}

func (s *Server) updateCloudPolicy(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var pol models.CloudPolicy
	if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	pol.OrgID = tc.OrgID
	if err := s.cloud.policies.Update(r.Context(), &pol); err != nil {
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(pol)
}

func (s *Server) deleteCloudPolicy(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.cloud.policies.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listCloudCosts(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	orgID := tc.OrgID
	snapshots, err := s.cloud.costs.ListByOrg(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if snapshots == nil {
		snapshots = []*models.CostSnapshot{}
	}
	json.NewEncoder(w).Encode(snapshots)
}

func (s *Server) listCloudDrift(w http.ResponseWriter, r *http.Request) {
	if s.cloud == nil {
		http.Error(w, `{"error":"cloud_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	orgID := tc.OrgID
	drift, err := s.cloud.resources.ListDrift(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if drift == nil {
		drift = []*models.CloudResource{}
	}
	json.NewEncoder(w).Encode(drift)
}
