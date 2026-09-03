package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/eve"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func (s *Server) mountEveRoutes(r chi.Router) {
	r.Route("/eve", func(r chi.Router) {
		r.Get("/clusters", s.listEVEClusters)
		r.With(auth.RequireRole(auth.RoleAdmin)).Post("/clusters", s.createEVECluster)
		r.With(auth.RequireRole(auth.RoleAdmin)).Put("/clusters/{id}", s.updateEVECluster)
		r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/clusters/{id}", s.deleteEVECluster)
		r.Get("/clusters/{id}/resources", s.listEVEClusterResources)
		r.Get("/clusters/{id}/events", s.listEVEClusterEvents)
		r.Get("/resources/{id}", s.getEVEResource)
		r.Post("/resources/{id}/enroll", s.enrollEVEResource)
	})
}

// eveStores bundles the three EVE persistence interfaces the handlers need.
// Following the cloudStores pattern: nil → endpoint returns 503.
type eveStores struct {
	clusters   eve.ClusterStore
	resources eve.ResourceStore
	events    eve.EventStore
}

// SetEVEStores wires the EVE persistence layer into the server.
func (s *Server) SetEVEStores(stores *eveStores) {
	s.eve = stores
}

func (s *Server) listEVEClusters(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	clusters, err := s.eve.clusters.ListByOrg(r.Context(), tc.OrgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if clusters == nil {
		clusters = []*models.HypervisorCluster{}
	}
	writeJSON(w, 200, clusters)
}

func (s *Server) createEVECluster(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var cluster models.HypervisorCluster
	if err := json.NewDecoder(r.Body).Decode(&cluster); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	cluster.OrgID = tc.OrgID
	if err := s.eve.clusters.Create(r.Context(), &cluster); err != nil {
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 201, cluster)
}

func (s *Server) updateEVECluster(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var cluster models.HypervisorCluster
	if err := json.NewDecoder(r.Body).Decode(&cluster); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	cluster.ID = id
	if err := s.eve.clusters.Update(r.Context(), &cluster); err != nil {
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, cluster)
}

func (s *Server) deleteEVECluster(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.eve.clusters.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listEVEClusterResources(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	clusterID := chi.URLParam(r, "id")
	resources, err := s.eve.resources.ListByCluster(r.Context(), clusterID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if resources == nil {
		resources = []*models.HypervisorResource{}
	}
	writeJSON(w, 200, resources)
}

func (s *Server) listEVEClusterEvents(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	clusterID := chi.URLParam(r, "id")
	events, err := s.eve.events.ListByCluster(r.Context(), clusterID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*models.HypervisorEvent{}
	}
	writeJSON(w, 200, events)
}

func (s *Server) getEVEResource(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	res, err := s.eve.resources.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) enrollEVEResource(w http.ResponseWriter, r *http.Request) {
	if s.eve == nil {
		http.Error(w, `{"error":"eve_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	res, err := s.eve.resources.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	if err := s.eve.resources.MarkEnrolled(r.Context(), id); err != nil {
		http.Error(w, `{"error":"enroll_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, res)
}
