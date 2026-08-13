package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

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
				"script_body":     script.Body,
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
		"script_id":    id,
		"run_ids":      runIDs,
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
