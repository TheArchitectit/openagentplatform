package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// HITL approval request API (hitl-approval spec R1). The approval state
// machine already lives in a2a/hitl (ApprovalManager); this file is the
// HTTP surface over it. Routes are mounted under /api/v1/a2a/approvals —
// the spec's logical /a2a/v1/approvals path mapped onto the platform's
// established /api/v1/a2a/* prefix (same convention as the gateway's
// /a2a/v1/tasks, which mounts at /api/v1/a2a/tasks).
//
// s.hitlManager may be nil; every handler then returns 503 with
// {"error":"hitl_not_configured"} (the patches.go posture).

// RegisterHITLRoutes mounts the approval API under an a2a sub-router.
func RegisterHITLRoutes(r chi.Router, s *Server) {
	r.Route("/approvals", func(r chi.Router) {
		// Reads: any authenticated org member.
		r.Get("/", s.handleListApprovals)
		r.Get("/{id}", s.handleGetApproval)
		// Creating and deciding drive what an agent is allowed to
		// execute, so they are elevated-role like the task-invoke routes.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
			r.Post("/", s.handleCreateApproval)
			r.Post("/{id}/approve", s.handleApproveApproval)
			r.Post("/{id}/reject", s.handleRejectApproval)
		})
	})
}

// hitlNotConfigured reports 503 when the approval engine is not wired.
func hitlNotConfigured(w http.ResponseWriter) bool {
	http.Error(w, `{"error":"hitl_not_configured"}`, http.StatusServiceUnavailable)
	return true
}

// handleCreateApproval — R1.1.
func (s *Server) handleCreateApproval(w http.ResponseWriter, r *http.Request) {
	if s.hitlManager == nil {
		hitlNotConfigured(w)
		return
	}
	var body struct {
		ActionType       string         `json:"action_type"`
		Payload          map[string]any `json:"payload"`
		RequesterAgentID string         `json:"requester_agent_id"`
		Urgency          string         `json:"urgency"`
		TaskID           string         `json:"task_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if body.ActionType == "" || body.RequesterAgentID == "" {
		http.Error(w, `{"error":"action_type_and_requester_agent_id_required"}`, http.StatusBadRequest)
		return
	}
	if body.Urgency == "" {
		body.Urgency = "medium"
	}
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	req, err := s.hitlManager.CreateRequestWithOrg(uuid.NewString(), body.ActionType, body.RequesterAgentID, body.Urgency, body.TaskID, orgID, body.Payload)
	if err != nil {
		// Unknown action type (the engine's validation signal) → 400.
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	writeRESTJSON(w, http.StatusCreated, req)
}

// handleListApprovals — R1.2: optional status filter (default: pending).
func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if s.hitlManager == nil {
		hitlNotConfigured(w)
		return
	}
	status := r.URL.Query().Get("status")
	var items []*hitl.ApprovalRequest
	switch {
	case status == "" || status == string(hitl.StatusPending):
		items = s.hitlManager.ListPending()
	case hitl.ValidStatuses[hitl.ApprovalStatus(status)]:
		items = s.hitlManager.ListByStatus(hitl.ApprovalStatus(status))
	default:
		http.Error(w, `{"error":"invalid_status"}`, http.StatusBadRequest)
		return
	}
	if items == nil {
		items = []*hitl.ApprovalRequest{}
	}
	writeRESTJSON(w, http.StatusOK, map[string]any{"approvals": items})
}

// handleGetApproval — R1.3.
func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	if s.hitlManager == nil {
		hitlNotConfigured(w)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	req, err := s.hitlManager.GetRequest(id)
	if err != nil {
		if errors.Is(err, hitl.ErrApprovalNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	writeRESTJSON(w, http.StatusOK, req)
}

// handleApproveApproval — R1.4.
func (s *Server) handleApproveApproval(w http.ResponseWriter, r *http.Request) {
	if s.hitlManager == nil {
		hitlNotConfigured(w)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	// Empty body is legal (approve without a comment).
	_ = json.NewDecoder(r.Body).Decode(&body)

	if err := s.hitlManager.Approve(id, approvalActor(r), body.Comment); err != nil {
		writeApprovalDecisionError(w, err)
		return
	}
	req, _ := s.hitlManager.GetRequest(id)
	writeRESTJSON(w, http.StatusOK, req)
}

// handleRejectApproval — R1.5 (reason required; the engine enforces it).
func (s *Server) handleRejectApproval(w http.ResponseWriter, r *http.Request) {
	if s.hitlManager == nil {
		hitlNotConfigured(w)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if body.Reason == "" {
		http.Error(w, `{"error":"reason_required"}`, http.StatusBadRequest)
		return
	}
	if err := s.hitlManager.Reject(id, approvalActor(r), body.Reason); err != nil {
		writeApprovalDecisionError(w, err)
		return
	}
	req, _ := s.hitlManager.GetRequest(id)
	writeRESTJSON(w, http.StatusOK, req)
}

// writeApprovalDecisionError maps engine decision errors onto HTTP:
// not-found → 404, already-decided → 409, missing reason → 400, else 500.
func writeApprovalDecisionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, hitl.ErrApprovalNotFound):
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	case errors.Is(err, hitl.ErrAlreadyDecided):
		http.Error(w, `{"error":"already_decided"}`, http.StatusConflict)
	case errors.Is(err, hitl.ErrInvalidTransition):
		http.Error(w, `{"error":"invalid_transition"}`, http.StatusConflict)
	case err != nil && errors.Is(err, hitl.ErrEscalationDepthMax):
		http.Error(w, `{"error":"escalation_depth_max"}`, http.StatusConflict)
	default:
		// Reject's "reason is required" validation error.
		http.Error(w, `{"error":"decision_failed"}`, http.StatusInternalServerError)
	}
}

// approvalActor resolves the deciding user from auth claims.
func approvalActor(r *http.Request) string {
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		if claims.Email != "" {
			return claims.Email
		}
		if claims.Subject != "" {
			return claims.Subject
		}
	}
	return "unknown"
}
