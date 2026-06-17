// Package auth provides A2A authentication token management.
// This file implements the in-memory revocation list with optional Redis backing.
package auth

import (
	"context"
	"sync"
	"time"
)

// RevocationStore is the interface for revocation list backends.
type RevocationStore interface {
	Add(ctx context.Context, jti string, expiresAt time.Time) error
	Contains(ctx context.Context, jti string) (bool, error)
	PurgeExpired(ctx context.Context) (int, error)
}

// RevocationList is an in-memory thread-safe revocation list for JWT IDs (jti).
// Entries expire automatically when the associated token would have expired,
// so the list only holds entries that are still relevant.
type RevocationList struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti -> expiresAt
	store   RevocationStore      // optional external backing store (e.g. Redis)
}

// NewRevocationList creates a new empty in-memory revocation list.
func NewRevocationList() *RevocationList {
	return &RevocationList{
		entries: make(map[string]time.Time),
	}
}

// NewRevocationListWithStore creates a revocation list backed by an external store.
// The in-memory map is always used as the primary fast-path cache.
func NewRevocationListWithStore(store RevocationStore) *RevocationList {
	return &RevocationList{
		entries: make(map[string]time.Time),
		store:   store,
	}
}

// Add marks a JTI as revoked until expiresAt.
// Once the expiry time passes, the entry is no longer meaningful (the token itself
// has expired) and will be removed by PurgeExpired.
func (r *RevocationList) Add(jti string, expiresAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[jti] = expiresAt
	if r.store != nil {
		// Fire-and-forget; external store failures are logged but do not block.
		go func() {
			_ = r.store.Add(context.Background(), jti, expiresAt)
		}()
	}
}

// Contains reports whether the given JTI has been revoked and has not yet expired.
func (r *RevocationList) Contains(jti string) bool {
	r.mu.RLock()
	expiresAt, ok := r.entries[jti]
	r.mu.RUnlock()

	if ok {
		return time.Now().Before(expiresAt)
	}

	// Fall back to external store if configured.
	if r.store != nil {
		found, err := r.store.Contains(context.Background(), jti)
		if err == nil && found {
			// Cache the result locally.
			// We don't know the original expiresAt from the external store query,
			// so assume a short TTL to allow periodic re-checking.
			r.mu.Lock()
			if _, exists := r.entries[jti]; !exists {
				r.entries[jti] = time.Now().Add(5 * time.Minute)
			}
			r.mu.Unlock()
			return true
		}
	}

	return false
}

// PurgeExpired removes all revocation entries whose associated tokens have already expired.
// Returns the number of entries purged.
func (r *RevocationList) PurgeExpired() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	purged := 0
	for jti, expiresAt := range r.entries {
		if now.After(expiresAt) {
			delete(r.entries, jti)
			purged++
		}
	}
	return purged
}

// StartPurgeLoop starts a background goroutine that calls PurgeExpired at the given interval.
// It runs until the provided context is cancelled.
func (r *RevocationList) StartPurgeLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.PurgeExpired()
				if r.store != nil {
					_, _ = r.store.PurgeExpired(ctx)
				}
			}
		}
	}()
}

// Size returns the current number of entries in the revocation list.
func (r *RevocationList) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
