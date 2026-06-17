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
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// checkStore is the interface the API uses to read/write check definitions
// and assignments. The default Postgres implementation is pgCheckStore.
type checkStore interface {
	InsertCheck(ctx context.Context, c *models.CheckDefinition) error
	GetCheck(ctx context.Context, id string) (*models.CheckDefinition, error)
	ListChecks(ctx context.Context, f CheckListFilter) ([]models.CheckDefinition, int, error)
	UpdateCheck(ctx context.Context, id string, patch CheckPatch) (*models.CheckDefinition, error)
	DeleteCheck(ctx context.Context, id string) error
	CountAssignments(ctx context.Context, checkID string) (int, error)
	AssignCheck(ctx context.Context, a *models.CheckAssignment) error
	AssignCheckToSite(ctx context.Context, checkID, siteID, assignedBy string) (int, error)
	RemoveAssignment(ctx context.Context, checkID, agentID string) error
	ListAssignments(ctx context.Context, checkID string) ([]models.CheckAssignmentDetail, error)
	GetAssignmentsForAgent(ctx context.Context, agentID string) ([]string, error)
}

// checkStoreFunc returns the active store. Wrapped in a method so tests can
// swap in an in-memory store.
func (s *Server) checkStore() checkStore {
	return &pgCheckStore{pool: s.db}
}

// Valid check types. The config schema is validated against this list.
// validCheckTypes lists the check types the API accepts.
// Note: "custom" is intentionally absent — there is no handler for it
// in pkg/agent/checkers/registry.go. Custom checks should be expressed
// as "script" (with a script body in the config) or as a new dedicated
// checker that is registered in the registry. Re-introduce "custom"
// only once a corresponding checker implementation exists.
var validCheckTypes = map[string]bool{
	"ping":    true,
	"http":    true,
	"tcp":     true,
	"dns":     true,
	"cpu":     true,
	"memory":  true,
	"disk":    true,
	"service": true,
	"script":  true,
}

