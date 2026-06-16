// Package api - policy_violations.go implements the HTTP handlers for
// the policy-violation REST surface:
//
//   GET  /api/v1/policies/{id}/violations -- list violations for a policy
//   GET  /api/v1/agents/{id}/violations   -- list violations for an agent
//   POST /api/v1/violations/{id}/dismiss  -- dismiss a violation
//   POST /api/v1/violations/{id}/remediate -- trigger remediation
//   GET  /api/v1/compliance/summary       -- org-level compliance summary
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/policy"
)

// listViolationsByPolicy handles GET /api/v1/policies/{id}/violations.
// Supports query params: agent_id, resolved (bool), limit, offset.
func (s *Server) listViolationsByPolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	q := r.URL.Query()
	f := policy.ViolationFilter{
		AgentID: q.Get("agent_id"),
	}
	if v := q.Get("resolved"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			f.Resolved = &b
		}
	}
	if v := q.Get("status"); v != "" {
		// Accept "open"/"dismissed" as aliases for resolved=true/false.
		switch v {
		case "open":
			b := false
			f.Resolved = &b
		case "dismissed", "resolved":
			b := true
			f.Resolved = &b
		}
	}
	if lim, err := strconv.Atoi(q.Get("limit")); err == nil {
		f.Limit = lim
	}
	if off, err := strconv.Atoi(q.Get("offset")); err == nil {
		f.Offset = off
	}
	violations, total, err := s.policyStore.GetPolicyViolations(r.Context(), id, f)
	if err != nil {
		s.log.Error("list policy violations failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"violations": violations,
		"total":      total,
		"limit":      f.Limit,
		"offset":     f.Offset,
	})
}

// listViolationsByAgent handles GET /api/v1/agents/{id}/violations.
func (s *Server) listViolationsByAgent(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	q := r.URL.Query()

	var resolved *bool
	if v := q.Get("resolved"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			resolved = &b
		}
	}
	if v := q.Get("status"); v != "" {
		switch v {
		case "open":
			b := false
			resolved = &b
		case "dismissed", "resolved":
			b := true
			resolved = &b
		}
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	violations, total, err := s.policyStore.ListViolationsByAgent(r.Context(), id, resolved, limit, offset)
	if err != nil {
		s.log.Error("list agent violations failed", "agent_id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"agent_id":   id,
		"violations": violations,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

// dismissViolation handles POST /api/v1/violations/{id}/dismiss. Body
// carries an optional "reason" string; the caller is taken from the
// auth context.
func (s *Server) dismissViolation(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var body struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
			return
		}
	}
	actor := actorFromContext(r)
	v, err := s.policyStore.DismissPolicyViolation(r.Context(), id, body.Reason, actor)
	if err != nil {
		if errors.Is(err, policy.ErrPolicyViolationNotFound) {
			http.Error(w, `{"error":"violation_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("dismiss violation failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	s.recordViolationAudit(r, "violation_dismissed", v.ID, map[string]any{
		"policy_id": v.PolicyID,
		"agent_id":  v.AgentID,
		"reason":    body.Reason,
	})
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// remediateViolation handles POST /api/v1/violations/{id}/remediate.
// The actual remediation logic is policy-defined (an agent may need
// to install a package, change a config, or trigger a script). This
// handler records the remediation request in the violation's Details
// and publishes a "remediation.requested" event so downstream agents
// can pick it up.
func (s *Server) remediateViolation(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	if s.eventBus == nil {
		http.Error(w, `{"error":"event_bus_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	v, err := s.policyStore.GetPolicyViolationByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, policy.ErrPolicyViolationNotFound) {
			http.Error(w, `{"error":"violation_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("remediate: get violation failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	if v.Resolved {
		http.Error(w, `{"error":"already_resolved"}`, http.StatusConflict)
		return
	}

	actor := actorFromContext(r)
	var body struct {
		Action string `json:"action"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Action == "" {
		body.Action = "default"
	}

	payload, _ := json.Marshal(map[string]any{
		"type":          "remediation.requested",
		"violation_id":  v.ID,
		"policy_id":     v.PolicyID,
		"agent_id":      v.AgentID,
		"action":        body.Action,
		"requested_by":  actor,
		"timestamp":     time.Now().UTC(),
	})
	if err := s.eventBus.Publish(r.Context(), "oap.events.remediation", payload); err != nil {
		s.log.Error("publish remediation event failed", "err", err)
		http.Error(w, `{"error":"publish_failed"}`, http.StatusBadGateway)
		return
	}
	s.recordViolationAudit(r, "remediation_requested", v.ID, map[string]any{
		"policy_id": v.PolicyID,
		"agent_id":  v.AgentID,
		"action":    body.Action,
	})

	w.WriteHeader(http.StatusAccepted)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "remediation_requested",
		"violation_id": v.ID,
		"action":       body.Action,
	})
}

// complianceSummary handles GET /api/v1/compliance/summary.
func (s *Server) complianceSummary(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	orgID := r.URL.Query().Get("org_id")
	summary, err := s.policyStore.ComplianceSummary(r.Context(), orgID)
	if err != nil {
		s.log.Error("compliance summary failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

// recordViolationAudit writes a "policy_change" audit event for
// user-driven actions on a violation (dismiss, remediate). Failures
// are logged and do not block the HTTP response.
func (s *Server) recordViolationAudit(r *http.Request, action, violationID string, details map[string]any) {
	if s.audit == nil {
		return
	}
	actor := actorFromContext(r)
	orgID := ""
	if d, ok := details["org_id"].(string); ok {
		orgID = d
	}
	ev, err := s.audit.Record(r.Context(), audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actor,
		Action:       action,
		ResourceType: "policy_violation",
		ResourceID:   violationID,
		Details:      details,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
	})
	if err != nil {
		s.log.Error("audit: violation event failed", "action", action, "err", err)
		_ = ev
	}
}
