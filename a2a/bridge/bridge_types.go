package bridge

import (
	"context"
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
	mu      sync.Mutex
	buckets map[string]*rateBucket
	rate    float64 // tokens per second
	burst   float64 // max tokens
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
		log = slog.Default()
	}

	rate := cfg.RatePerSecond
	if rate == 0 {
		rate = defaultRatePerSecond
	}
	burst := cfg.RateBurst
	if burst == 0 {
		burst = defaultRateBurst
	}

	return &Bridge{
		nc:      nc,
		gw:      gw,
		log:     log,
		cfg:     cfg,
		limiter: newEventRateLimiter(rate, burst),
	}, nil
}

// subjectHandlers maps each NATS subject to a short human label used
// only for logging.
var bridgeSubjects = []string{
	SubjectCheckResult,
	SubjectAlertEvents,
	SubjectAgentOnline,
	SubjectAgentOffline,
	SubjectPolicyViolation,
	SubjectPatchStatus,
	SubjectScriptResult,
	SubjectShellSession,
}

// Start subscribes to all event subjects and begins converting events
// into A2A tasks via the gateway.
func (b *Bridge) Start() error {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return ErrAlreadyStarted
	}
	b.mu.Unlock()

	identity := &gateway.Identity{
		Subject: "event-bridge",
		Method:  gateway.AuthNone,
		Scopes:  []string{gateway.PermSend},
	}

	for _, subject := range bridgeSubjects {
		sub, err := b.subscribe(subject, identity)
		if err != nil {
			// Roll back any subscriptions already made.
			b.Stop()
			return fmt.Errorf("bridge: subscribe %q: %w", subject, err)
		}
		b.mu.Lock()
		b.subs = append(b.subs, sub)
		b.mu.Unlock()
	}

	b.mu.Lock()
	b.started = true
	b.mu.Unlock()

	b.log.Info("bridge: started",
		"subjects", len(bridgeSubjects),
		"queue_group", b.cfg.QueueGroup,
	)
	return nil
}

// subscribe wires a single NATS subject to the event handler.
func (b *Bridge) subscribe(subject string, identity *gateway.Identity) (*nats.Subscription, error) {
	handler := func(msg *nats.Msg) {
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("bridge: handler panic", "subject", subject, "panic", r)
			}
		}()

		if !b.limiter.allow(subject) {
			return
		}

		task := &models.Task{
			ID:      uuid.NewString(),
			AgentID: "event-handler",
			Message: models.Message{
				Role: "user",
				Parts: []models.Part{
					{Text: string(msg.Data)},
				},
			},
			Metadata: map[string]string{
				"source_subject": subject,
			},
		}

		if _, err := b.gw.SendTask(context.Background(), identity, task); err != nil {
			b.log.Warn("bridge: send task", "subject", subject, "err", err)
			return
		}
	}

	if b.cfg.QueueGroup != "" {
		return b.nc.QueueSubscribe(subject, b.cfg.QueueGroup, handler)
	}
	return b.nc.Subscribe(subject, handler)
}

// Stop unsubscribes from all subjects and resets the bridge state.
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
		if err := sub.Drain(); err != nil {
			b.log.Warn("bridge: drain subscription", "subject", sub.Subject, "err", err)
		}
	}

	b.log.Info("bridge: stopped")
}