// validateCheckConfig applies the per-type schema and defaults from the
// task spec. Returns the canonicalised config (with defaults applied) and
// an error explaining the first violation.
func validateCheckConfig(checkType string, raw map[string]any) (map[string]any, error) {
	if !validCheckTypes[checkType] {
		return nil, fmt.Errorf("invalid check_type: %s", checkType)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	switch checkType {
	case "ping":
		host, _ := raw["host"].(string)
		if host == "" {
			return nil, errors.New("ping: host is required")
		}
		count := getIntDefault(raw, "count", 3)
		timeout := getIntDefault(raw, "timeout_ms", 3000)
		return map[string]any{"host": host, "count": count, "timeout_ms": timeout}, nil
	case "http":
		url, _ := raw["url"].(string)
		if url == "" {
			return nil, errors.New("http: url is required")
		}
		method, _ := raw["method"].(string)
		if method == "" {
			method = "GET"
		}
		expStatus := getIntDefault(raw, "expected_status", 200)
		expBody, _ := raw["expected_body"].(string)
		timeout := getIntDefault(raw, "timeout_ms", 5000)
		follow := true
		if v, ok := raw["follow_redirects"]; ok {
			if b, ok := v.(bool); ok {
				follow = b
			}
		}
		out := map[string]any{
			"url": url, "method": method, "expected_status": expStatus,
			"timeout_ms": timeout, "follow_redirects": follow,
		}
		if expBody != "" {
			out["expected_body"] = expBody
		}
		return out, nil
	case "tcp":
		host, _ := raw["host"].(string)
		portF, _ := raw["port"].(float64)
		port := int(portF)
		if host == "" || port == 0 {
			return nil, errors.New("tcp: host and port are required")
		}
		timeout := getIntDefault(raw, "timeout_ms", 5000)
		return map[string]any{"host": host, "port": port, "timeout_ms": timeout}, nil
	case "dns":
		hostname, _ := raw["hostname"].(string)
		if hostname == "" {
			return nil, errors.New("dns: hostname is required")
		}
		out := map[string]any{"hostname": hostname}
		if v, ok := raw["expected_ips"]; ok {
			out["expected_ips"] = v
		}
		if ns, _ := raw["nameserver"].(string); ns != "" {
			out["nameserver"] = ns
		}
		return out, nil
	case "cpu":
		return map[string]any{
			"threshold_percent": getFloatDefault(raw, "threshold_percent", 90),
			"duration_seconds":  getIntDefault(raw, "duration_seconds", 60),
		}, nil
	case "memory":
		return map[string]any{
			"threshold_percent": getFloatDefault(raw, "threshold_percent", 90),
		}, nil
	case "disk":
		path, _ := raw["path"].(string)
		if path == "" {
			path = "/"
		}
		return map[string]any{
			"path":              path,
			"threshold_percent": getFloatDefault(raw, "threshold_percent", 90),
		}, nil
	case "service":
		svc, _ := raw["service_name"].(string)
		if svc == "" {
			return nil, errors.New("service: service_name is required")
		}
		state, _ := raw["expected_state"].(string)
		if state == "" {
			state = "running"
		}
		return map[string]any{"service_name": svc, "expected_state": state}, nil
	case "script":
		rt, _ := raw["runtime"].(string)
		body, _ := raw["script_body"].(string)
		if rt == "" || body == "" {
			return nil, errors.New("script: runtime and script_body are required")
		}
		return map[string]any{
			"runtime":         rt,
			"script_body":     body,
			"timeout_seconds": getIntDefault(raw, "timeout_seconds", 30),
		}, nil
	}
	return nil, fmt.Errorf("unhandled check_type: %s", checkType)
}

func getIntDefault(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return def
}

func getFloatDefault(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return def
}

// handleCreateCheck validates and persists a new check definition.
func (s *Server) handleCreateCheck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Name            string         `json:"name"`
		Description     string         `json:"description"`
		CheckType       string         `json:"check_type"`
		Config          map[string]any `json:"config"`
		IntervalSeconds int            `json:"interval_seconds"`
		TimeoutSeconds  int            `json:"timeout_seconds"`
		Enabled         *bool          `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name_required"}`, http.StatusBadRequest)
		return
	}
	if req.CheckType == "" {
		http.Error(w, `{"error":"check_type_required"}`, http.StatusBadRequest)
		return
	}
	cfg, err := validateCheckConfig(req.CheckType, req.Config)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid_config","detail":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	if req.IntervalSeconds <= 0 {
		req.IntervalSeconds = 60
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	now := time.Now().UTC()
	c := &models.CheckDefinition{
		ID:              uuid.NewString(),
		OrgID:           orgID,
		Name:            req.Name,
		Description:     req.Description,
		CheckType:       req.CheckType,
		Config:          cfg,
		IntervalSeconds: req.IntervalSeconds,
		TimeoutSeconds:  req.TimeoutSeconds,
		Enabled:         enabled,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.checkStore().InsertCheck(r.Context(), c); err != nil {
		s.log.Error("insert check failed", "err", err)
		http.Error(w, `{"error":"insert_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "check.create", "check", c.ID, map[string]any{"check_type": c.CheckType, "name": c.Name})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(c)
}

// handleListChecks returns paginated, filterable check definitions.
func (s *Server) handleListChecks(w http.ResponseWriter, r *http.Request) {
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
	filter := CheckListFilter{
		CheckType: q.Get("check_type"),
		Enabled:   enabled,
		Search:    q.Get("search"),
		Limit:     limit,
		Offset:    offset,
	}
	checks, total, err := s.checkStore().ListChecks(r.Context(), filter)
	if err != nil {
		s.log.Error("list checks failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if checks == nil {
		checks = []models.CheckDefinition{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"checks": checks,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// handleGetCheck returns one check with its current assignment count.
func (s *Server) handleGetCheck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	store := s.checkStore()
	c, err := store.GetCheck(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get check failed", "err", err)
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	count, err := store.CountAssignments(r.Context(), id)
	if err != nil {
		s.log.Warn("count assignments failed", "id", id, "err", err)
		count = 0
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"check":            c,
		"assignment_count": count,
	})
}

// handleUpdateCheck applies a partial update to a check definition.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	// Read full body first to be able to validate config after we know the
	// (possibly unchanged) check_type.
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	store := s.checkStore()
	existing, err := store.GetCheck(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	patch := CheckPatch{}
	if v, ok := req["name"].(string); ok {
		patch.Name = &v
	}
	if v, ok := req["description"].(string); ok {
		patch.Description = &v
	}
	if v, ok := req["interval_seconds"]; ok {
		n, err := toInt(v)
		if err != nil {
			http.Error(w, `{"error":"invalid_interval_seconds"}`, http.StatusBadRequest)
			return
		}
		patch.IntervalSeconds = &n
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
	if raw, ok := req["config"].(map[string]any); ok {
		validated, err := validateCheckConfig(existing.CheckType, raw)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid_config","detail":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		patch.Config = validated
	}
	updated, err := store.UpdateCheck(r.Context(), id, patch)
	if err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("update check failed", "err", err)
		http.Error(w, `{"error":"update_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "check.update", "check", id, nil)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// handleDeleteCheck removes a check. If active assignments exist the
// request is rejected with 409 Conflict and the count is returned so the
// UI can prompt the user to unassign first.
func (s *Server) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	store := s.checkStore()
	if _, err := store.GetCheck(r.Context(), id); err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	count, err := store.CountAssignments(r.Context(), id)
	if err != nil {
		s.log.Warn("count assignments failed", "id", id, "err", err)
	}
	if count > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":            "has_assignments",
			"assignment_count": count,
			"detail":           "remove all assignments before deleting this check",
		})
		return
	}
	if err := store.DeleteCheck(r.Context(), id); err != nil {
		s.log.Error("delete check failed", "err", err)
		http.Error(w, `{"error":"delete_failed"}`, http.StatusInternalServerError)
		return
	}
	s.recordAudit(r, "check.delete", "check", id, nil)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// handleRunCheckNow queues a check for immediate execution on all
// assigned agents. It looks up the assignment list and publishes a
// "RunCheck" command to each agent's NATS subject. Returns the list of
// agents that were signalled.
func (s *Server) handleRunCheckNow(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	store := s.checkStore()
	if _, err := store.GetCheck(r.Context(), id); err != nil {
		if errors.Is(err, ErrCheckNotFound) {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"get_failed"}`, http.StatusInternalServerError)
		return
	}
	assignments, err := store.ListAssignments(r.Context(), id)
	if err != nil {
		s.log.Error("list assignments failed", "err", err)
		http.Error(w, `{"error":"list_assignments_failed"}`, http.StatusInternalServerError)
		return
	}
	signalled := make([]string, 0, len(assignments))
	if s.eventBus != nil {
		cmd := map[string]any{
			"type":      "RunCheck",
			"check_id":  id,
			"timestamp": time.Now().UTC().Unix(),
		}
		payload, _ := json.Marshal(cmd)
		for _, a := range assignments {
			subject := fmt.Sprintf("oap.agents.%s.commands", a.AgentID)
			if err := s.eventBus.Publish(r.Context(), subject, payload); err != nil {
				s.log.Warn("publish run-check failed", "agent_id", a.AgentID, "err", err)
				continue
			}
			signalled = append(signalled, a.AgentID)
		}
	} else {
		// No event bus configured — surface the assignment list as the
		// "would-have-been-signalled" set so the UI can still show feedback.
		for _, a := range assignments {
			signalled = append(signalled, a.AgentID)
		}
	}
	s.recordAudit(r, "check.run_now", "check", id, map[string]any{"agents": signalled})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"check_id":     id,
		"queued_count": len(signalled),
		"agents":       signalled,
	})
}

// toInt accepts any numeric value from a JSON decode and returns int.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case string:
		return strconv.Atoi(n)
	}
	return 0, fmt.Errorf("not a number: %T", v)
}

// recordAudit writes a check-related audit event. Best-effort: a nil
// audit service or transient DB error does not fail the request.
func (s *Server) recordAudit(r *http.Request, action, resourceType, resourceID string, details map[string]any) {
	if s.audit == nil {
		return
	}
	actorID := ""
	orgID := ""
	siteID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		actorID = claims.Subject
		orgID = claims.OrgID
		siteID = claims.SiteID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
		SiteID:       siteID,
	}); err != nil {
		s.log.Error("audit: check event failed", "action", action, "err", err)
	}
}
