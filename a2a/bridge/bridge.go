// Package bridge - bridge.go implements the Event-to-Task bridge that
// subscribes to internal NATS event subjects and converts each event
// into an A2A Task. The bridge is the integration point between the
// platform's internal event bus and the A2A agent ecosystem.
//
// Each event type is mapped to a set of skill tags so that agents
// that advertise matching skills can be routed the resulting task.
// A per-event-type token-bucket rate limiter prevents event storms
// from flooding the task store.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// NATS event subject constants
// ============================================================
//
// These are the subjects the bridge subscribes to. They mirror the
// subjects published by the internal subsystems (check ingestor,
// alert engine, heartbeat handler, policy engine, patch deployer,
// script executor, and remote shell manager).
const (
	// SubjectCheckResult is the wildcard subject for check result
	// events published by the ingest pipeline.
	SubjectCheckResult = "oap.events.checks.result"

	// SubjectAlertEvents is the wildcard subject for alert
	// lifecycle events (fired, resolved, acknowledged).
	SubjectAlertEvents = "oap.events.alerts"

	// SubjectAgentOnline is the subject on which the heartbeat
	// handler publishes when an agent transitions to online.
	SubjectAgentOnline = "oap.events.agent.online"

	// SubjectAgentOffline is the subject on which the heartbeat
	// handler publishes when an agent transitions to offline.
	SubjectAgentOffline = "oap.events.agent.offline"

	// SubjectPolicyViolation is the subject for OPA policy
	// violation notifications.
	SubjectPolicyViolation = "oap.events.policy.violation"

	// SubjectPatchStatus is the wildcard subject for patch
	// deployment status changes.
	SubjectPatchStatus = "oap.events.patches"

	// SubjectScriptResult is the wildcard subject for script
	// execution completion events.
	SubjectScriptResult = "oap.events.scripts"

	// SubjectShellSession is the subject for remote shell session
	// lifecycle events.
	SubjectShellSession = "oap.events.shell.session"
)

// ============================================================
// Errors
// ============================================================

var (
	// ErrNilNATSClient is returned when a nil NATS client is provided.
	ErrNilNATSClient = fmt.Errorf("bridge: nil nats client")

	// ErrNilGateway is returned when a nil A2A gateway is provided.
	ErrNilGateway = fmt.Errorf("bridge: nil a2a gateway")

	// ErrNilLogger is returned when a nil logger is provided.
	ErrNilLogger = fmt.Errorf("bridge: nil logger")

	// ErrAlreadyStarted is returned when Start is called on an
	// already-running bridge.
	ErrAlreadyStarted = fmt.Errorf("bridge: already started")

	// ErrNotStarted is returned when Stop is called on a bridge
	// that was never started.
	ErrNotStarted = fmt.Errorf("bridge: not started")
)

// ============================================================
// Rate limiter (per event type)
// ============================================================

// eventRateLimiter is a simple per-event-type token bucket. It
// prevents a burst of identical events (e.g., thousands of check
// results arriving at once) from generating an equal number of
// A2A tasks. Each event subject gets its own bucket.
type eventRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	rate     float64 // tokens per second
	burst    float64 // max tokens
}

type rateBucket struct {
	tokens     float64
	lastRefill time.Time
}

func newEventRateLimiter(rate, burst float64) *eventRateLimiter {
	return &eventRateLimiter{
		buckets: make(map[string]*rateBucket),
		rate:    rate,
		burst:   burst,
	}
}

