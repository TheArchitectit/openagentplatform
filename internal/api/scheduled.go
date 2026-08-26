package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/scheduled"
)

// handleListScheduledTasks returns all automated tasks for the requesting org.
func (s *Server) handleListScheduledTasks(w http.ResponseWriter, r *http.Request) {
	if s.scheduledStore == nil {
		http.Error(w, `{"error":"scheduled_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	tasks, err := s.scheduledStore.ListTasks(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
}

// handleGetScheduledTask returns a single task.
func (s *Server) handleGetScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledStore == nil {
		http.Error(w, `{"error":"scheduled_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	task, err := s.scheduledStore.GetTask(r.Context(), claims.OrgID, id)
	if err != nil {
		if errors.Is(err, scheduled.ErrNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

// handleCreateScheduledTask creates a new automated task.
func (s *Server) handleCreateScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledStore == nil {
		http.Error(w, `{"error":"scheduled_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := claims.OrgID
	var body struct {
		Name     string          `json:"name"`
		Enabled  bool            `json:"enabled"`
		CronExpr string          `json:"cron_expr"`
		Action   string          `json:"action"`
		Params   json.RawMessage `json:"params"`
		Timezone string          `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.CronExpr == "" || body.Action == "" {
		http.Error(w, `{"error":"name, cron_expr, and action are required"}`, http.StatusBadRequest)
		return
	}
	if !validAction(body.Action) {
		http.Error(w, `{"error":"unknown_action"}`, http.StatusBadRequest)
		return
	}
	task := &scheduled.TaskRecord{
		ID:       uuid.NewString(),
		OrgID:    orgID,
		Name:     body.Name,
		Enabled:  body.Enabled,
		CronExpr: strings.TrimSpace(body.CronExpr),
		Action:   body.Action,
		Params:   body.Params,
		Timezone: body.Timezone,
	}
	if err := s.scheduledStore.CreateTask(r.Context(), task); err != nil {
		// Fail-closed: an invalid cron_expr surfaces as 400.
		if strings.Contains(err.Error(), "invalid cron_expr") || strings.Contains(err.Error(), "compute next_run_at") {
			http.Error(w, `{"error":"invalid_cron_expr"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, `{"error":"create_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(task)
}

// handleUpdateScheduledTask patches a task.
func (s *Server) handleUpdateScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledStore == nil {
		http.Error(w, `{"error":"scheduled_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := claims.OrgID
	id := chi.URLParam(r, "id")
	existing, err := s.scheduledStore.GetTask(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, scheduled.ErrNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	var body struct {
		Name     *string          `json:"name"`
		Enabled  *bool            `json:"enabled"`
		CronExpr *string          `json:"cron_expr"`
		Action   *string          `json:"action"`
		Params   json.RawMessage  `json:"params"`
		Timezone *string          `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.Enabled != nil {
		existing.Enabled = *body.Enabled
	}
	if body.CronExpr != nil {
		existing.CronExpr = strings.TrimSpace(*body.CronExpr)
	}
	if body.Action != nil {
		if !validAction(*body.Action) {
			http.Error(w, `{"error":"unknown_action"}`, http.StatusBadRequest)
			return
		}
		existing.Action = *body.Action
	}
	if body.Params != nil {
		existing.Params = body.Params
	}
	if body.Timezone != nil {
		existing.Timezone = *body.Timezone
	}
	if err := s.scheduledStore.UpdateTask(r.Context(), existing); err != nil {
		if strings.Contains(err.Error(), "invalid cron_expr") || strings.Contains(err.Error(), "compute next_run_at") {
			http.Error(w, `{"error":"invalid_cron_expr"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(existing)
}

// handleDeleteScheduledTask removes a task.
func (s *Server) handleDeleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledStore == nil {
		http.Error(w, `{"error":"scheduled_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := claims.OrgID
	id := chi.URLParam(r, "id")
	if err := s.scheduledStore.DeleteTask(r.Context(), orgID, id); err != nil {
		if errors.Is(err, scheduled.ErrNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunScheduledTask triggers a task immediately, bypassing the schedule.
func (s *Server) handleRunScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledStore == nil || s.scheduledScheduler == nil {
		http.Error(w, `{"error":"scheduled_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := claims.OrgID
	id := chi.URLParam(r, "id")
	task, err := s.scheduledStore.GetTask(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, scheduled.ErrNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	// Fire via the scheduler's executor path.
	if err := s.scheduledScheduler.RunNow(r.Context(), task); err != nil {
		http.Error(w, `{"error":"run_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "fired"})
}

// validAction reports whether action is one of the supported task actions.
func validAction(action string) bool {
	switch action {
	case "patch_deploy", "reboot", "script_run", "check_enable":
		return true
	}
	return false
}

// mountScheduled wires scheduled-automation routes under /api/v1/scheduled.
func (s *Server) mountScheduled(r chi.Router) {
	r.Route("/scheduled", func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
		r.Get("/", s.handleListScheduledTasks)
		r.Post("/", s.handleCreateScheduledTask)
		r.Get("/{id}", s.handleGetScheduledTask)
		r.Patch("/{id}", s.handleUpdateScheduledTask)
		r.Delete("/{id}", s.handleDeleteScheduledTask)
		r.Post("/{id}/run", s.handleRunScheduledTask)
	})
}
