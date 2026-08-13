package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

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