func (rl *eventRateLimiter) allow(key string) bool {
	if rl == nil {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &rateBucket{
			tokens:     rl.burst - 1,
			lastRefill: now,
		}
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastRefill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ============================================================
// Bridge configuration
// ============================================================

// Config holds optional bridge configuration. Sensible defaults
// are used for zero-value fields.
type Config struct {
	// RatePerSecond is the max number of tasks per event type per
	// second. Default: 50.
	RatePerSecond float64

	// RateBurst is the max burst per event type. Default: 100.
	RateBurst float64

	// QueueGroup is the NATS queue group name for the bridge
	// subscriptions. If empty, every server instance receives every
	// event (broadcast). If set, events are load-balanced across
	// the queue group.
	QueueGroup string
}

// defaultRatePerSecond is the default per-event-type rate limit.
const defaultRatePerSecond = 50.0

// defaultRateBurst is the default per-event-type burst size.
const defaultRateBurst = 100.0

// ============================================================
// Bridge
// ============================================================

// Bridge subscribes to internal NATS event subjects and converts
// each event into an A2A Task via the Gateway. It is safe for
// concurrent use.
type Bridge struct {
	nc      *nats.Conn
	gw      *gateway.Gateway
	log     *slog.Logger
	limiter *eventRateLimiter
	cfg     Config

	mu      sync.Mutex
	started bool
	subs    []*nats.Subscription
}

// NewBridge constructs an Event-to-Task bridge. The gateway is
// used to authorise and persist tasks; the NATS connection is used
// to subscribe to event subjects.
func NewBridge(nc *nats.Conn, gw *gateway.Gateway, log *slog.Logger, cfg Config) (*Bridge, error) {
	if nc == nil {
		return nil, ErrNilNATSClient
	}
	if gw == nil {
		return nil, ErrNilGateway
	}
	if log == nil {
		return nil, ErrNilLogger
	}

	rate := cfg.RatePerSecond
	if rate <= 0 {
		rate = defaultRatePerSecond
	}
	burst := cfg.RateBurst
	if burst <= 0 {
		burst = defaultRateBurst
	}

	return &Bridge{
		nc:      nc,
		gw:      gw,
		log:     log,
		limiter: newEventRateLimiter(rate, burst),
		cfg:     cfg,
	}, nil
}

// Start subscribes to all configured NATS subjects and begins
// converting events into A2A tasks. Returns ErrAlreadyStarted if
// the bridge is already running.
func (b *Bridge) Start() error {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return ErrAlreadyStarted
	}
	b.started = true
	b.mu.Unlock()

	subs, err := b.subscribeAll()
	if err != nil {
		b.mu.Lock()
		b.started = false
		b.mu.Unlock()
		return fmt.Errorf("bridge: subscribe: %w", err)
	}

	b.mu.Lock()
	b.subs = subs
	b.mu.Unlock()

	b.log.Info("bridge started",
		"subjects", len(subs),
		"queue_group", b.cfg.QueueGroup,
	)
	return nil
}

// Stop unsubscribes from all subjects and stops the bridge. Safe to
// call multiple times.
func (b *Bridge) Stop() {
	b.mu.Lock()
	if !b.started {
		b.mu.Unlock()
		return
	}
	b.started = false
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()

	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if err := sub.Unsubscribe(); err != nil {
			b.log.Warn("bridge unsubscribe failed",
				"subject", sub.Subject,
				"err", err,
			)
		}
	}
	b.log.Info("bridge stopped")
}

// IsStarted reports whether the bridge is currently running.
func (b *Bridge) IsStarted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.started
}

// ============================================================
// Internal: subscription management
// ============================================================

// subscriptionSpec pairs a NATS subject with the handler that
// converts its payloads into A2A tasks.
type subscriptionSpec struct {
	subject string
	handler nats.MsgHandler
}

