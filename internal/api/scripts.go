package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// scriptStore is the interface the API uses to read/write script definitions
// and script run records. The default Postgres implementation is pgScriptStore.
type scriptStore interface {
	InsertScript(ctx context.Context, s *models.ScriptDefinition) error
	GetScript(ctx context.Context, id string) (*models.ScriptDefinition, error)
	ListScripts(ctx context.Context, f ScriptListFilter) ([]models.ScriptDefinition, int, error)
	UpdateScript(ctx context.Context, id string, patch ScriptPatch) (*models.ScriptDefinition, error)
	DeleteScript(ctx context.Context, id string) error
	InsertScriptRun(ctx context.Context, run *models.ScriptRun) error
	GetScriptRun(ctx context.Context, id string) (*models.ScriptRun, error)
	ListScriptRuns(ctx context.Context, f ScriptRunListFilter) ([]models.ScriptRun, int, error)
	UpdateScriptRunOutput(ctx context.Context, run *models.ScriptRun) error
}

// scriptStoreFn returns the active store. Uses the wired-in store if
// available (set via SetScriptStore), otherwise falls back to a
// pgScriptStore backed by the server's DB pool.
func (s *Server) scriptStoreFn() scriptStore {
	if s.scriptStore != nil {
		return s.scriptStore
	}
	return &pgScriptStore{pool: s.db}
}

// validScriptRuntimes is the whitelist of allowed script runtimes.
var validScriptRuntimes = map[string]bool{
	"bash":       true,
	"powershell": true,
	"python":     true,
	"node":       true,
}

