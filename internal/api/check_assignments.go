package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// handleAssignCheck accepts a single (or small batch) assignment request.
// Body: { "agent_ids": ["..."], "site_ids": ["..."] } — any combination.
// Responds with the number of assignments created per source.
func (s *Server) handleAssignCheck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	checkID := chi.URLParam(r, "id")
	if checkID == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		AgentIDs []string `json:"agent_ids"`
		SiteIDs  []string `json:"site_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if len(req.AgentIDs) == 0 && len(req.SiteIDs) == 0 {
		http.Error(w, `{"error":"agent_ids_or_site_ids_required"}`, http.StatusBadRequest)
		return
	}
	store := s.checkStore()
	if _, err := store.GetCheck(r.Context(), checkID); err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	actor := ""
	if claims, ok := authFromCtx(r); ok {
		actor = claims.Subject
	}
	now := time.Now().UTC()
	agentCreated := 0
	for _, aid := range req.AgentIDs {
		a := &models.CheckAssignment{
			ID:         uuid.NewString(),
			CheckID:    checkID,
			AgentID:    aid,
			AssignedBy: actor,
			CreatedAt:  now,
		}
		if err := store.AssignCheck(r.Context(), a); err != nil {
			s.log.Warn("assign agent failed", "agent_id", aid, "err", err)
			continue
		}
		agentCreated++
	}
	siteCreated := 0
	for _, sid := range req.SiteIDs {
		n, err := store.AssignCheckToSite(r.Context(), checkID, sid, actor)
		if err != nil {
			s.log.Warn("assign site failed", "site_id", sid, "err", err)
			continue
		}
		siteCreated += n
	}
	s.recordAudit(r, "check.assign", "check", checkID, map[string]any{
		"agent_ids":         req.AgentIDs,
		"site_ids":          req.SiteIDs,
		"agents_created":    agentCreated,
		"site_fanned_count": siteCreated,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"check_id":          checkID,
		"agents_assigned":   agentCreated,
		"sites_fanned":      len(req.SiteIDs),
		"site_agents_added": siteCreated,
		"total":             agentCreated + siteCreated,
	})
}

// handleUnassignCheck removes a single (check_id, agent_id) assignment.
func (s *Server) handleUnassignCheck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	checkID := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agent_id")
	if checkID == "" || agentID == "" {
		http.Error(w, `{"error":"missing_params"}`, http.StatusBadRequest)
		return
	}
	if err := s.checkStore().RemoveAssignment(r.Context(), checkID, agentID); err != nil {
		if errors.Is(err, ErrAssignmentNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("unassign failed", "err", err)
		http.Error(w, `{"error":"unassign_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "check.unassign", "check", checkID, map[string]any{"agent_id": agentID})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// handleListCheckAssignments returns the agents currently assigned to a
// check, each with its most recent result.
func (s *Server) handleListCheckAssignments(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	checkID := chi.URLParam(r, "id")
	if checkID == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	if _, err := s.checkStore().GetCheck(r.Context(), checkID); err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	assignments, err := s.checkStore().ListAssignments(r.Context(), checkID)
	if err != nil {
		s.log.Error("list assignments failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if assignments == nil {
		assignments = []models.CheckAssignmentDetail{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"check_id":    checkID,
		"assignments": assignments,
		"total":       len(assignments),
	})
}

// handleBulkAssign assigns one check to many agents and/or sites at once.
// Body: { "check_id": "...", "agent_ids": ["..."], "site_ids": ["..."] }.
// The path is /api/v1/checks/assign-bulk so the {id} param isn't used.
func (s *Server) handleBulkAssign(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var req struct {
		CheckID  string   `json:"check_id"`
		AgentIDs []string `json:"agent_ids"`
		SiteIDs  []string `json:"site_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.CheckID == "" {
		http.Error(w, `{"error":"check_id_required"}`, http.StatusBadRequest)
		return
	}
	if len(req.AgentIDs) == 0 && len(req.SiteIDs) == 0 {
		http.Error(w, `{"error":"agent_ids_or_site_ids_required"}`, http.StatusBadRequest)
		return
	}
	store := s.checkStore()
	if _, err := store.GetCheck(r.Context(), req.CheckID); err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	actor := ""
	if claims, ok := authFromCtx(r); ok {
		actor = claims.Subject
	}
	now := time.Now().UTC()
	agentCreated := 0
	for _, aid := range req.AgentIDs {
		a := &models.CheckAssignment{
			ID:         uuid.NewString(),
			CheckID:    req.CheckID,
			AgentID:    aid,
			AssignedBy: actor,
			CreatedAt:  now,
		}
		if err := store.AssignCheck(r.Context(), a); err != nil {
			s.log.Warn("bulk assign agent failed", "agent_id", aid, "err", err)
			continue
		}
		agentCreated++
	}
	siteCreated := 0
	for _, sid := range req.SiteIDs {
		n, err := store.AssignCheckToSite(r.Context(), req.CheckID, sid, actor)
		if err != nil {
			s.log.Warn("bulk assign site failed", "site_id", sid, "err", err)
			continue
		}
		siteCreated += n
	}
	s.recordAudit(r, "check.bulk_assign", "check", req.CheckID, map[string]any{
		"agent_ids":         req.AgentIDs,
		"site_ids":          req.SiteIDs,
		"agents_created":    agentCreated,
		"site_agents_added": siteCreated,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"check_id":          req.CheckID,
		"agents_assigned":   agentCreated,
		"sites_fanned":      len(req.SiteIDs),
		"site_agents_added": siteCreated,
		"total":             agentCreated + siteCreated,
	})
}

// authFromCtx pulls claims from the request context (set by the auth
// middleware). Returns the claims and true on success, nil/false otherwise.
func authFromCtx(r *http.Request) (*auth.SessionClaims, bool) {
	return auth.UserFromContext(r.Context())
}