// subscribeAll creates one NATS subscription per event subject.
// If a queue group is configured, subscriptions are joined to that
// group for load-balanced work distribution.
func (b *Bridge) subscribeAll() ([]*nats.Subscription, error) {
	specs := b.handlerSpecs()
	subs := make([]*nats.Subscription, 0, len(specs))

	for _, spec := range specs {
		var (
			sub *nats.Subscription
			err error
		)
		if b.cfg.QueueGroup != "" {
			sub, err = b.nc.QueueSubscribe(spec.subject, b.cfg.QueueGroup, spec.handler)
		} else {
			sub, err = b.nc.Subscribe(spec.subject, spec.handler)
		}
		if err != nil {
			// Roll back any subscriptions already created.
			for _, s := range subs {
				if s != nil {
					_ = s.Unsubscribe()
				}
			}
			return nil, fmt.Errorf("subscribe %q: %w", spec.subject, err)
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// handlerSpecs returns the list of (subject, handler) pairs the
// bridge subscribes to. Each handler converts its event type into
// an A2A task with the appropriate skill tags.
func (b *Bridge) handlerSpecs() []subscriptionSpec {
	return []subscriptionSpec{
		{SubjectCheckResult, b.handleCheckResult},
		{SubjectAlertEvents, b.handleAlert},
		{SubjectAgentOnline, b.handleAgentOnline},
		{SubjectAgentOffline, b.handleAgentOffline},
		{SubjectPolicyViolation, b.handlePolicyViolation},
		{SubjectPatchStatus, b.handlePatchStatus},
		{SubjectScriptResult, b.handleScriptResult},
		{SubjectShellSession, b.handleShellSession},
	}
}

// ============================================================
// Internal: event-to-task conversion
// ============================================================

// eventEnvelope is the common shape of events published on the
// internal NATS bus. Fields are optional; missing fields are
// simply not included in the generated task.
type eventEnvelope struct {
	ID        string         `json:"id,omitempty"`
	AgentID   string         `json:"agent_id,omitempty"`
	OrgID     string         `json:"org_id,omitempty"`
	Severity  string         `json:"severity,omitempty"`
	Status    string         `json:"status,omitempty"`
	Message   string         `json:"message,omitempty"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// convertToTask is the central conversion routine. It builds a
// models.Task from the event payload, applying the skill tags
// defined in mappings.go. The task is persisted via the gateway
// so that authorisation, rate limiting, and routing are applied
// consistently.
func (b *Bridge) convertToTask(ctx context.Context, subject string, data []byte) {
	if !b.limiter.allow(subject) {
		b.log.Warn("bridge: rate limited",
			"subject", subject,
		)
		return
	}

	env := eventEnvelope{}
	// Best-effort JSON parse. An unparseable payload still becomes
	// a task, just with fewer metadata fields.
	_ = json.Unmarshal(data, &env)

	task := b.buildTask(subject, data, env)
	if task == nil {
		return
	}

	// Create the task directly via the task manager. The gateway's
	// SendTask requires an authenticated identity; the bridge
	// operates as an internal service, so it bypasses the HTTP
	// auth layer and uses the manager directly. Skill tags are
	// already in task.Metadata for the router to consume.
	_, err := b.persistTask(ctx, task)
	if err != nil {
		b.log.Error("bridge: persist task failed",
			"subject", subject,
			"err", err,
		)
		return
	}

	b.log.Debug("bridge: task created",
		"subject", subject,
		"task_id", task.ID,
		"agent_id", env.AgentID,
	)
}

// buildTask constructs a models.Task from an event subject, raw
// payload, and parsed envelope. Returns nil if the subject is not
// recognised.
func (b *Bridge) buildTask(subject string, data []byte, env eventEnvelope) *models.Task {
	skills, ok := EventSubjectToSkills[subject]
	if !ok {
		b.log.Warn("bridge: unmapped subject", "subject", subject)
		return nil
	}

	now := time.Now().UTC()
	name := EventSubjectToName[subject]
	ctxPrefix := EventSubjectToContextPrefix[subject]

	// Compose metadata. Skill tags go first so the router can match
	// on them, followed by event-type context and the original
	// payload for the agent to inspect.
	metadata := make(map[string]string, len(skills)+6)
	metadata["source_subject"] = subject
	metadata["event_type"] = name
	metadata["skill"] = skills[0] // primary skill for single-skill routers
	for i, s := range skills {
		metadata[fmt.Sprintf("skill_%d", i)] = s
	}
	if env.AgentID != "" {
		metadata["agent_id"] = env.AgentID
	}
	if env.OrgID != "" {
		metadata["org_id"] = env.OrgID
	}
	if env.Severity != "" {
		metadata["severity"] = env.Severity
	}
	if env.ID != "" {
		metadata["event_id"] = env.ID
	}
	// Include the raw payload so agents have full context.
	metadata["raw_payload"] = string(data)

	// Context ID groups related tasks. We use the event type prefix
	// and the agent ID (if present) so that an agent can fetch all
	// tasks for its own context.
	contextID := ctxPrefix
	if env.AgentID != "" {
		contextID = fmt.Sprintf("%s-%s", ctxPrefix, env.AgentID)
	}

	return &models.Task{
		ID:        uuid.NewString(),
		ContextID: contextID,
		Status:    models.TaskStatusPending,
		Message: models.Message{
			ID:   uuid.NewString(),
			Role: "system",
			Parts: []models.Part{
				{Text: fmt.Sprintf("Event: %s | Agent: %s | Severity: %s | %s",
					name, env.AgentID, env.Severity, env.Message)},
			},
		},
		Metadata:  metadata,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// persistTask stores the task using the gateway with a system
// identity. The bridge is an internal service and does not have an
// HTTP identity; the system identity carries the a2a:send
// permission so the gateway's authorisation check passes. Skill
// tags in metadata drive agent routing via the router.
func (b *Bridge) persistTask(ctx context.Context, t *models.Task) (*models.Task, error) {
	systemID := &gateway.Identity{
		Subject: "bridge",
		Method:  gateway.AuthNone,
		Scopes:  []string{gateway.PermSend},
	}
	return b.gw.SendTask(ctx, systemID, t)
}

// ============================================================
// Event-specific handlers
// ============================================================
//
// Each handler is a thin nats.MsgHandler that defers to
// convertToTask. Keeping them as named methods makes the
// subscription table readable and allows future per-event-type
// customisation (e.g., enrichment from the database) without
// changing the dispatch logic.

func (b *Bridge) handleCheckResult(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectCheckResult, msg.Data)
}

func (b *Bridge) handleAlert(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectAlertEvents, msg.Data)
}

func (b *Bridge) handleAgentOnline(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectAgentOnline, msg.Data)
}

func (b *Bridge) handleAgentOffline(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectAgentOffline, msg.Data)
}

func (b *Bridge) handlePolicyViolation(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectPolicyViolation, msg.Data)
}

func (b *Bridge) handlePatchStatus(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectPatchStatus, msg.Data)
}

func (b *Bridge) handleScriptResult(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectScriptResult, msg.Data)
}

func (b *Bridge) handleShellSession(msg *nats.Msg) {
	b.convertToTask(context.Background(), SubjectShellSession, msg.Data)
}