// handleCreateScript validates and persists a new script definition.
func (s *Server) handleCreateScript(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		Runtime        string   `json:"runtime"`
		ScriptBody     string   `json:"script_body"`
		TimeoutSeconds int      `json:"timeout_seconds"`
		Enabled        *bool    `json:"enabled"`
		Tags           []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name_required"}`, http.StatusBadRequest)
		return
	}
	if !validScriptRuntimes[req.Runtime] {
		http.Error(w, `{"error":"invalid_runtime","allowed":["bash","powershell","python","node"]}`, http.StatusBadRequest)
		return
	}
	if req.ScriptBody == "" {
		http.Error(w, `{"error":"script_body_required"}`, http.StatusBadRequest)
		return
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	orgID := ""
	if claims, ok := authFromCtx(r); ok && claims != nil {
		orgID = claims.OrgID
	}
	now := time.Now().UTC()
	script := &models.ScriptDefinition{
		ID:             uuid.NewString(),
		OrgID:          orgID,
		Name:           req.Name,
		Description:    req.Description,
		Runtime:        req.Runtime,
		ScriptBody:     req.ScriptBody,
		TimeoutSeconds: req.TimeoutSeconds,
		Enabled:        enabled,
		Tags:           req.Tags,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.scriptStoreFn().InsertScript(r.Context(), script); err != nil {
		s.log.Error("insert script failed", "err", err)
		http.Error(w, `{"error":"insert_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "script.create", "script", script.ID, map[string]any{"runtime": script.Runtime, "name": script.Name})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(script)
}

// handleListScripts returns paginated, filterable script definitions.
func (s *Server) handleListScripts(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
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
	var enabled *bool
	if es := q.Get("enabled"); es != "" {
		b, err := strconv.ParseBool(es)
		if err == nil {
			enabled = &b
		}
	}
	filter := ScriptListFilter{
		Runtime: q.Get("runtime"),
		Enabled: enabled,
		Tag:     q.Get("tag"),
		Search:  q.Get("search"),
		Limit:   limit,
		Offset:  offset,
	}
	scripts, total, err := s.scriptStoreFn().ListScripts(r.Context(), filter)
	if err != nil {
		s.log.Error("list scripts failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if scripts == nil {
		scripts = []models.ScriptDefinition{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"scripts": scripts,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// handleGetScript returns one script with its recent run history summary.
func (s *Server) handleGetScript(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	store := s.scriptStoreFn()
	script, err := store.GetScript(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrScriptNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get script failed", "err", err)
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	// Run history summary: last 5 runs.
	runs, _, err := store.ListScriptRuns(r.Context(), ScriptRunListFilter{
		ScriptID: id,
		Limit:    5,
		Offset:   0,
	})
	if err != nil {
		s.log.Warn("list script runs failed", "id", id, "err", err)
		runs = []models.ScriptRun{}
	}
	if runs == nil {
		runs = []models.ScriptRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"script":      script,
		"recent_runs": runs,
	})
}

// handleUpdateScript applies a partial update to a script definition.
func (s *Server) handleUpdateScript(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	patch := ScriptPatch{}
	if v, ok := req["name"].(string); ok {
		patch.Name = &v
	}
	if v, ok := req["description"].(string); ok {
		patch.Description = &v
	}
	if v, ok := req["script_body"].(string); ok {
		patch.ScriptBody = &v
	}
	if v, ok := req["runtime"].(string); ok {
		if !validScriptRuntimes[v] {
			http.Error(w, `{"error":"invalid_runtime"}`, http.StatusBadRequest)
			return
		}
		patch.Runtime = &v
	}
	if v, ok := req["timeout_seconds"]; ok {
		n, err := toInt(v)
		if err != nil {
			http.Error(w, `{"error":"invalid_timeout_seconds"}`, http.StatusBadRequest)
			return
		}
		patch.TimeoutSeconds = &n
	}
	if v, ok := req["enabled"].(bool); ok {
		patch.Enabled = &v
	}
	if v, ok := req["tags"]; ok {
		if as, ok := v.([]any); ok {
			tags := make([]string, 0, len(as))
			for _, a := range as {
				if s, ok := a.(string); ok {
					tags = append(tags, s)
				}
			}
			patch.Tags = tags
		}
	}
	updated, err := s.scriptStoreFn().UpdateScript(r.Context(), id, patch)
	if err != nil {
		if errors.Is(err, ErrScriptNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("update script failed", "err", err)
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "script.update", "script", id, nil)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// handleDeleteScript soft-deletes a script definition.
func (s *Server) handleDeleteScript(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	if err := s.scriptStoreFn().DeleteScript(r.Context(), id); err != nil {
		if errors.Is(err, ErrScriptNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("delete script failed", "err", err)
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "script.delete", "script", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleRunScript enqueues a script run on the specified agent(s). Body:
// { "agent_ids": ["..."] }. Returns the list of created run_ids.
func (s *Server) handleRunScript(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		AgentIDs []string `json:"agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if len(req.AgentIDs) == 0 {
		http.Error(w, `{"error":"agent_ids_required"}`, http.StatusBadRequest)
		return
	}
	store := s.scriptStoreFn()
	script, err := store.GetScript(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrScriptNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	if !script.Enabled {
		http.Error(w, `{"error":"script_disabled"}`, http.StatusConflict)
		return
	}

	actor := actorFromContext(r)
	now := time.Now().UTC()
	runIDs := make([]string, 0, len(req.AgentIDs))
	for _, agentID := range req.AgentIDs {
		run := &models.ScriptRun{
			ID:          uuid.NewString(),
			ScriptID:    script.ID,
			AgentID:     agentID,
			Status:      "pending",
			TriggeredBy: actor,
			Scheduled:   false,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := store.InsertScriptRun(r.Context(), run); err != nil {
			s.log.Warn("insert script run failed", "script_id", script.ID, "agent_id", agentID, "err", err)
			continue
		}
		runIDs = append(runIDs, run.ID)
		// Publish a RunScript command to the agent's NATS subject.
		if s.eventBus != nil {
			cmd := map[string]any{
				"type":            "RunScript",
				"run_id":          run.ID,
				"script_id":       script.ID,
				"runtime":         script.Runtime,
				"script_body":     script.ScriptBody,
				"timeout_seconds": script.TimeoutSeconds,
				"timestamp":       now.Unix(),
			}
			payload, _ := json.Marshal(cmd)
			subject := fmt.Sprintf("oap.agents.%s.commands", agentID)
			if err := s.eventBus.Publish(r.Context(), subject, payload); err != nil {
				s.log.Warn("publish run-script failed", "agent_id", agentID, "err", err)
			}
		}
	}
	s.recordAudit(r, "script.run", "script", id, map[string]any{"run_ids": runIDs, "agents": req.AgentIDs})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"script_id":   id,
		"run_ids":     runIDs,
		"queued_count": len(runIDs),
	})
}

// handleListScriptRuns returns the run history for a script, paginated and
// filterable by agent_id and status.
func (s *Server) handleListScriptRuns(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
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
	filter := ScriptRunListFilter{
		ScriptID: id,
		AgentID:  q.Get("agent_id"),
		Status:   q.Get("status"),
		Limit:    limit,
		Offset:   offset,
	}
	runs, total, err := s.scriptStoreFn().ListScriptRuns(r.Context(), filter)
	if err != nil {
		s.log.Error("list script runs failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []models.ScriptRun{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"script_id": id,
		"runs":      runs,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

// handleGetScriptRun returns a single script run with full output.
func (s *Server) handleGetScriptRun(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	runID := chi.URLParam(r, "run_id")
	if runID == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	run, err := s.scriptStoreFn().GetScriptRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, ErrScriptRunNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get script run failed", "err", err)
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(run)
}
