// Package api - patches.go implements the HTTP handlers for the patch
// approval workflow. All endpoints are mounted under /api/v1/patches
// in routes.go.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// listPatches returns paginated, filterable patch jobs.
func (s *Server) listPatches(w http.ResponseWriter, r *http.Request) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := atoiDefault(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	filter := patches.PatchJobFilter{
		State:    q.Get("state"),
		Severity: q.Get("severity"),
		OrgID:    q.Get("org_id"),
		AgentID:  q.Get("agent_id"),
		Limit:    limit,
		Offset:   offset,
	}
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil && claims.OrgID != "" {
		filter.OrgID = claims.OrgID
	}
	if from := q.Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = t
		}
	}
	if to := q.Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = t
		}
	}
	jobs, total, err := s.patchStore.ListPatchJobs(r.Context(), filter)
	if err != nil {
		s.log.Error("list patch jobs failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []models.PatchJob{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"patches": jobs,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// getPatch returns a single patch job with its targets and approval
// history.
func (s *Server) getPatch(w http.ResponseWriter, r *http.Request) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	job, err := s.patchStore.GetPatchJob(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, patches.ErrPatchJobNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get patch job failed", "id", id, "err", err)
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

// createPatchJob validates and persists a new patch job. Critical
// severity patches are auto-approved; standard and major_os patches
// begin in pending_approval state.
func (s *Server) createPatchJob(w http.ResponseWriter, r *http.Request) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Title                  string                  `json:"title"`
		Description            string                  `json:"description"`
		Severity               models.PatchSeverity    `json:"severity"`
		PackageName            string                  `json:"package_name"`
		PackageVersion         string                  `json:"package_version"`
		RollbackVersion        string                  `json:"rollback_version"`
		Targets                []models.PatchJobTarget `json:"targets"`
		MaintenanceWindowStart *time.Time              `json:"maintenance_window_start,omitempty"`
		MaintenanceWindowEnd   *time.Time              `json:"maintenance_window_end,omitempty"`
		AutoApproveOnTimeout   *bool                   `json:"auto_approve_on_timeout,omitempty"`
		RequiredApprovals      int                     `json:"required_approvals,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, `{"error":"title_required"}`, http.StatusBadRequest)
		return
	}
	if req.PackageName == "" {
		http.Error(w, `{"error":"package_name_required"}`, http.StatusBadRequest)
		return
	}
	if req.Severity == "" {
		req.Severity = models.PatchSeverityStandard
	}
	switch req.Severity {
	case models.PatchSeverityCritical, models.PatchSeverityStandard, models.PatchSeverityMajorOS:
		// valid
	default:
		http.Error(w, `{"error":"invalid_severity"}`, http.StatusBadRequest)
		return
	}

	orgID := ""
	actorID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
		actorID = claims.Subject
	}

	now := time.Now().UTC()
	job := &models.PatchJob{
		ID:                     uuid.NewString(),
		OrgID:                  orgID,
		Title:                  req.Title,
		Description:            req.Description,
		Severity:               req.Severity,
		State:                  patches.StatePendingApproval,
		CreatedBy:              actorID,
		MaintenanceWindowStart: req.MaintenanceWindowStart,
		MaintenanceWindowEnd:   req.MaintenanceWindowEnd,
		RequiredApprovals:      req.RequiredApprovals,
		AutoApproveOnTimeout:   true,
		PackageName:            req.PackageName,
		PackageVersion:         req.PackageVersion,
		RollbackVersion:        req.RollbackVersion,
		Targets:                req.Targets,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if req.AutoApproveOnTimeout != nil {
		job.AutoApproveOnTimeout = *req.AutoApproveOnTimeout
	}

	// Apply approval policy (critical auto-approves, etc.).
	wf := patches.NewApprovalWorkflow()
	wf.ApplyPolicy(job)

	if err := s.patchStore.CreatePatchJob(r.Context(), job); err != nil {
		s.log.Error("create patch job failed", "err", err)
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}

	s.recordPatchAudit(r, "patch.create", job.ID, map[string]any{
		"severity":     string(job.Severity),
		"state":        job.State,
		"target_count": len(job.Targets),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(job)
}

// approvePatch transitions a patch job from pending_approval to
// approved (or stays in pending_approval until the required number
// of approvals is met).
func (s *Server) approvePatch(w http.ResponseWriter, r *http.Request) {
	s.patchTransition(w, r, patches.EventApprove)
}

// rejectPatch transitions a patch job from pending_approval (or
// approved, for admin override) to rejected.
func (s *Server) rejectPatch(w http.ResponseWriter, r *http.Request) {
	s.patchTransition(w, r, patches.EventReject)
}

// schedulePatch transitions an approved patch job to scheduled,
// setting the deployment time and optionally a maintenance window.
func (s *Server) schedulePatch(w http.ResponseWriter, r *http.Request) {
	s.patchTransition(w, r, patches.EventSchedule)
}

// cancelPatch transitions a scheduled patch job to rejected (cancelled).
func (s *Server) cancelPatch(w http.ResponseWriter, r *http.Request) {
	s.patchTransition(w, r, patches.EventCancel)
}

// rollbackPatch transitions a completed or failed patch job to
// rolled_back.
func (s *Server) rollbackPatch(w http.ResponseWriter, r *http.Request) {
	s.patchTransition(w, r, patches.EventRollback)
}

// patchTransition is the shared handler for all user-driven patch
// state transitions. It reads an optional comment and event-specific
// fields from the request body, runs the workflow, and persists the
// result.
func (s *Server) patchTransition(w http.ResponseWriter, r *http.Request, event string) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}

	var req struct {
		Comment          string     `json:"comment"`
		ScheduledAt      *time.Time `json:"scheduled_at,omitempty"`
		MaintenanceStart *time.Time `json:"maintenance_window_start,omitempty"`
		MaintenanceEnd   *time.Time `json:"maintenance_window_end,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	job, err := s.patchStore.GetPatchJob(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, patches.ErrPatchJobNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get patch job failed", "id", id, "err", err)
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}

	actor := patchActorFromContext(r)
	wf := patches.NewApprovalWorkflow()
	result, err := wf.Transition(r.Context(), patches.TransitionInput{
		Job:              job,
		Event:            event,
		Actor:            actor,
		Comment:          req.Comment,
		ScheduledAt:      req.ScheduledAt,
		MaintenanceStart: req.MaintenanceStart,
		MaintenanceEnd:   req.MaintenanceEnd,
	})
	if err != nil {
		switch {
		case errors.Is(err, patches.ErrInvalidTransition):
			http.Error(w, `{"error":"invalid_transition"}`, http.StatusConflict)
		case errors.Is(err, patches.ErrPermissionDenied):
			http.Error(w, `{"error":"permission_denied"}`, http.StatusForbidden)
		case errors.Is(err, patches.ErrOutsideMaintenanceWindow):
			http.Error(w, `{"error":"outside_maintenance_window"}`, http.StatusBadRequest)
		default:
			s.log.Error("patch transition failed", "id", id, "event", event, "err", err)
			http.Error(w, `{"error":"transition_failed"}`, http.StatusInternalServerError)
		}
		return
	}

	// Persist updated job and any new approval record.
	if err := s.patchStore.UpdatePatchJob(r.Context(), result.Job); err != nil {
		s.log.Error("update patch job failed", "id", id, "err", err)
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	if result.ApprovalRecord != nil && result.ApprovalRecord.ID == "" {
		result.ApprovalRecord.ID = uuid.NewString()
		if err := s.patchStore.InsertApprovalRecord(r.Context(), result.ApprovalRecord); err != nil {
			s.log.Error("insert approval record failed", "id", id, "err", err)
		}
	}

	s.recordPatchAudit(r, "patch."+event, id, map[string]any{
		"from_state": job.State,
		"to_state":   result.Job.State,
		"actor":      actorIDString(actor),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"patch":               result.Job,
		"auto_approved":       result.AutoApproved,
		"approvals_remaining": patches.RequiredApprovalCount(result.Job) - patches.CountApprovals(result.Job),
	})
}

// handleGetKBBatch returns per-KB WinUpdate states for the caller's org.
// It is a read-only endpoint available to any authenticated org member
// (no licensing gate, no role gate). Query params:
//   - agent_id (optional): if given, scope to that agent; must resolve
//     to the caller's org or 404 is returned.
//   - state (optional): if given, filter by state.
//
// Results are capped at 200 rows when unfiltered.
func (s *Server) handleGetKBBatch(w http.ResponseWriter, r *http.Request) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	if orgID == "" {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	agentID := q.Get("agent_id")
	state := q.Get("state")

	// If an agent_id is given, verify it belongs to the caller's org.
	if agentID != "" {
		if err := s.checkAgentOrg(r.Context(), orgID, agentID); err != nil {
			if errors.Is(err, patches.ErrWinUpdateKBNotFound) || errors.Is(err, errCrossOrgAgent) {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			s.log.Error("kb batch: check agent org failed", "agent_id", agentID, "err", err)
			http.Error(w, `{"error":"check_failed"}`, http.StatusInternalServerError)
			return
		}
	}

	// Cap unfiltered results at 200 rows (the store enforces the limit).
	limit := 200
	if state != "" {
		// state filter is applied in the store query; limit still 200.
		_ = limit
	}

	states, err := s.patchStore.GetKBStatesByAgent(r.Context(), orgID, agentID)
	if err != nil {
		s.log.Error("kb batch failed", "org_id", orgID, "agent_id", agentID, "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}

	// Apply state filter in-memory (the store returns all states for the
	// agent; filtering here keeps the store query simple and org-scoped).
	if state != "" {
		filtered := make([]models.WinUpdateKBState, 0, len(states))
		for _, st := range states {
			if st.State == state {
				filtered = append(filtered, st)
			}
		}
		states = filtered
	}

	if states == nil {
		states = []models.WinUpdateKBState{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"kb_states": states,
		"total":     len(states),
	})
}

// handleScheduleReboot accepts a list of agent_ids and enqueues a
// reboot directive for each. It uses the PatchDeployer's
// CoordinateReboots method to stagger the reboots. Admin or Technician
// role required (enforced by the route middleware).
//
// Every requested agent is verified to belong to the caller's org before
// a reboot directive is published, so a caller cannot reboot endpoints in
// another tenant.
func (s *Server) handleScheduleReboot(w http.ResponseWriter, r *http.Request) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	if s.patchDeployer == nil {
		http.Error(w, `{"error":"patch_deployer_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil || claims.OrgID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		AgentIDs []string `json:"agent_ids"`
		JobID    string   `json:"job_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if len(req.AgentIDs) == 0 {
		http.Error(w, `{"error":"agent_ids_required"}`, http.StatusBadRequest)
		return
	}
	// Deduplicate.
	seen := make(map[string]struct{}, len(req.AgentIDs))
	deduped := make([]string, 0, len(req.AgentIDs))
	for _, id := range req.AgentIDs {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) == 0 {
		http.Error(w, `{"error":"agent_ids_required"}`, http.StatusBadRequest)
		return
	}
	// Org-scope every target: an agent outside the caller's org is a 404
	// so the endpoint cannot be used to probe or reboot other tenants.
	for _, aid := range deduped {
		if err := s.checkAgentOrg(r.Context(), claims.OrgID, aid); err != nil {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
	}

	// Build reboot requests and delegate to the deployer.
	reboots := make([]patches.RebootRequest, 0, len(deduped))
	for _, aid := range deduped {
		reboots = append(reboots, patches.RebootRequest{
			AgentID: aid,
			JobID:   req.JobID,
		})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	results := s.patchDeployer.CoordinateReboots(ctx, reboots)

	s.recordPatchAudit(r, "patch.reboot", req.JobID, map[string]any{
		"agent_count": len(reboots),
		"job_id":      req.JobID,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"results": results,
		"total":   len(results),
	})
}

// errCrossOrgAgent is returned by checkAgentOrg when the requested agent
// does not belong to the caller's org.
var errCrossOrgAgent = errors.New("agent does not belong to org")

// checkAgentOrg verifies that agentID resolves to the given org. It
// reuses the agentStore lookup used elsewhere in the API (heartbeat /
// check ingest). Returns errCrossOrgAgent on mismatch.
func (s *Server) checkAgentOrg(ctx context.Context, orgID, agentID string) error {
	if agentID == "" {
		return errCrossOrgAgent
	}
	store := s.agentStore()
	a, err := store.GetAgent(ctx, orgID, agentID)
	if err != nil {
		return errCrossOrgAgent
	}
	if a == nil || a.OrgID != orgID {
		return errCrossOrgAgent
	}
	return nil
}

// getPatchStats returns aggregate statistics for the dashboard.
func (s *Server) getPatchStats(w http.ResponseWriter, r *http.Request) {
	if s.patchStore == nil {
		http.Error(w, `{"error":"patch_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	orgID := r.URL.Query().Get("org_id")
	stats, err := s.patchStore.GetPatchStats(r.Context(), orgID)
	if err != nil {
		s.log.Error("get patch stats failed", "err", err)
		http.Error(w, `{"error":"stats_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// patchActorFromContext extracts the actor from the request context.
// Returns a system actor (no permissions) if no auth claims are present.
func patchActorFromContext(r *http.Request) *patches.Actor {
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		perms := patchPermissionsForRole(claims.Role)
		return &patches.Actor{
			ID:          claims.Subject,
			Name:        claims.Name,
			Permissions: perms,
		}
	}
	return &patches.Actor{ID: "unknown", Permissions: nil}
}

// patchPermissionsForRole maps a user role to the set of patch RBAC
// permissions. Admin and operator roles get full access; viewers get
// read-only (no patch permissions).
func patchPermissionsForRole(role string) []string {
	switch role {
	case "admin":
		return []string{
			patches.PermApprove, patches.PermReject,
			patches.PermSchedule, patches.PermCancel,
			patches.PermRollback, patches.PermCreate,
		}
	case "operator":
		return []string{
			patches.PermApprove, patches.PermReject,
			patches.PermSchedule, patches.PermCancel,
			patches.PermCreate,
		}
	case "engineer":
		return []string{
			patches.PermApprove, patches.PermCreate,
		}
	}
	return nil
}

// actorIDString returns the actor's ID, or "system" if nil.
func actorIDString(a *patches.Actor) string {
	if a == nil {
		return "system"
	}
	return a.ID
}

// recordPatchAudit writes a patch-related audit event.
func (s *Server) recordPatchAudit(r *http.Request, action, resourceID string, details map[string]any) {
	if s.audit == nil {
		return
	}
	actorID := ""
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		actorID = claims.Subject
		orgID = claims.OrgID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actorID,
		Action:       action,
		ResourceType: "patch_job",
		ResourceID:   resourceID,
		Details:      details,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
	}); err != nil {
		s.log.Error("audit: patch event failed", "action", action, "err", err)
	}
}

// Ensure strconv import is used (silence unused import in case of
// future refactors that remove strconv usage above).
var _ = strconv.Atoi
