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

// signAgentCommand wraps an agent-bound command payload in a signed
// envelope (pkg/models.SignedCommand). Agents that hold the server's
// public key (delivered at registration as signing_key) refuse unsigned
// or forged commands. When no session minter is configured — tests only —
// the payload is published as-is.
func (s *Server) signAgentCommand(payload []byte) []byte {
	if s.sessionMinter == nil {
		return payload
	}
	sig, err := s.sessionMinter.SignPayload(payload)
	if err != nil {
		s.log.Warn("agent command signing failed; publishing unsigned", "err", err)
		return payload
	}
	wrapped, err := json.Marshal(models.SignedCommand{
		Payload:   payload,
		Algorithm: models.SignedCommandAlgorithm,
		Signature: sig,
	})
	if err != nil {
		s.log.Warn("agent command envelope marshal failed; publishing unsigned", "err", err)
		return payload
	}
	return wrapped
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

	// Tenant guard: every target agent must belong to the caller's org.
	// Without this check any authenticated user could execute arbitrary
	// scripts on another org's endpoints by supplying their agent IDs.
	allowed, rejected := s.filterAgentsInOrg(r.Context(), orgID, req.AgentIDs)
	if len(allowed) == 0 {
		s.recordAudit(r, "script.run.rejected", "script", id, map[string]any{"rejected_agents": rejected})
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"no_valid_agents"}`, http.StatusForbidden)
		return
	}

	actor := actorFromContext(r)
	now := time.Now().UTC()
	runIDs := make([]string, 0, len(allowed))
	for _, agentID := range allowed {
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
		// Publish a RunScript command to the agent's scripts subject
		// (pkg/agent.ScriptsSubject — the agent has no subscription on
		// the .commands subject), wrapped in a signed envelope. Script
		// bodies are arbitrary code executed at the agent's privilege;
		// NATS reachability alone must not be sufficient to command an
		// endpoint. The agent verifies the Ed25519 signature against the
		// public key delivered at registration (signing_key). Field
		// names follow pkg/agent.ScriptCommand's JSON contract.
		if s.eventBus != nil {
			cmd := map[string]any{
				"type":        "RunScript",
				"run_id":      run.ID,
				"script_id":   script.ID,
				"runtime":     script.Runtime,
				"script":      script.Body,
				"timeout_sec": script.TimeoutSeconds,
				"timestamp":   now.Unix(),
			}
			payload, _ := json.Marshal(cmd)
			data := s.signAgentCommand(payload)
			subject := fmt.Sprintf("oap.agents.%s.scripts", agentID)
			if err := s.eventBus.Publish(r.Context(), subject, data); err != nil {
				s.log.Warn("publish run-script failed", "agent_id", agentID, "err", err)
			}
		}
	}
	s.recordAudit(r, "script.run", "script", id, map[string]any{"run_ids": runIDs, "agents": allowed, "rejected_agents": rejected})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"script_id":       id,
		"run_ids":         runIDs,
		"queued_count":    len(runIDs),
		"rejected_agents": rejected,
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
