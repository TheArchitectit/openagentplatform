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
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// scriptStore is the interface the API uses to read/write script definitions
// and script run records. The default Postgres implementation is pgScriptStore.
type scriptStore interface {
	InsertScript(ctx context.Context, s *models.ScriptDefinition) error
	GetScript(ctx context.Context, orgID, id string) (*models.ScriptDefinition, error)
	ListScripts(ctx context.Context, f ScriptListFilter) ([]models.ScriptDefinition, int, error)
	UpdateScript(ctx context.Context, orgID, id string, patch ScriptPatch) (*models.ScriptDefinition, error)
	DeleteScript(ctx context.Context, orgID, id string) error
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
		Body:           req.ScriptBody,
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
	if claims, ok := authFromCtx(r); ok && claims != nil {
		filter.OrgID = claims.OrgID
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
	orgID := ""
	if claims, ok := authFromCtx(r); ok && claims != nil {
		orgID = claims.OrgID
	}
	script, err := store.GetScript(r.Context(), orgID, id)
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
