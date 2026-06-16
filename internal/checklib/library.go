// Package checklib provides the server-side check library catalog: a
// collection of CheckTemplate definitions representing the built-in check
// types agents know how to execute, and the HTTP handlers to list/instantiate
// them. The templates match the checker implementations registered in
// pkg/agent/checkers.
package checklib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// CheckTemplate is a built-in check definition that can be instantiated
// into a real check_definitions row. The "config" field contains the
// default parameters agents receive when they pick up the check.
type CheckTemplate struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	CheckType           string         `json:"check_type"`
	Description         string         `json:"description"`
	Category            string         `json:"category"`
	DefaultConfig       map[string]any `json:"default_config"`
	DefaultIntervalSecs int            `json:"default_interval_seconds"`
	DefaultTimeoutSecs  int            `json:"default_timeout_seconds"`
	ConfigSchema        map[string]any `json:"config_schema,omitempty"`
}

// Library is the catalog. It is a stateless service; the templates
// are returned from BuiltInChecks() on every call.
type Library struct {
	db *pgxpool.Pool // optional; the catalog itself is in-memory
}

// NewLibrary constructs a Library. db may be nil; the library still serves
// the catalog and the instantiate endpoint will return 503 if db is nil.
func NewLibrary(db *pgxpool.Pool) *Library {
	return &Library{db: db}
}

// BuiltInChecks returns the canonical set of CheckTemplate values that
// correspond to the checkers registered in pkg/agent/checkers. The order
// here is the order presented in the UI.
func BuiltInChecks() []CheckTemplate {
	return []CheckTemplate{
		{
			ID:                  "builtin-ping",
			Name:                "Ping",
			CheckType:           "ping",
			Description:         "ICMP ping a host. Useful for basic reachability and latency checks.",
			Category:            "network",
			DefaultConfig:       map[string]any{"host": "8.8.8.8", "count": 3, "timeout_ms": 3000},
			DefaultIntervalSecs: 60,
			DefaultTimeoutSecs:  10,
			ConfigSchema: map[string]any{
				"host":        map[string]any{"type": "string", "required": true, "description": "Hostname or IP to ping"},
				"count":       map[string]any{"type": "integer", "default": 3, "description": "Number of echo requests"},
				"timeout_ms":  map[string]any{"type": "integer", "default": 3000, "description": "Per-packet timeout"},
			},
		},
		{
			ID:                  "builtin-cpu",
			Name:                "CPU Usage",
			CheckType:           "cpu",
			Description:         "Sample CPU utilisation over a window and alert when it exceeds a threshold.",
			Category:            "system",
			DefaultConfig:       map[string]any{"threshold_percent": 90, "duration_seconds": 60},
			DefaultIntervalSecs: 60,
			DefaultTimeoutSecs:  15,
			ConfigSchema: map[string]any{
				"threshold_percent": map[string]any{"type": "number", "default": 90, "min": 1, "max": 100},
				"duration_seconds":  map[string]any{"type": "integer", "default": 60, "min": 1},
			},
		},
		{
			ID:                  "builtin-memory",
			Name:                "Memory Usage",
			CheckType:           "memory",
			Description:         "Alert when available memory drops below a percentage threshold.",
			Category:            "system",
			DefaultConfig:       map[string]any{"threshold_percent": 90},
			DefaultIntervalSecs: 60,
			DefaultTimeoutSecs:  10,
			ConfigSchema: map[string]any{
				"threshold_percent": map[string]any{"type": "number", "default": 90, "min": 1, "max": 100},
			},
		},
		{
			ID:                  "builtin-disk",
			Name:                "Disk Usage",
			CheckType:           "disk",
			Description:         "Alert when free space on a path drops below a percentage threshold.",
			Category:            "system",
			DefaultConfig:       map[string]any{"path": "/", "threshold_percent": 90},
			DefaultIntervalSecs: 300,
			DefaultTimeoutSecs:  10,
			ConfigSchema: map[string]any{
				"path":              map[string]any{"type": "string", "default": "/", "description": "Filesystem path to check"},
				"threshold_percent": map[string]any{"type": "number", "default": 90, "min": 1, "max": 100},
			},
		},
		{
			ID:                  "builtin-service",
			Name:                "Service Status",
			CheckType:           "service",
			Description:         "Check that a system service is in the expected state (e.g. running).",
			Category:            "system",
			DefaultConfig:       map[string]any{"service_name": "", "expected_state": "running"},
			DefaultIntervalSecs: 60,
			DefaultTimeoutSecs:  10,
			ConfigSchema: map[string]any{
				"service_name":  map[string]any{"type": "string", "required": true, "description": "systemd unit name (without .service)"},
				"expected_state": map[string]any{"type": "string", "default": "running", "enum": []string{"running", "stopped"}},
			},
		},
	}
}

