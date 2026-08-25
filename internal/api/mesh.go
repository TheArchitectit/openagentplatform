package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/mesh"
)

// meshSessionCreateReq is the JSON body for POST /mesh/session.
type meshSessionCreateReq struct {
	AgentID string `json:"agent_id"`
	Purpose string `json:"purpose"`
}

// meshSessionCloseReq is the JSON body for POST /mesh/session/{id}/close.
type meshSessionCloseReq struct{}

// handleMeshSessionCreate validates RBAC + org-scoping, opens a mesh tunnel
// session, and returns the SessionGrant (WireGuard config + SSH cert).
func (s *Server) handleMeshSessionCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	// RBAC: only admin and operator roles may open mesh sessions.
	if user.Role != auth.RoleAdmin && user.Role != auth.RoleOperator && user.Role != auth.RoleTechnician {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	// Org-scoping: ALWAYS from the authenticated session, never from body.
	orgID := user.OrgID
	if orgID == "" {
		http.Error(w, `{"error":"org_id missing from session"}`, http.StatusForbidden)
		return
	}

	var req meshSessionCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.AgentID == "" || req.Purpose == "" {
		http.Error(w, `{"error":"agent_id and purpose required"}`, http.StatusBadRequest)
		return
	}

	grant, err := s.meshAdmission.RequestSession(r.Context(), orgID, user.Subject, req.AgentID, req.Purpose)
	if err != nil {
		switch {
		case errors.Is(err, mesh.ErrAgentNotInMesh):
			http.Error(w, `{"error":"agent not enrolled in mesh"}`, http.StatusNotFound)
		case errors.Is(err, mesh.ErrInvalidPurpose):
			http.Error(w, `{"error":"invalid purpose"}`, http.StatusBadRequest)
		default:
			s.log.Warn("mesh session create failed", "err", err)
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(grant)
}

// handleMeshSessionList returns the caller's mesh sessions (org-scoped).
func (s *Server) handleMeshSessionList(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := user.OrgID
	if orgID == "" {
		http.Error(w, `{"error":"org_id missing from session"}`, http.StatusForbidden)
		return
	}
	// Admin sees all org sessions; others see only their own.
	operatorID := user.Subject
	if user.Role == auth.RoleAdmin {
		operatorID = "" // empty = all
	}

	sessions, err := s.meshAdmission.ListSessions(r.Context(), orgID, operatorID)
	if err != nil {
		s.log.Warn("mesh session list failed", "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// handleMeshSessionClose terminates a mesh session. The session must belong
// to the caller's org.
func (s *Server) handleMeshSessionClose(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := user.OrgID
	if orgID == "" {
		http.Error(w, `{"error":"org_id missing from session"}`, http.StatusForbidden)
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, `{"error":"session id required"}`, http.StatusBadRequest)
		return
	}

	if err := s.meshAdmission.CloseSession(r.Context(), orgID, sessionID); err != nil {
		if errors.Is(err, mesh.ErrMeshSessionNotFound) {
			http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		} else {
			s.log.Warn("mesh session close failed", "err", err)
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
