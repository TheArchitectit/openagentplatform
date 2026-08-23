package resolver

import (
	"container/list"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/secrets"
)

// DefaultCacheMaxEntries is the default maximum number of entries in the LRU cache.
const DefaultCacheMaxEntries = 10000

// CacheTTL is the default TTL for cached secret values (non-dynamic only).
const CacheTTL = 10 * time.Second

// cacheEntry holds a cached secret value along with its expiration time.
type cacheEntry struct {
	key       string
	value     *secrets.SecretValue
	expiresAt time.Time
}

// LRUCache is a TTL-based LRU cache for resolved secret values.
type LRUCache struct {
	mu       sync.Mutex
	max      int
	items    map[string]*list.Element
	order    *list.List
}

// NewLRUCache creates a new TTL-based LRU cache with the given max entries.
func NewLRUCache(maxEntries int) *LRUCache {
	if maxEntries <= 0 {
		maxEntries = DefaultCacheMaxEntries
	}
	return &LRUCache{
		max:   maxEntries,
		items: make(map[string]*list.Element),
		order: list.New(),
	}
}

// CacheKey generates a cache key from a ParsedRef.
func CacheKey(ref ParsedRef) string {
	version := -1
	if ref.Version != nil {
		version = *ref.Version
	}
	return fmt.Sprintf("%s:%s:%s:%d", ref.BackendType, ref.WorkspaceID, ref.Path, version)
}

// Get retrieves a cached entry. Returns the value and true if found and not expired.
func (c *LRUCache) Get(key string) (*secrets.SecretValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.order.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(el)
	return entry.value, true
}

// Put inserts or updates a cache entry with the default TTL.
func (c *LRUCache) Put(key string, val *secrets.SecretValue) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Now().Add(CacheTTL)

	if el, ok := c.items[key]; ok {
		entry := el.Value.(*cacheEntry)
		entry.value = val
		entry.expiresAt = expiresAt
		c.order.MoveToFront(el)
		return
	}

	entry := &cacheEntry{key: key, value: val, expiresAt: expiresAt}
	el := c.order.PushFront(entry)
	c.items[key] = el

	if c.order.Len() > c.max {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*cacheEntry).key)
		}
	}
}

// Invalidate removes all cache entries whose key starts with the given path prefix.
func (c *LRUCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	toDelete := make([]string, 0)
	for k := range c.items {
		if containsPath(k, path) {
			toDelete = append(toDelete, k)
		}
	}
	for _, k := range toDelete {
		if el, ok := c.items[k]; ok {
			c.order.Remove(el)
			delete(c.items, k)
		}
	}
}

// containsPath checks if a cache key contains the given path as a
// colon-delimited segment. The cache key format is
// "backendType:workspaceID:path:version", so we match on the path
// segment specifically to avoid over-invalidation from naive substring
// matching (e.g. path "secret" should not match "secretariat").
func containsPath(key, path string) bool {
	// Fast check: path must appear in the key at all.
	idx := strings.Index(key, path)
	if idx < 0 {
		return false
	}
	// Check that the match is bounded by ':' delimiters or string boundaries.
	// A valid match means the path is a complete segment in the key.
	startOK := idx == 0 || key[idx-1] == ':'
	endIdx := idx + len(path)
	endOK := endIdx == len(key) || key[endIdx] == ':'
	return startOK && endOK
}
