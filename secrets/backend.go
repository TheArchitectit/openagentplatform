package secrets

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNotSupported is returned by backend methods that the underlying
// engine does not implement (e.g. lease revocation on a static-secret
// backend).
var ErrNotSupported = errors.New("secrets: operation not supported by this backend")

// SecretMetadata contains backend-specific metadata about a secret.
type SecretMetadata struct {
	Version       int           `json:"version"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	TTL           time.Duration `json:"ttl,omitempty"`
	IsDynamic     bool          `json:"is_dynamic,omitempty"`
	LeaseID       string        `json:"lease_id,omitempty"`
	LeaseDuration time.Duration `json:"lease_duration,omitempty"`
}

// SecretValue represents a resolved secret returned from a backend.
type SecretValue struct {
	Path      string         `json:"path"`
	Version   int            `json:"version"`
	Data      map[string]any `json:"data"`
	Metadata  SecretMetadata `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// SecretVersion identifies a specific version of a secret.
type SecretVersion struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
}

// SetOptions configures a Set operation.
type SetOptions struct {
	TTL    time.Duration     `json:"ttl,omitempty"`
	CAS    int               `json:"cas,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// DeleteOptions configures a Delete operation.
type DeleteOptions struct {
	Versions  []int `json:"versions,omitempty"`
	Permanent bool  `json:"permanent,omitempty"`
}

// ListOptions configures a List operation.
type ListOptions struct {
	Prefix string `json:"prefix,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// RotateOptions configures a Rotate operation.
type RotateOptions struct {
	NewData          map[string]any `json:"new_data,omitempty"`
	PreserveVersions int            `json:"preserve_versions,omitempty"`
}

// SecretBackend is the interface implemented by all secret storage backends.
type SecretBackend interface {
	// Get retrieves a secret value by path and optional version.
	Get(ctx context.Context, path string, version *int) (*SecretValue, error)

	// Set writes a secret at the given path.
	Set(ctx context.Context, path string, data map[string]any, opts SetOptions) (*SecretVersion, error)

	// Delete removes a secret.
	Delete(ctx context.Context, path string, opts DeleteOptions) error

	// List enumerates secret paths under a prefix.
	List(ctx context.Context, opts ListOptions) ([]string, error)

	// Metadata returns metadata for a secret path.
	Metadata(ctx context.Context, path string) (*SecretMetadata, error)

	// Rotate triggers backend-native rotation.
	Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error)

	// Healthcheck verifies the backend is reachable and authenticated.
	Healthcheck(ctx context.Context) error

	// Close releases any held connections, leases, or tokens.
	Close(ctx context.Context) error

	// SupportsDynamic returns true if the backend can issue short-lived dynamic secrets.
	SupportsDynamic() bool

	// RevokeLease revokes a dynamic-secret lease by its lease ID.
	// Backends that do not support dynamic secrets may return ErrNotSupported.
	RevokeLease(ctx context.Context, leaseID string) error
}

// BackendRegistry manages a collection of named backends.
type BackendRegistry struct {
	mu       sync.RWMutex
	backends map[string]SecretBackend
}

// NewBackendRegistry creates a new empty BackendRegistry.
func NewBackendRegistry() *BackendRegistry {
	return &BackendRegistry{
		backends: make(map[string]SecretBackend),
	}
}

// Register adds a backend under the given name.
func (r *BackendRegistry) Register(name string, backend SecretBackend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[name] = backend
}

// Get retrieves a backend by name.
func (r *BackendRegistry) Get(name string) (SecretBackend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	return b, ok
}

// List returns the names of all registered backends.
func (r *BackendRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.backends))
	for name := range r.backends {
		names = append(names, name)
	}
	return names
}

// Unregister removes a backend by name.
func (r *BackendRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.backends, name)
}
