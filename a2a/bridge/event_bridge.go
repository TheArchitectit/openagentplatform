package bridge

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// TaskCreator is the interface the bridge uses to persist tasks.
// It delegates to the Task Manager's normal creation path.
// ============================================================

// TaskCreator abstracts task persistence so the bridge doesn't
// couple to a specific store.
type TaskCreator interface {
	// CreateTask persists a new task in SUBMITTED state and returns
	// its assigned ID. The bridge MUST NOT write task rows directly.
	CreateTask(ctx context.Context, task *TaskRequest) (string, error)

	// UpdateTask annotates an existing task (e.g. recurring event update).
	UpdateTask(ctx context.Context, taskID string, parts []Part) error
}

// TaskRequest is the payload for creating a new task.
type TaskRequest struct {
	SkillTag      string            `json:"skill_tag"`
	SourceEventID string            `json:"source_event_id"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	Metadata      map[string]string `json:"metadata"`
	Parts         []Part            `json:"parts"`
}

// ============================================================
// EventBridge — the core bridge logic.
// ============================================================

// EventBridgeConfig holds tunable parameters for the bridge.
type EventBridgeConfig struct {
	// DedupWindow is how long a dedup key prevents re-creation.
	DedupWindow time.Duration

	// RateLimitPerType is max tasks/min per event type.
	RateLimitPerType int64

	// RateLimitAggregate is max tasks/min across all types.
	RateLimitAggregate int64

	// CircuitThreshold is failures before the breaker trips.
	CircuitThreshold int

	// CircuitRecovery is how long the breaker stays open.
	CircuitRecovery time.Duration

	// SeverityFilter: events below this severity are not delegated.
	// Empty string means all severities pass.
	SeverityFilter string

	// MaxRetries for transient task-creation failures.
	MaxRetries int
}

// DefaultEventBridgeConfig returns sensible defaults.
func DefaultEventBridgeConfig() EventBridgeConfig {
	return EventBridgeConfig{
		DedupWindow:        15 * time.Minute,
		RateLimitPerType:   60,
		RateLimitAggregate: 300,
		CircuitThreshold:   5,
		CircuitRecovery:    30 * time.Second,
		SeverityFilter:     "",
		MaxRetries:         3,
	}
}

// EventBridgeMetrics exposes operational counters.
type EventBridgeMetrics struct {
	EventsConsumed  atomic.Int64
	TasksCreated    atomic.Int64
	EventsDeduped   atomic.Int64
	EventsRateLimit atomic.Int64
	EventsFiltered  atomic.Int64
	EventsCircuit   atomic.Int64
	ConversionFails atomic.Int64
}

// Snapshot returns a point-in-time read of all counters.
func (m *EventBridgeMetrics) Snapshot() map[string]int64 {
	return map[string]int64{
		"events_consumed":   m.EventsConsumed.Load(),
		"tasks_created":     m.TasksCreated.Load(),
		"events_deduped":    m.EventsDeduped.Load(),
		"events_rate_limit": m.EventsRateLimit.Load(),
		"events_filtered":   m.EventsFiltered.Load(),
		"events_circuit":    m.EventsCircuit.Load(),
		"conversion_fails":  m.ConversionFails.Load(),
	}
}

// EventBridge consumes RMM events and converts them into A2A tasks.
type EventBridge struct {
	cfg       EventBridgeConfig
	mappings  *MappingRegistry
	creator   TaskCreator
	dedup     *Deduplicator
	rateLimit *RateLimiter
	circuit   *CircuitBreaker
	metrics   EventBridgeMetrics
	mu        sync.Mutex
}

// NewEventBridge creates a fully wired bridge.
func NewEventBridge(cfg EventBridgeConfig, mappings []EventMapping, creator TaskCreator) *EventBridge {
	return &EventBridge{
		cfg:       cfg,
		mappings:  NewMappingRegistry(mappings),
		creator:   creator,
		dedup:     NewDeduplicator(cfg.DedupWindow),
		rateLimit: NewRateLimiter(cfg.RateLimitPerType, cfg.RateLimitAggregate),
		circuit:   NewCircuitBreaker(cfg.CircuitThreshold, cfg.CircuitRecovery),
	}
}

// ProcessEvent is the main entry point. It applies the full pipeline:
// mapping lookup → dedup → rate limit → circuit break → severity filter
// → task construction → persistence. Returns the task ID if created,
// empty string if suppressed, and an error only on transient failures.
func (eb *EventBridge) ProcessEvent(ctx context.Context, event *RMMEvent) (string, error) {
	eb.metrics.EventsConsumed.Add(1)

	// 1. Mapping lookup — unknown or disabled types are ignored.
	mapping, ok := eb.mappings.Lookup(event.Type)
	if !ok {
		log.Printf("bridge: ignoring unknown/disabled event type %q", event.Type)
		return "", nil
	}

	// 2. Severity filter.
	if eb.cfg.SeverityFilter != "" && severityBelow(event.Severity, eb.cfg.SeverityFilter) {
		eb.metrics.EventsFiltered.Add(1)
		return "", nil
	}

	// 3. Circuit breaker — gateway failing?
	if !eb.circuit.Allow() {
		eb.metrics.EventsCircuit.Add(1)
		return "", fmt.Errorf("bridge: circuit breaker open — gateway failing")
	}

	// 4. Rate limit.
	if !eb.rateLimit.Allow(event.Type) {
		eb.metrics.EventsRateLimit.Add(1)
		return "", nil // shed silently — logged via metrics
	}

	// 5. Deduplication.
	dedupKey := event.DedupKey
	if dedupKey == "" {
		dedupKey = event.ID
	}
	existingTaskID := eb.dedup.ExistingTaskID(dedupKey)
	if eb.dedup.IsDuplicate(dedupKey, existingTaskID) {
		eb.metrics.EventsDeduped.Add(1)
		return existingTaskID, nil // already have a task for this
	}

	// 6. Construct task parts from the event.
	parts := buildTaskParts(event, mapping)

	// 7. Persist via Task Manager.
	taskReq := &TaskRequest{
		SkillTag:      mapping.SkillTag,
		SourceEventID: event.ID,
		CorrelationID: event.CorrelationID,
		Metadata: map[string]string{
			"event_type":     string(event.Type),
			"severity":       event.Severity,
			"source_agent":   event.SourceAgentID,
			"affected":       event.AffectedResource,
			"org_id":         event.OrgID,
			"client_id":      event.ClientID,
			"site_id":        event.SiteID,
		},
		Parts: parts,
	}

	var taskID string
	var err error
	for attempt := 0; attempt <= eb.cfg.MaxRetries; attempt++ {
		taskID, err = eb.creator.CreateTask(ctx, taskReq)
		if err == nil {
			break
		}
		if attempt < eb.cfg.MaxRetries {
			backoff := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}

	if err != nil {
		eb.metrics.ConversionFails.Add(1)
		eb.circuit.RecordFailure()
		return "", fmt.Errorf("bridge: task creation failed after %d retries: %w", eb.cfg.MaxRetries, err)
	}

	eb.circuit.RecordSuccess()
	eb.metrics.TasksCreated.Add(1)
	eb.dedup.UpdateTaskID(dedupKey, taskID)
	return taskID, nil
}

// buildTaskParts constructs the message parts for the task from the event.
func buildTaskParts(event *RMMEvent, mapping EventMapping) []Part {
	summary := fmt.Sprintf(
		"[%s] %s on %s (severity: %s, agent: %s)",
		event.Type, mapping.Description, event.AffectedResource,
		event.Severity, event.SourceAgentID,
	)

	payload := ""
	if len(event.Payload) > 0 {
		payload = fmt.Sprintf("\n\nEvent payload:\n%v", event.Payload)
	}

	return []Part{
		{
			Type: "text",
			Text: summary + payload,
		},
	}
}

// severityBelow returns true if a is below b in the severity hierarchy.
func severityBelow(a, b string) bool {
	rank := map[string]int{
		"critical": 5, "high": 4, "medium": 3, "low": 2, "info": 1,
	}
	return rank[a] < rank[b]
}

// Metrics returns the current bridge metrics snapshot.
func (eb *EventBridge) Metrics() map[string]int64 {
	return eb.metrics.Snapshot()
}

// Dedup returns the deduplicator (for testing / inspection).
func (eb *EventBridge) Dedup() *Deduplicator {
	return eb.dedup
}

// Circuit returns the circuit breaker (for testing / inspection).
func (eb *EventBridge) Circuit() *CircuitBreaker {
	return eb.circuit
}

// RateLimiter returns the rate limiter (for testing / inspection).
func (eb *EventBridge) RateLimiter() *RateLimiter {
	return eb.rateLimit
}

// ============================================================
// Maintenance — background housekeeping.
// ============================================================

// MaintenanceLoop runs periodic dedup purging until ctx is cancelled.
func (eb *EventBridge) MaintenanceLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			purged := eb.dedup.Purge()
			if purged > 0 {
				log.Printf("bridge: purged %d expired dedup entries", purged)
			}
		case <-ctx.Done():
			return
		}
	}
}
