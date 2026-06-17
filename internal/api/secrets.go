package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

// secretsResolveRequest is the JSON body for POST /api/v1/secrets/resolve.
type secretsResolveRequest struct {
	// URI is the OAP secret reference, e.g. ref:oap://env/workspace/path?key=foo
	URI string `json:"uri"`
}

// secretsResolveResponse is the JSON body returned from
// POST /api/v1/secrets/resolve. Sensitive fields are redacted.
type secretsResolveResponse struct {
	Path     string         `json:"path"`
	Version  int            `json:"version"`
	Redacted map[string]any `json:"data"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// secretsHealthEntry reports the health of a single backend.
type secretsHealthEntry struct {
	Backend string `json:"backend"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// secretsHealthResponse is the JSON body for GET /api/v1/secrets/health.
type secretsHealthResponse struct {
	Backends []secretsHealthEntry `json:"backends"`
}

// secretsBackendsResponse is the JSON body for GET /api/v1/secrets/backends.
type secretsBackendsResponse struct {
	Backends []string `json:"backends"`
}

// handleSecretsHealth reports health for every registered secret backend.
// GET /api/v1/secrets/health
func (s *Server) handleSecretsHealth(w http.ResponseWriter, r *http.Request) {
	if s.secretsResolver == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "secrets_not_configured",
		})
		return
	}

	backends := s.secretsResolver.Backends()
	resp := secretsHealthResponse{Backends: make([]secretsHealthEntry, 0, len(backends))}

	for _, name := range backends {
		b, ok := s.secretsResolver.BackendFor(name)
		entry := secretsHealthEntry{Backend: name}
		if !ok {
			entry.Status = "unavailable"
			entry.Error = "backend not found in registry"
			resp.Backends = append(resp.Backends, entry)
			continue
		}
		hcCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err := b.Healthcheck(hcCtx)
		cancel()
		if err != nil {
			entry.Status = "unhealthy"
			entry.Error = err.Error()
		} else {
			entry.Status = "healthy"
		}
		resp.Backends = append(resp.Backends, entry)
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSecretsResolve resolves a single secret reference and returns the
// result with sensitive values redacted.
// POST /api/v1/secrets/resolve
func (s *Server) handleSecretsResolve(w http.ResponseWriter, r *http.Request) {
	if s.secretsResolver == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "secrets_not_configured",
		})
		return
	}

	var req secretsResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid_request_body",
		})
		return
	}
	if req.URI == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "uri_required",
		})
		return
	}

	// Build an AuthContext from the current session/identity so the
	// resolver can perform hierarchy-based access checks.
	authCtx := buildSecretsAuthContext(r, s)

	val, err := s.secretsResolver.Resolve(r.Context(), req.URI, authCtx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "resolve_failed",
			"detail": err.Error(),
		})
		return
	}

	resp := secretsResolveResponse{
		Path:     val.Path,
		Version:  val.Version,
		Redacted: redactSecretData(val.Data),
		Metadata: map[string]any{
			"created_at": val.CreatedAt,
		},
	}
	if val.Metadata.TTL > 0 {
		resp.Metadata["ttl"] = val.Metadata.TTL.String()
	}
	if val.Metadata.LeaseID != "" {
		resp.Metadata["lease_id"] = val.Metadata.LeaseID
	}
	if val.Metadata.LeaseDuration > 0 {
		resp.Metadata["lease_duration"] = val.Metadata.LeaseDuration.String()
	}
	if val.Metadata.IsDynamic {
		resp.Metadata["is_dynamic"] = true
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSecretsBackends returns the names of all configured secret backends.
// GET /api/v1/secrets/backends
func (s *Server) handleSecretsBackends(w http.ResponseWriter, r *http.Request) {
	if s.secretsResolver == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "secrets_not_configured",
		})
		return
	}

	resp := secretsBackendsResponse{
		Backends: s.secretsResolver.Backends(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// buildSecretsAuthContext extracts identity information from the request
// and builds a resolver AuthContext. When the request is unauthenticated a
// minimal context with only ClientID set is returned (the resolver will
// reject cross-tenant accesses during authorisation).
func buildSecretsAuthContext(r *http.Request, _ *Server) *resolver.AuthContext {
	ctx := &resolver.AuthContext{}

	// Try to extract identity from request context (set by auth middleware).
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		if claims.Subject != "" {
			ctx.AgentID = claims.Subject
		}
		if claims.OrgID != "" {
			ctx.ClientID = claims.OrgID
		}
		if claims.SiteID != "" {
			ctx.SiteID = claims.SiteID
		}
	}

	return ctx
}

// redactSecretData replaces all string values in a secret data map with
// a fixed redaction marker. Non-string values are replaced with their type
// name. The original keys are preserved so the caller can see the
// structure without seeing the values.
func redactSecretData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	const redacted = "***REDACTED***"
	for k, v := range data {
		switch v.(type) {
		case string:
			out[k] = redacted
		default:
			out[k] = "***REDACTED(" + typeName(v) + ")***"
		}
	}
	return out
}

// typeName returns a human-readable type name for a value.
func typeName(v any) string {
	switch v.(type) {
	case int, int32, int64, float32, float64:
		return "number"
	case bool:
		return "bool"
	case map[string]any:
		return "object"
	default:
		return "value"
	}
}
