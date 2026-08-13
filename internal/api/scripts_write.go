package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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
	orgID := ""
	if claims, ok := authFromCtx(r); ok && claims != nil {
		orgID = claims.OrgID
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
		patch.Body = &v
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
	updated, err := s.scriptStoreFn().UpdateScript(r.Context(), orgID, id, patch)
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
	orgID := ""
	if claims, ok := authFromCtx(r); ok && claims != nil {
		orgID = claims.OrgID
	}
	if err := s.scriptStoreFn().DeleteScript(r.Context(), orgID, id); err != nil {
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
