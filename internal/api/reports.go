// Package api - reports.go implements the HTTP endpoints for the
// enterprise reporting system: templates, runs, and schedules.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/reports"
)

// reportStore returns the reports Store wired into the Server.
// If no store is configured, the endpoints return 503.
func (s *Server) reportStore() reports.Store {
	if s.reportsStore == nil {
		return nil
	}
	return s.reportsStore
}

// reportScheduler returns the Scheduler wired into the Server.
func (s *Server) reportScheduler() *reports.Scheduler {
	return s.reportsScheduler
}

// listReportTemplates handles GET /api/v1/reports/templates.
// Returns all report templates for the caller's org.
func (s *Server) listReportTemplates(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	if store == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	templates, err := store.ListTemplates(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"templates": templates,
		"built_in":  reports.AllTemplates,
	})
}

// generateReport handles POST /api/v1/reports/generate.
// Body: { "template_id": "...", "format": "json|csv|pdf",
//         "delivery_method": "email|webhook|download",
//         "delivery_target": "...", "params": {...} }
func (s *Server) generateReport(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	sched := s.reportScheduler()
	if store == nil || sched == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		TemplateID     string            `json:"template_id"`
		Format         string            `json:"format"`
		DeliveryMethod string            `json:"delivery_method"`
		DeliveryTarget string            `json:"delivery_target"`
		Params         map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.TemplateID == "" {
		http.Error(w, `{"error":"template_id is required"}`, http.StatusBadRequest)
		return
	}
	if !reports.AllTemplatesContains(body.TemplateID) {
		http.Error(w, `{"error":"unknown template_id"}`, http.StatusBadRequest)
		return
	}

	run, err := sched.RunNow(r.Context(), claims.OrgID, body.TemplateID, body.Format, body.DeliveryMethod, body.DeliveryTarget, body.Params)
	if err != nil {
		http.Error(w, `{"error":"failed to start report run"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// listReportRuns handles GET /api/v1/reports/runs.
// Query: limit, offset
func (s *Server) listReportRuns(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	if store == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	runs, err := store.ListRuns(r.Context(), claims.OrgID, limit, offset)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// getReportRun handles GET /api/v1/reports/runs/{id}.
func (s *Server) getReportRun(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	if store == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	run, err := store.GetRun(r.Context(), claims.OrgID, id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// createReportSchedule handles POST /api/v1/reports/schedules.
// Body: { "template_id": "...", "cron_expr": "0 9 * * *",
//         "format": "json", "delivery_method": "email",
//         "delivery_target": "...", "params": {...} }
func (s *Server) createReportSchedule(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	sched := s.reportScheduler()
	if store == nil || sched == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		TemplateID     string            `json:"template_id"`
		CronExpr       string            `json:"cron_expr"`
		Format         string            `json:"format"`
		DeliveryMethod string            `json:"delivery_method"`
		DeliveryTarget string            `json:"delivery_target"`
		Params         map[string]string `json:"params"`
		Enabled        *bool             `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.TemplateID == "" {
		http.Error(w, `{"error":"template_id is required"}`, http.StatusBadRequest)
		return
	}
	if body.CronExpr == "" {
		http.Error(w, `{"error":"cron_expr is required"}`, http.StatusBadRequest)
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	paramsJSON, _ := json.Marshal(body.Params)
	schedule := &reports.ReportSchedule{
		OrgID:          claims.OrgID,
		TemplateID:     body.TemplateID,
		CronExpr:       body.CronExpr,
		Format:         reports.ReportFormat(body.Format),
		Params:         paramsJSON,
		DeliveryMethod: body.DeliveryMethod,
		DeliveryTarget: body.DeliveryTarget,
		Enabled:        enabled,
	}

	if err := sched.AddSchedule(r.Context(), schedule); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

// listReportSchedules handles GET /api/v1/reports/schedules.
func (s *Server) listReportSchedules(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	if store == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	schedules, err := store.ListSchedules(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": schedules})
}

// deleteReportSchedule handles DELETE /api/v1/reports/schedules/{id}.
func (s *Server) deleteReportSchedule(w http.ResponseWriter, r *http.Request) {
	store := s.reportStore()
	sched := s.reportScheduler()
	if store == nil || sched == nil {
		http.Error(w, `{"error":"reports unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	if err := sched.RemoveSchedule(r.Context(), claims.OrgID, id); err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
