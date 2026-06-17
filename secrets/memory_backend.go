package secrets

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// MemoryBackend is an in-process backend for testing only.
// It panics if instantiated in production.
type MemoryBackend struct {
	mu      sync.RWMutex
	store   map[string][]memoryEntry
	counter int
}

type memoryEntry struct {
	data    map[string]any
	version int
	created time.Time
	labels  map[string]string
}

// NewMemoryBackend creates a new in-memory backend.
// It panics if OAP_ENV is set to "production".
func NewMemoryBackend() *MemoryBackend {
	if os.Getenv("OAP_ENV") == "production" {
		panic("MemoryBackend cannot be used in production")
	}
	return &MemoryBackend{
		store: make(map[string][]memoryEntry),
	}
}

// Get retrieves a secret by path and optional version.
func (m *MemoryBackend) Get(ctx context.Context, path string, version *int) (*SecretValue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, ok := m.store[path]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("secret not found: %s", path)
	}

	var entry memoryEntry
	if version != nil {
		found := false
		for _, e := range entries {
			if e.version == *version {
				entry = e
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("version %d not found for %s", *version, path)
		}
	} else {
		entry = entries[len(entries)-1]
	}

	dataCopy := make(map[string]any, len(entry.data))
	for k, v := range entry.data {
		dataCopy[k] = v
	}

	return &SecretValue{
		Path:    path,
		Version: entry.version,
		Data:    dataCopy,
		Metadata: SecretMetadata{
			Version:   entry.version,
			CreatedAt: entry.created,
			UpdatedAt: entry.created,
		},
		CreatedAt: entry.created,
	}, nil
}

// Set writes a secret at the given path, creating a new version.
func (m *MemoryBackend) Set(ctx context.Context, path string, data map[string]any, opts SetOptions) (*SecretVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if opts.CAS > 0 {
		entries := m.store[path]
		if len(entries) == 0 || entries[len(entries)-1].version != opts.CAS {
			return nil, fmt.Errorf("CAS mismatch: expected %d, got %d", opts.CAS, m.currentVersion(path))
		}
	}

	m.counter++
	entry := memoryEntry{
		data:    copyMap(data),
		version: m.counter,
		created: time.Now(),
		labels:  opts.Labels,
	}
	m.store[path] = append(m.store[path], entry)

	return &SecretVersion{Path: path, Version: entry.version}, nil
}

func (m *MemoryBackend) currentVersion(path string) int {
	entries := m.store[path]
	if len(entries) == 0 {
		return 0
	}
	return entries[len(entries)-1].version
}

// Delete removes a secret.
func (m *MemoryBackend) Delete(ctx context.Context, path string, opts DeleteOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if opts.Permanent {
		delete(m.store, path)
		return nil
	}

	if len(opts.Versions) > 0 {
		entries := m.store[path]
		filtered := make([]memoryEntry, 0, len(entries))
		for _, e := range entries {
			remove := false
			for _, v := range opts.Versions {
				if e.version == v {
					remove = true
					break
				}
			}
			if !remove {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(m.store, path)
		} else {
			m.store[path] = filtered
		}
		return nil
	}

	delete(m.store, path)
	return nil
}

// List returns all secret paths under a prefix.
func (m *MemoryBackend) List(ctx context.Context, opts ListOptions) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var paths []string
	for path := range m.store {
		if opts.Prefix == "" || hasPrefix(path, opts.Prefix) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if opts.Limit > 0 && len(paths) > opts.Limit {
		paths = paths[:opts.Limit]
	}
	return paths, nil
}

// Metadata returns metadata for a secret path.
func (m *MemoryBackend) Metadata(ctx context.Context, path string) (*SecretMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, ok := m.store[path]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("secret not found: %s", path)
	}

	entry := entries[len(entries)-1]
	return &SecretMetadata{
		Version:   entry.version,
		CreatedAt: entry.created,
		UpdatedAt: entry.created,
	}, nil
}

// Rotate creates a new version of the secret with new data.
func (m *MemoryBackend) Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, ok := m.store[path]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("secret not found: %s", path)
	}

	m.counter++
	newData := opts.NewData
	if newData == nil {
		newData = entries[len(entries)-1].data
	}

	entry := memoryEntry{
		data:    copyMap(newData),
		version: m.counter,
		created: time.Now(),
	}
	entries = append(entries, entry)

	if opts.PreserveVersions > 0 && len(entries) > opts.PreserveVersions {
		entries = entries[len(entries)-opts.PreserveVersions:]
	}
	m.store[path] = entries

	return &SecretVersion{Path: path, Version: entry.version}, nil
}

// Healthcheck always returns nil for the memory backend.
func (m *MemoryBackend) Healthcheck(ctx context.Context) error {
	return nil
}

// Close releases all stored secrets.
func (m *MemoryBackend) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = make(map[string][]memoryEntry)
	return nil
}

// SupportsDynamic returns false.
func (m *MemoryBackend) SupportsDynamic() bool {
	return false
}

// RevokeLease is a no-op for the in-memory backend.
func (m *MemoryBackend) RevokeLease(ctx context.Context, leaseID string) error {
	return nil
}

func copyMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