// FindTemplate returns the template with the given id, or an error.
func FindTemplate(id string) (CheckTemplate, error) {
	for _, t := range BuiltInChecks() {
		if t.ID == id {
			return t, nil
		}
	}
	return CheckTemplate{}, fmt.Errorf("template not found: %s", id)
}

// GetTemplateByName returns the first template whose Name matches (case-insensitive),
// used by the seeder to look up built-in checks by canonical name.
func GetTemplateByName(name string) (CheckTemplate, bool) {
	for _, t := range BuiltInChecks() {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return CheckTemplate{}, false
}

// handleListLibrary returns the full catalog of built-in templates.
func (l *Library) handleListLibrary(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"templates": BuiltInChecks(),
		"total":     len(BuiltInChecks()),
	})
}

// handleInstantiateFromTemplate creates a check_definitions row from a
// built-in template. The request body may override the default config,
// name, description, interval, and timeout. The new check is created
// in the authenticated user's org (or empty org_id if unauthenticated).
func (l *Library) handleInstantiateFromTemplate(w http.ResponseWriter, r *http.Request) {
	if l.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	templateID := chi.URLParam(r, "template_id")
	if templateID == "" {
		http.Error(w, `{"error":"missing_template_id"}`, http.StatusBadRequest)
		return
	}
	tmpl, err := FindTemplate(templateID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"template_not_found","detail":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	var req struct {
		Name            string         `json:"name"`
		Description     string         `json:"description"`
		Config          map[string]any `json:"config"`
		IntervalSeconds int            `json:"interval_seconds"`
		TimeoutSeconds  int            `json:"timeout_seconds"`
		Enabled         *bool          `json:"enabled"`
	}
	// Body is optional; an empty body falls back to template defaults.
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Merge user overrides on top of template defaults.
	cfg := map[string]any{}
	for k, v := range tmpl.DefaultConfig {
		cfg[k] = v
	}
	for k, v := range req.Config {
		cfg[k] = v
	}

	name := req.Name
	if name == "" {
		name = tmpl.Name
	}
	desc := req.Description
	if desc == "" {
		desc = tmpl.Description
	}
	interval := req.IntervalSeconds
	if interval <= 0 {
		interval = tmpl.DefaultIntervalSecs
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = tmpl.DefaultTimeoutSecs
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, `{"error":"config_marshal_failed"}`, http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	const q = `
		INSERT INTO check_definitions (
			id, org_id, name, description, check_type, config,
			interval_seconds, timeout_seconds, enabled, created_at, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2,''), ''), $3, $4, $5, $6,
			$7, $8, $9, $10, $10
		)
		RETURNING created_at, updated_at
	`
	var createdAt, updatedAt time.Time
	err = l.db.QueryRow(r.Context(), q,
		id, orgID, name, desc, tmpl.CheckType, cfgJSON,
		interval, timeout, enabled, now,
	).Scan(&createdAt, &updatedAt)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"insert_failed","detail":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":               id,
		"template_id":      tmpl.ID,
		"name":             name,
		"description":      desc,
		"check_type":       tmpl.CheckType,
		"config":           cfg,
		"interval_seconds": interval,
		"timeout_seconds":  timeout,
		"enabled":          enabled,
		"created_at":       createdAt,
		"updated_at":       updatedAt,
	})
}

// RegisterRoutes mounts the library endpoints on r. The library must be
// installed under the authenticated API group by the caller.
func (l *Library) RegisterRoutes(r chi.Router) {
	r.Route("/library", func(r chi.Router) {
		r.Get("/", l.handleListLibrary)
		r.Route("/{template_id}", func(r chi.Router) {
			r.Post("/create", l.handleInstantiateFromTemplate)
		})
	})
}

// ErrNoDB is returned by helpers that require a database connection.
var ErrNoDB = errors.New("checklib: database not configured")
// ensure context import is used (keep compiler happy if helpers grow)
var _ = context.Background
