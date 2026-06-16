// Package notify implements the notification channel dispatch system.
// Notifier is the interface every channel implementation (email, Slack,
// webhook, ...) must satisfy. NotifierRegistry maps channel-type strings
// to their implementations, and Dispatch fans out an alert to all
// channels concurrently with exponential-backoff retry.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Supported channel types.
const (
	ChannelEmail   = "email"
	ChannelSlack   = "slack"
	ChannelWebhook = "webhook"
)

// MaxRetryAttempts is the maximum number of delivery attempts per channel
// (the initial attempt plus up to two retries with exponential backoff).
const MaxRetryAttempts = 3

// BaseBackoff is the initial backoff interval for the first retry. Each
// subsequent retry doubles the interval (1s, 2s, 4s by default).
const BaseBackoff = 1 * time.Second

// DispatchTimeout is the per-channel delivery timeout. A single Notify call
// must complete within this window before the next retry is scheduled.
const DispatchTimeout = 30 * time.Second

// NotificationChannel is the serialised, validated configuration for a
// single channel instance. The Notifier for ChannelType is responsible for
// interpreting Config.
type NotificationChannel struct {
	ID        string          `json:"id"`
	OrgID     string          `json:"org_id"`
	UserID    string          `json:"user_id,omitempty"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // "email", "slack", "webhook"
	Enabled   bool            `json:"enabled"`
	Config    json.RawMessage `json:"config"` // type-specific configuration
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Notifier is the contract every channel implementation must satisfy.
// Notify delivers a single alert payload through the channel. ValidateConfig
// checks that the type-specific Config blob is well-formed for the
// channel -- it is called on channel create/update and on startup.
type Notifier interface {
	Notify(ctx context.Context, alert *models.Alert, channel NotificationChannel) error
	ValidateConfig(config json.RawMessage) error
}

// NotifierRegistry maps a channel type string to its Notifier. The
// default registry is populated by InitDefaultRegistry. Callers can
// register additional notifiers (e.g. PagerDuty, OpsGenie) by calling
// Register.
type NotifierRegistry struct {
	mu       sync.RWMutex
	notifiers map[string]Notifier
}

// NewRegistry creates an empty NotifierRegistry.
func NewRegistry() *NotifierRegistry {
	return &NotifierRegistry{notifiers: make(map[string]Notifier)}
}

// Register adds (or replaces) a notifier for a channel type.
func (r *NotifierRegistry) Register(channelType string, n Notifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifiers[channelType] = n
}

// Get looks up the notifier for the given channel type. Returns nil if
// no implementation is registered.
func (r *NotifierRegistry) Get(channelType string) Notifier {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.notifiers[channelType]
}

// SupportedTypes returns the set of registered channel types. Useful for
// the API to validate user input.
func (r *NotifierRegistry) SupportedTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.notifiers))
	for t := range r.notifiers {
		out = append(out, t)
	}
	return out
}

// InitDefaultRegistry creates a registry pre-populated with the built-in
// notifiers: email, slack, webhook. This is the recommended entry point
// for production wiring; tests can use NewRegistry to register mocks.
func InitDefaultRegistry() *NotifierRegistry {
	r := NewRegistry()
	r.Register(ChannelEmail, &EmailNotifier{})
	r.Register(ChannelSlack, &SlackNotifier{})
	r.Register(ChannelWebhook, &WebhookNotifier{})
	return r
}

// DispatchResult records the outcome of a single channel's delivery
// attempt. Err is nil on success; Status reflects the persisted state.
type DispatchResult struct {
	ChannelID string
	ChannelType string
	Attempt   int
	Status    string // "sent" or "failed"
	Err       error
}

// Dispatch fans out an alert to all channels concurrently, retrying each
// channel independently with exponential backoff. Channels with no
// registered notifier or with malformed configs are logged and skipped
// (their results are returned with Err set so the caller can audit).
// The caller's context cancels all in-flight deliveries.
func Dispatch(ctx context.Context, registry *NotifierRegistry, alert *models.Alert, channels []NotificationChannel, log *slog.Logger) []DispatchResult {
	if log == nil {
		log = slog.Default()
	}
	results := make([]DispatchResult, len(channels))
	var wg sync.WaitGroup
	for i := range channels {
		i := i
		ch := channels[i]
		if !ch.Enabled {
			results[i] = DispatchResult{
				ChannelID: ch.ID, ChannelType: ch.Type,
				Status: "skipped", Err: fmt.Errorf("channel disabled"),
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = dispatchOne(ctx, registry, alert, ch, log)
		}()
	}
	wg.Wait()
	return results
}

// dispatchOne delivers a single channel with exponential-backoff retry.
// The retry sequence is 3 attempts total: initial, +1s, +2s. Each attempt
// uses a fresh sub-context bounded by DispatchTimeout.
func dispatchOne(ctx context.Context, registry *NotifierRegistry, alert *models.Alert, ch NotificationChannel, log *slog.Logger) DispatchResult {
	notifier := registry.Get(ch.Type)
	if notifier == nil {
		return DispatchResult{
			ChannelID: ch.ID, ChannelType: ch.Type,
			Status: "failed", Err: fmt.Errorf("no notifier registered for type %q", ch.Type),
		}
	}
	if err := notifier.ValidateConfig(ch.Config); err != nil {
		return DispatchResult{
			ChannelID: ch.ID, ChannelType: ch.Type,
			Status: "failed", Err: fmt.Errorf("invalid channel config: %w", err),
		}
	}

	var lastErr error
	for attempt := 1; attempt <= MaxRetryAttempts; attempt++ {
		if ctx.Err() != nil {
			return DispatchResult{
				ChannelID: ch.ID, ChannelType: ch.Type, Attempt: attempt,
				Status: "failed", Err: ctx.Err(),
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, DispatchTimeout)
		err := notifier.Notify(attemptCtx, alert, ch)
		cancel()
		if err == nil {
			log.Info("notification delivered",
				"alert_id", alert.ID,
				"channel_id", ch.ID,
				"channel_type", ch.Type,
				"attempt", attempt)
			return DispatchResult{
				ChannelID: ch.ID, ChannelType: ch.Type, Attempt: attempt,
				Status: "sent",
			}
		}
		lastErr = err
		log.Warn("notification attempt failed",
			"alert_id", alert.ID,
			"channel_id", ch.ID,
			"channel_type", ch.Type,
			"attempt", attempt,
			"err", err)
		if attempt < MaxRetryAttempts {
			backoff := time.Duration(float64(BaseBackoff) * math.Pow(2, float64(attempt-1)))
			select {
			case <-ctx.Done():
				return DispatchResult{
					ChannelID: ch.ID, ChannelType: ch.Type, Attempt: attempt,
					Status: "failed", Err: ctx.Err(),
				}
			case <-time.After(backoff):
			}
		}
	}
	return DispatchResult{
		ChannelID: ch.ID, ChannelType: ch.Type, Attempt: MaxRetryAttempts,
		Status: "failed", Err: lastErr,
	}
}
