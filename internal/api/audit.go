package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/audit"
)

// listAuditEvents handles GET /api/v1/audit/events.
//
// Query parameters:
//
//	actor_id, action, resource_type, resource_id
//	since, until   (RFC3339 timestamps)
//	limit, offset  (pagination)
func (s *Server) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		http.Error(w, `{"error":"audit_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	f := audit.EventFilter{
		ActorID:      q.Get("actor_id"),
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = t
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Offset = n
		}
	}

	events, total, err := s.audit.GetEvents(r.Context(), f)
	if err != nil {
		s.log.Error("audit: list events failed", "err", err)
		http.Error(w, `{"error":"audit_list_failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events": events,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

// getAuditEvent handles GET /api/v1/audit/events/{id}.
func (s *Server) getAuditEvent(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		http.Error(w, `{"error":"audit_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	ev, err := s.audit.GetEvent(r.Context(), id)
	if err != nil {
		if errors.Is(err, audit.ErrNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("audit: get event failed", "err", err)
		http.Error(w, `{"error":"audit_get_failed"}`, http.StatusInternalServerError)
		return
	}
	// Recompute the hash to flag any tampering.
	valid := audit.VerifyHash(ev)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"event":           ev,
		"hash_valid":      valid,
		"verification":    "sha256",
	})
}

// getAuditChain handles GET /api/v1/audit/chain/{resource_id}.
func (s *Server) getAuditChain(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		http.Error(w, `{"error":"audit_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	resourceID := chi.URLParam(r, "resource_id")
	if resourceID == "" {
		http.Error(w, `{"error":"missing_resource_id"}`, http.StatusBadRequest)
		return
	}
	ver, err := s.audit.GetEventChain(r.Context(), resourceID)
	if err != nil {
		s.log.Error("audit: get chain failed", "err", err)
		http.Error(w, `{"error":"audit_chain_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ver)
}
