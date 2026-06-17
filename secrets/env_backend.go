package secrets

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// EnvBackend reads secrets from process environment variables.
// Set and Delete are no-ops that log warnings.
type EnvBackend struct {
	mu     sync.RWMutex
	prefix string
}

// NewEnvBackend creates a new environment variable backend.
func NewEnvBackend(prefix string) *EnvBackend {
	if prefix == "" {
		prefix = "OAP_SECRET_"
	}
	return &EnvBackend{prefix: prefix}
}

// Get reads a secret from the process environment.
// The path is sanitized and used to construct the env var name.
func (e *EnvBackend) Get(ctx context.Context, path string, version *int) (*SecretValue, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	key := e.prefix + sanitizePath(path)
	val := os.Getenv(key)
	if val == "" {
		return nil, fmt.Errorf("environment variable %s not set", key)
	}

	return &SecretValue{
		Path:    path,
		Version: 1,
		Data:    map[string]any{"value": val},
		Metadata: SecretMetadata{
			Version: 1,
		},
	}, nil
}

// Set is a no-op that logs a warning.
func (e *EnvBackend) Set(ctx context.Context, path string, data map[string]any, opts SetOptions) (*SecretVersion, error) {
	log.Printf("warning: EnvBackend.Set is a no-op for path %s", path)
	return nil, fmt.Errorf("env backend is read-only")
}

// Delete is a no-op that logs a warning.
func (e *EnvBackend) Delete(ctx context.Context, path string, opts DeleteOptions) error {
	log.Printf("warning: EnvBackend.Delete is a no-op for path %s", path)
	return fmt.Errorf("env backend is read-only")
}

// List returns environment variables matching the prefix.
func (e *EnvBackend) List(ctx context.Context, opts ListOptions) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var paths []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, e.prefix) {
			idx := strings.Index(env, "=")
			if idx > 0 {
				varName := env[:idx]
				path := strings.TrimPrefix(varName, e.prefix)
				path = strings.ToLower(strings.ReplaceAll(path, "_", "/"))
				if opts.Prefix == "" || hasPrefix(path, opts.Prefix) {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths, nil
}

// Metadata returns minimal metadata.
func (e *EnvBackend) Metadata(ctx context.Context, path string) (*SecretMetadata, error) {
	key := e.prefix + sanitizePath(path)
	if os.Getenv(key) == "" {
		return nil, fmt.Errorf("environment variable %s not set", key)
	}
	return &SecretMetadata{Version: 1}, nil
}

// Rotate is a no-op.
func (e *EnvBackend) Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error) {
	return nil, fmt.Errorf("env backend does not support rotation")
}

// Healthcheck always returns nil.
func (e *EnvBackend) Healthcheck(ctx context.Context) error {
	return nil
}

// Close is a no-op.
func (e *EnvBackend) Close(ctx context.Context) error {
	return nil
}

// SupportsDynamic returns false.
func (e *EnvBackend) SupportsDynamic() bool {
	return false
}

// RevokeLease is a no-op for the env backend.
func (e *EnvBackend) RevokeLease(ctx context.Context, leaseID string) error {
	return nil
}

// sanitizePath converts a path to a valid environment variable suffix.
func sanitizePath(path string) string {
	return strings.ToUpper(strings.ReplaceAll(path, "/", "_"))
}
