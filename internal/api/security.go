// Active Security / EDR — API handlers for /api/v1/security.
//
// Three surfaces:
//   - /edr/integrations — CRUD for EDR vendor integrations (REQUIRES auth)
//   - /siem/forwarders  — CRUD for SIEM forwarders (REQUIRES auth)
//   - /events           — list/get security events (REQUIRES auth)
//   - /security-events/ingest/{provider} — webhook receiver (NO auth; HMAC-gated)
//
// Following the cloudStores/eveStores pattern: nil → endpoint returns 503.
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/security/ingest"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// securityStores bundles the security persistence interfaces the handlers need.
type securityStores struct {
	events      SecurityEventStore
	integrations EDRIntegrationStore
	forwarders  SIEMForwarderStore
}

// SecurityEventStore is the read contract for security events.
type SecurityEventStore interface {
	ListByOrg(ctx context.Context, orgID string, limit int) ([]*models.SecurityEvent, error)
	Get(ctx context.Context, id string) (*models.SecurityEvent, error)
}

// EDRIntegrationStore is the CRUD contract for EDR integrations.
type EDRIntegrationStore interface {
	Create(ctx context.Context, i *models.EDRIntegration) error
	Get(ctx context.Context, id string) (*models.EDRIntegration, error)
	ListByOrg(ctx context.Context, orgID string) ([]*models.EDRIntegration, error)
	Update(ctx context.Context, i *models.EDRIntegration) error
	Delete(ctx context.Context, id string) error
}

// SIEMForwarderStore is the CRUD contract for SIEM forwarders.
type SIEMForwarderStore interface {
	Create(ctx context.Context, f *models.SIEMForwarder) error
	Get(ctx context.Context, id string) (*models.SIEMForwarder, error)
	ListByOrg(ctx context.Context, orgID string) ([]*models.SIEMForwarder, error)
	Update(ctx context.Context, f *models.SIEMForwarder) error
	Delete(ctx context.Context, id string) error
}

// securityQueue is the interface the webhook handler needs from the
// async ingest queue. The concrete type is *ingest.Queue.
type securityQueue interface {
	Submit(ctx context.Context, job ingest.IngestJob) bool
}

// SetSecurityStores wires the security persistence layer into the server.
func (s *Server) SetSecurityStores(stores *securityStores) {
	s.security = stores
}

// SetSecurityQueue wires the async ingest queue into the server.
func (s *Server) SetSecurityQueue(q securityQueue) {
	s.securityQueue = q
}

func (s *Server) mountSecurityRoutes(r chi.Router) {
	r.Route("/security", func(r chi.Router) {
		r.Get("/events", s.listSecurityEvents)
		r.Get("/events/{id}", s.getSecurityEvents)

		r.With(auth.RequireRole(auth.RoleAdmin)).Post("/edr/integrations", s.createEDRIntegration)
		r.Get("/edr/integrations", s.listEDRIntegrations)
		r.Get("/edr/integrations/{id}", s.getEDRIntegration)
		r.With(auth.RequireRole(auth.RoleAdmin)).Put("/edr/integrations/{id}", s.updateEDRIntegration)
		r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/edr/integrations/{id}", s.deleteEDRIntegration)

		r.With(auth.RequireRole(auth.RoleAdmin)).Post("/siem/forwarders", s.createSIEMForwarder)
		r.Get("/siem/forwarders", s.listSIEMForwarders)
		r.Get("/siem/forwarders/{id}", s.getSIEMForwarder)
		r.With(auth.RequireRole(auth.RoleAdmin)).Put("/siem/forwarders/{id}", s.updateSIEMForwarder)
		r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/siem/forwarders/{id}", s.deleteSIEMForwarder)
	})
}

// handleSecurityWebhook receives EDR webhook events. It is registered
// outside auth middleware (HMAC signature is the gate). The body is read,
// enqueued to the ingest queue, and 202 returned immediately.
func (s *Server) handleSecurityWebhook(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	job := ingest.IngestJob{IntegrationID: provider, Event: body, Source: "webhook"}
	if s.securityQueue == nil || !s.securityQueue.Submit(r.Context(), job) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, `{"error":"queue_full"}`, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) listSecurityEvents(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	events, err := s.security.events.ListByOrg(r.Context(), tc.OrgID, 100)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*models.SecurityEvent{}
	}
	writeJSON(w, 200, events)
}

func (s *Server) getSecurityEvents(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	ev, err := s.security.events.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, 200, ev)
}

func (s *Server) listEDRIntegrations(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	items, err := s.security.integrations.ListByOrg(r.Context(), tc.OrgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []*models.EDRIntegration{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createEDRIntegration(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var i models.EDRIntegration
	if err := json.NewDecoder(r.Body).Decode(&i); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	i.OrgID = tc.OrgID
	if err := s.security.integrations.Create(r.Context(), &i); err != nil {
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 201, i)
}

func (s *Server) getEDRIntegration(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	item, err := s.security.integrations.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) updateEDRIntegration(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var i models.EDRIntegration
	if err := json.NewDecoder(r.Body).Decode(&i); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	i.ID = chi.URLParam(r, "id")
	if err := s.security.integrations.Update(r.Context(), &i); err != nil {
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, i)
}

func (s *Server) deleteEDRIntegration(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.security.integrations.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listSIEMForwarders(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	items, err := s.security.forwarders.ListByOrg(r.Context(), tc.OrgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []*models.SIEMForwarder{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createSIEMForwarder(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var f models.SIEMForwarder
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	tc, _ := tenancy.GetTenant(r.Context())
	f.OrgID = tc.OrgID
	if err := s.security.forwarders.Create(r.Context(), &f); err != nil {
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 201, f)
}

func (s *Server) getSIEMForwarder(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	item, err := s.security.forwarders.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) updateSIEMForwarder(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var f models.SIEMForwarder
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	f.ID = chi.URLParam(r, "id")
	if err := s.security.forwarders.Update(r.Context(), &f); err != nil {
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, f)
}

func (s *Server) deleteSIEMForwarder(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		http.Error(w, `{"error":"security_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.security.forwarders.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(204)
}
