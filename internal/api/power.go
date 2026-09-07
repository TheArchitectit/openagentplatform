// Power monitoring API handlers — /api/v1/power.
package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// powerStores bundles the power persistence interfaces the handlers need.
type powerStores struct {
	events PowerEventLogStore
}

// PowerEventLogStore is the read contract for power state transitions.
type PowerEventLogStore interface {
	ListByOrg(ctx context.Context, orgID string, limit int) ([]*models.PowerStateLog, error)
	LatestByOrg(ctx context.Context, orgID string) ([]*models.PowerStateLog, error)
}

// SetPowerStores wires the power persistence layer into the server.
func (s *Server) SetPowerStores(stores *powerStores) {
	s.power = stores
}

func (s *Server) mountPowerRoutes(r chi.Router) {
	r.Route("/power", func(r chi.Router) {
		r.Get("/events", s.listPowerEvents)
		r.Get("/state", s.listPowerState)
	})
}

func (s *Server) listPowerEvents(w http.ResponseWriter, r *http.Request) {
	if s.power == nil {
		http.Error(w, `{"error":"power_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	events, err := s.power.events.ListByOrg(r.Context(), tc.OrgID, 100)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*models.PowerStateLog{}
	}
	writeJSON(w, 200, events)
}

func (s *Server) listPowerState(w http.ResponseWriter, r *http.Request) {
	if s.power == nil {
		http.Error(w, `{"error":"power_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	state, err := s.power.events.LatestByOrg(r.Context(), tc.OrgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if state == nil {
		state = []*models.PowerStateLog{}
	}
	writeJSON(w, 200, state)
}
