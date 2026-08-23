package bridge

import (
	"sync"
	"time"
)

// ============================================================
// Deduplicator — prevents the same condition from generating
// multiple tasks within a suppression window.
// ============================================================

// DedupEntry records that a dedup key was seen at a given time.
type DedupEntry struct {
	Key       string
	TaskID    string // the task already open for this condition
	SeenAt    time.Time
	ExpiresAt time.Time
}

// Deduplicator prevents duplicate task creation for the same dedup key
// within a configurable suppression window.
type Deduplicator struct {
	mu      sync.Mutex
	entries map[string]*DedupEntry
	window  time.Duration
}

// NewDeduplicator creates a deduplicator with the given suppression window.
func NewDeduplicator(window time.Duration) *Deduplicator {
	return &Deduplicator{
		entries: make(map[string]*DedupEntry),
		window:  window,
	}
}

// IsDuplicate returns true if the dedup key was already seen within the
// suppression window. If not seen, it records the key and returns false.
// taskID is the existing open task for this condition (empty if new).
func (d *Deduplicator) IsDuplicate(key, taskID string) bool {
	if key == "" {
		return false // no dedup key — never duplicate
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if entry, ok := d.entries[key]; ok && now.Before(entry.ExpiresAt) {
		return true
	}

	// Record or refresh the entry.
	d.entries[key] = &DedupEntry{
		Key:       key,
		TaskID:    taskID,
		SeenAt:    now,
		ExpiresAt: now.Add(d.window),
	}
	return false
}

// ExistingTaskID returns the task ID previously associated with a dedup key,
// or empty string if none.
func (d *Deduplicator) ExistingTaskID(key string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry, ok := d.entries[key]; ok {
		return entry.TaskID
	}
	return ""
}

// UpdateTaskID associates a newly created task with an existing dedup key.
func (d *Deduplicator) UpdateTaskID(key, taskID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry, ok := d.entries[key]; ok {
		entry.TaskID = taskID
	}
}

// Purge removes expired entries. Called periodically by maintenance.
func (d *Deduplicator) Purge() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	purged := 0
	for key, entry := range d.entries {
		if now.After(entry.ExpiresAt) {
			delete(d.entries, key)
			purged++
		}
	}
	return purged
}

// Len returns the number of active entries.
func (d *Deduplicator) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}
