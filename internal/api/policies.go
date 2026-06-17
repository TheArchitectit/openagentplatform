// Package api - policies.go implements the HTTP handlers for the
// /api/v1/policies REST surface.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// --- Policy CRUD ----------------------------------------------------------

// listPolicies handles GET /api/v1/policies with pagination and
// optional filters (enforcement_mode, category, enabled, search).
func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	f := policy.PolicyFilter{
		OrgID:           q.Get("org_id"),
		Category:        q.Get("category"),
		EnforcementMode: q.Get("enforcement_mode"),
		Search:          q.Get("search"),
	}
	if claims, ok := authFromCtx(r); ok && claims != nil && claims.OrgID != "" {
		f.OrgID = claims.OrgID
	}
	if v := q.Get("enabled"); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			f.Enabled = &b
		}
	}
	if lim, err := strconv.Atoi(q.Get("limit")); err == nil {
		f.Limit = lim
	}
	if off, err := strconv.Atoi(q.Get("offset")); err == nil {
		f.Offset = off
	}
	policies, total, err := s.policyStore.ListPolicies(r.Context(), f)
	if err != nil {
		s.log.Error("list policies failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"policies": policies,
		"total":    total,
		"limit":    f.Limit,
		"offset":   f.Offset,
	})
}

// createPolicy handles POST /api/v1/policies. Validates Rego body,
// compiles it, and persists the policy.
func (s *Server) createPolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	if s.policyEngine == nil {
		http.Error(w, `{"error":"policy_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var p models.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if p.Name == "" {
		http.Error(w, `{"error":"name_required"}`, http.StatusBadRequest)
		return
	}
	if p.RegoBody == "" {
		http.Error(w, `{"error":"rego_body_required"}`, http.StatusBadRequest)
		return
	}
	if p.EnforcementMode == "" {
		p.EnforcementMode = "monitor"
	}
	if p.Severity == "" {
		p.Severity = "warning"
	}
	if p.Category == "" {
		p.Category = "security"
	}
	if p.ID == "" {
		p.ID = uuidNew()
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now

	if err := s.policyEngine.CompileAndStore(r.Context(), &p); err != nil {
		s.log.Error("create policy: compile failed", "err", err)
		http.Error(w, `{"error":"rego_compile_failed","detail":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

// getPolicy handles GET /api/v1/policies/{id}.
func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	p, err := s.policyStore.GetPolicy(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, policy.ErrPolicyNotFound) {
			http.Error(w, `{"error":"policy_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get policy failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

// updatePolicy handles PUT /api/v1/policies/{id}. Recompiles the Rego
// body and invalidates the OPA cache entry.
func (s *Server) updatePolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	if s.policyEngine == nil {
		http.Error(w, `{"error":"policy_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	var p models.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	existing, err := s.policyStore.GetPolicy(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, policy.ErrPolicyNotFound) {
			http.Error(w, `{"error":"policy_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get policy for update failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	// Merge mutable fields.
	p.ID = id
	if p.Name == "" {
		p.Name = existing.Name
	}
	if p.RegoBody == "" {
		p.RegoBody = existing.RegoBody
	}
	if p.EnforcementMode == "" {
		p.EnforcementMode = existing.EnforcementMode
	}
	if p.Severity == "" {
		p.Severity = existing.Severity
	}
	if p.Category == "" {
		p.Category = existing.Category
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()

	if err := s.policyEngine.UpdateAndRecompile(r.Context(), &p); err != nil {
		s.log.Error("update policy: compile failed", "err", err)
		http.Error(w, `{"error":"rego_compile_failed","detail":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

// deletePolicy handles DELETE /api/v1/policies/{id} (soft-delete).
func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	if err := s.policyStore.SoftDeletePolicy(r.Context(), orgID, id); err != nil {
		if errors.Is(err, policy.ErrPolicyNotFound) {
			http.Error(w, `{"error":"policy_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("delete policy failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	if s.policyEngine != nil {
		s.policyEngine.InvalidateCache(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// evaluatePolicy handles POST /api/v1/policies/{id}/evaluate. Body may
// include agent_id, site_id, org_id.
func (s *Server) evaluatePolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyEngine == nil {
		http.Error(w, `{"error":"policy_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var body policy.PolicyEvaluationRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
			return
		}
	}
	body.PolicyID = id

	results, err := s.policyEngine.EvaluatePolicyManual(r.Context(), body)
	if err != nil {
		if errors.Is(err, policy.ErrPolicyNotFound) {
			http.Error(w, `{"error":"policy_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("policy evaluation failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	compliant := 0
	for _, r := range results {
		if r.Compliant {
			compliant++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"results":   results,
		"count":     len(results),
		"compliant": compliant,
		"violated":  len(results) - compliant,
	})
}

// assignPolicy handles POST /api/v1/policies/{id}/assign. Body carries
// agent_ids and/or site_ids arrays.
func (s *Server) assignPolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyStore == nil {
		http.Error(w, `{"error":"policy_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	var body struct {
		AgentIDs []string `json:"agent_ids"`
		SiteIDs  []string `json:"site_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	// Confirm the policy exists.
	if _, err := s.policyStore.GetPolicy(r.Context(), orgID, id); err != nil {
		if errors.Is(err, policy.ErrPolicyNotFound) {
			http.Error(w, `{"error":"policy_not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	created := 0
	for _, agentID := range body.AgentIDs {
		a := &models.PolicyAssignment{
			ID:        uuidNew(),
			PolicyID:  id,
			AgentID:   agentID,
			CreatedAt: now,
		}
		if err := s.policyStore.InsertPolicyAssignment(r.Context(), a); err != nil {
			s.log.Error("insert assignment failed", "err", err, "agent_id", agentID)
			continue
		}
		created++
	}
	for _, siteID := range body.SiteIDs {
		a := &models.PolicyAssignment{
			ID:        uuidNew(),
			PolicyID:  id,
			SiteID:    siteID,
			CreatedAt: now,
		}
		if err := s.policyStore.InsertPolicyAssignment(r.Context(), a); err != nil {
			s.log.Error("insert assignment failed", "err", err, "site_id", siteID)
			continue
		}
		created++
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"created": created,
	})
}

// getPolicyViolations was relocated to policy_violations.go as
// listViolationsByPolicy. The route binding is updated in routes.go.

// evaluateSite handles POST /api/v1/policies/evaluate-site. Body
// carries site_id; all policies for all agents in the site are
// evaluated.
func (s *Server) evaluateSite(w http.ResponseWriter, r *http.Request) {
	if s.policyEngine == nil {
		http.Error(w, `{"error":"policy_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		SiteID string `json:"site_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if body.SiteID == "" {
		http.Error(w, `{"error":"site_id_required"}`, http.StatusBadRequest)
		return
	}
	results, err := s.policyEngine.EvaluateSite(r.Context(), body.SiteID)
	if err != nil {
		s.log.Error("evaluate site failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	// Flatten into a single list of (agent, result) pairs for the
	// dashboard.
	out := make([]map[string]any, 0, 64)
	for agentID, rs := range results {
		for _, r := range rs {
			out = append(out, map[string]any{
				"agent_id":   agentID,
				"policy_id":  r.PolicyID,
				"compliant":  r.Compliant,
				"violations": r.Violations,
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"site_id": body.SiteID,
		"results": out,
	})
}
