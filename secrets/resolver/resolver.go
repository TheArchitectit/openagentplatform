// Package resolver provides the SecretResolver for OAP secret references.
//
// A secret reference URI has the form:
//
//	ref:oap://<backend_type>/<workspace_id>/<path>?version=<v>&key=<k>
//
// The resolver parses these URIs, looks up the appropriate backend from the
// registry, performs hierarchy-based authorization, fetches the secret value,
// optionally extracts a nested key, and emits an audit event.
package resolver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/secrets"
)

// ParsedRef is the structured form of a secret reference URI.
type ParsedRef struct {
	BackendType string
	WorkspaceID string
	Path        string
	Version     *int
	Key         string
}

// SecretResolver resolves OAP secret reference URIs to their underlying values.
type SecretResolver struct {
	registry *secrets.BackendRegistry
	authz    *Authorizer
	cache    *LRUCache
	logger   *slog.Logger
	audit    *audit.AuditService
}

// New creates a new SecretResolver.
func New(registry *secrets.BackendRegistry, logger *slog.Logger, auditSvc *audit.AuditService) *SecretResolver {
	return &SecretResolver{
		registry: registry,
		authz:    NewAuthorizer(),
		cache:    NewLRUCache(DefaultCacheMaxEntries),
		logger:   logger,
		audit:    auditSvc,
	}
}

// ParseURI parses a raw OAP secret reference URI into a ParsedRef.
func ParseURI(raw string) (ParsedRef, error) {
	const scheme = "ref:oap://"
	if !strings.HasPrefix(raw, scheme) {
		return ParsedRef{}, fmt.Errorf("invalid scheme: expected %s prefix", scheme)
	}
	rest := raw[len(scheme):]

	u, err := url.Parse(rest)
	if err != nil {
		return ParsedRef{}, fmt.Errorf("parse uri: %w", err)
	}

	// url.Parse puts the first segment into Host when there is no //,
	// but with "ref:oap://backend/workspace/path" the structure is:
	//   Host = "backend"
	//   Path = "/workspace/path"
	backendType := u.Host
	if backendType == "" {
		// Fallback: split manually.
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) < 2 {
			return ParsedRef{}, errors.New("invalid uri: missing backend type and path")
		}
		backendType = parts[0]
		rest = parts[1]
		u, _ = url.Parse(rest)
	}

	trimmed := strings.TrimPrefix(u.Path, "/")
	segments := strings.SplitN(trimmed, "/", 2)
	if len(segments) < 2 {
		return ParsedRef{}, errors.New("invalid uri: missing workspace_id or path")
	}
	workspaceID := segments[0]
	path := segments[1]

	ref := ParsedRef{
		BackendType: backendType,
		WorkspaceID: workspaceID,
		Path:        path,
	}

	if v := u.Query().Get("version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return ParsedRef{}, fmt.Errorf("invalid version: %w", err)
		}
		ref.Version = &n
	}
	ref.Key = u.Query().Get("key")

	return ref, nil
}

// Resolve resolves a single secret reference URI to its value.
func (r *SecretResolver) Resolve(ctx context.Context, uri string, authCtx *AuthContext) (*secrets.SecretValue, error) {
	ref, err := ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("parse uri: %w", err)
	}

	// Authorize.
	if !r.authz.CanAccess(ref.Path, authCtx) {
		return nil, fmt.Errorf("access denied: %s cannot access %s", authCtx.AgentID, ref.Path)
	}

	// Check cache (cache only holds non-dynamic values by design).
	cacheKey := CacheKey(ref)
	if cached, ok := r.cache.Get(cacheKey); ok {
		return cached, nil
	}

	// Look up backend.
	backend, ok := r.registry.Get(ref.BackendType)
	if !ok {
		return nil, fmt.Errorf("backend lookup: %q not registered", ref.BackendType)
	}

	// Fetch.
	val, err := backend.Get(ctx, ref.Path, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("backend fetch: %w", err)
	}

	// Extract nested key if specified.
	if ref.Key != "" {
		if _, exists := val.Data[ref.Key]; !exists {
			return nil, fmt.Errorf("key %q not found in secret at %s", ref.Key, ref.Path)
		}
		val.Data = map[string]any{ref.Key: val.Data[ref.Key]}
	}

	// Cache (non-dynamic only).
	if !val.Metadata.IsDynamic {
		r.cache.Put(cacheKey, val)
	}

	// Audit.
	if r.audit != nil {
		_, _ = r.audit.Record(ctx, audit.EventInput{
			ActorType:    audit.ActorAgent,
			ActorID:      authCtx.AgentID,
			Action:       "secret_resolve",
			ResourceType: "secret",
			ResourceID:   ref.Path,
			Details: map[string]any{
				"uri":       uri,
				"backend":   ref.BackendType,
				"workspace": ref.WorkspaceID,
			},
			Outcome: audit.OutcomeSuccess,
			OrgID:   authCtx.ClientID,
			SiteID:  authCtx.SiteID,
		})
	}

	return val, nil
}

// ResolveMany resolves multiple secret reference URIs in parallel with a
// concurrency limit of 16.
func (r *SecretResolver) ResolveMany(ctx context.Context, uris []string, authCtx *AuthContext) ([]*secrets.SecretValue, error) {
	const maxConcurrency = 16
	sem := make(chan struct{}, maxConcurrency)
	results := make([]*secrets.SecretValue, len(uris))
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for i, uri := range uris {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, u string) {
			defer wg.Done()
			defer func() { <-sem }()
			val, err := r.Resolve(ctx, u, authCtx)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			results[idx] = val
		}(i, uri)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// InjectWorkspaceVariables replaces {{variable}} placeholders in a URI with
// values from the provided map.
func InjectWorkspaceVariables(uri string, vars map[string]string) string {
	for k, v := range vars {
		uri = strings.ReplaceAll(uri, "{{"+k+"}}", v)
	}
	return uri
}

// BackendFor returns the SecretBackend registered under the given name.
// This is used by the injection pipeline to revoke dynamic-secret leases
// through the originating backend.
func (r *SecretResolver) BackendFor(name string) (secrets.SecretBackend, bool) {
	if r == nil || r.registry == nil {
		return nil, false
	}
	return r.registry.Get(name)
}

// Backends returns the names of all registered backends. This is used by
// the API layer to report configured backends and run health checks.
func (r *SecretResolver) Backends() []string {
	if r == nil || r.registry == nil {
		return nil
	}
	return r.registry.List()
}
