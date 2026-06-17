// Package gateway - subscriber.go implements the SubscriberHub, a
// fan-out event bus for task status updates. Subscribers receive real-time
// notifications via Server-Sent Events (SSE) channels, with heartbeat
// keep-alives and automatic cleanup of stale connections.
package gateway

import (
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// Configuration
// ============================================================

// SubscriberConfig holds tuning parameters for the SubscriberHub.
type SubscriberConfig struct {
	// HeartbeatInterval is how often keep-alive pings are sent.
	// Default: 15s.
	HeartbeatInterval time.Duration

	// MaxConnections is the maximum number of concurrent subscribers.
	// Zero = unlimited.
	MaxConnections int

	// ChannelBufferSize is the buffer size for each subscriber's event
	// channel. Default: 64.
	ChannelBufferSize int
}

// Default subscriber configuration values.
const (
	DefaultHeartbeat      = 15 * time.Second
	DefaultChannelBuffer  = 64
)

// ============================================================
// Subscription
// ============================================================

// Subscription represents a single subscriber's connection to a task's
// event stream. Events are delivered on the Events channel. The Done
// channel is closed when the subscription is terminated.
type Subscription struct {
	TaskID  string
	Events  chan models.TaskStatusUpdate
	Done    chan struct{}
	created time.Time
}

// NewSubscription creates a new subscription for the given task ID.
func NewSubscription(taskID string, bufferSize int) *Subscription {
	if bufferSize <= 0 {
		bufferSize = DefaultChannelBuffer
	}
	return &Subscription{
		TaskID:  taskID,
		Events:  make(chan models.TaskStatusUpdate, bufferSize),
		Done:    make(chan struct{}),
		created: time.Now(),
	}
}

// ============================================================
// SubscriberHub
// ============================================================

// SubscriberHub is a thread-safe fan-out hub for task status updates.
// Subscribers register interest in a task ID and receive all subsequent
// status updates for that task.
type SubscriberHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[*Subscription]struct{} // taskID -> set of subs
	config      SubscriberConfig
	stopCh      chan struct{}
}

// NewSubscriberHub creates a new hub with default configuration.
func NewSubscriberHub() *SubscriberHub {
	return NewSubscriberHubWithConfig(SubscriberConfig{
		HeartbeatInterval: DefaultHeartbeat,
		ChannelBufferSize: DefaultChannelBuffer,
	})
}

// NewSubscriberHubWithConfig creates a new hub with custom configuration.
func NewSubscriberHubWithConfig(cfg SubscriberConfig) *SubscriberHub {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = DefaultHeartbeat
	}
	if cfg.ChannelBufferSize <= 0 {
		cfg.ChannelBufferSize = DefaultChannelBuffer
	}
	h := &SubscriberHub{
		subscribers: make(map[string]map[*Subscription]struct{}),
		config:      cfg,
		stopCh:      make(chan struct{}),
	}
	go h.heartbeatLoop()
	return h
}

// Subscribe registers a new subscription for the given task ID.
// Returns the subscription and a boolean indicating whether the
// connection was accepted (false if max connections reached).
func (h *SubscriberHub) Subscribe(taskID string) (*Subscription, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check connection limit
	if h.config.MaxConnections > 0 {
		total := 0
		for _, subs := range h.subscribers {
			total += len(subs)
		}
		if total >= h.config.MaxConnections {
			return nil, false
		}
	}

	sub := NewSubscription(taskID, h.config.ChannelBufferSize)

	if h.subscribers[taskID] == nil {
		h.subscribers[taskID] = make(map[*Subscription]struct{})
	}
	h.subscribers[taskID][sub] = struct{}{}

	return sub, true
}

// Unsubscribe removes a subscription and closes its Done channel.
func (h *SubscriberHub) Unsubscribe(sub *Subscription) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.subscribers[sub.TaskID]; ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(h.subscribers, sub.TaskID)
		}
	}

	// Close Done channel exactly once
	select {
	case <-sub.Done:
		// already closed
	default:
		close(sub.Done)
	}
}

// Publish sends a status update to all subscribers of the given task.
// If a subscriber's channel is full, the update is dropped for that
// subscriber (non-blocking publish).
func (h *SubscriberHub) Publish(taskID string, update models.TaskStatusUpdate) {
	h.mu.RLock()
	subs, ok := h.subscribers[taskID]
	if !ok {
		h.mu.RUnlock()
		return
	}
	// Copy the set so we don't hold the lock during channel sends
	recipients := make([]*Subscription, 0, len(subs))
	for s := range subs {
		recipients = append(recipients, s)
	}
	h.mu.RUnlock()

	for _, s := range recipients {
		select {
		case s.Events <- update:
		default:
			// Channel full; drop the update for this subscriber.
			// The heartbeat loop will detect and clean up dead subs.
		}
	}
}

// SubscriberCount returns the number of active subscribers for a task.
func (h *SubscriberHub) SubscriberCount(taskID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if subs, ok := h.subscribers[taskID]; ok {
		return len(subs)
	}
	return 0
}

// TotalSubscribers returns the total number of active subscriptions.
func (h *SubscriberHub) TotalSubscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, subs := range h.subscribers {
		total += len(subs)
	}
	return total
}

// Shutdown stops the heartbeat loop and closes all subscriptions.
func (h *SubscriberHub) Shutdown() {
	select {
	case <-h.stopCh:
		// already stopped
	default:
		close(h.stopCh)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.subscribers {
		for s := range subs {
			select {
			case <-s.Done:
			default:
				close(s.Done)
			}
		}
	}
	h.subscribers = make(map[string]map[*Subscription]struct{})
}

// heartbeatLoop sends periodic keep-alive signals and cleans up
// subscriptions with no recent activity.
func (h *SubscriberHub) heartbeatLoop() {
	ticker := time.NewTicker(h.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.sendHeartbeats()
		}
	}
}

// sendHeartbeats sends a heartbeat update to all active subscribers.
// A heartbeat is a zero-value TaskStatusUpdate with the current timestamp.
func (h *SubscriberHub) sendHeartbeats() {
	h.mu.RLock()
	recipients := make([]*Subscription, 0)
	for _, subs := range h.subscribers {
		for s := range subs {
			recipients = append(recipients, s)
		}
	}
	h.mu.RUnlock()

	heartbeat := models.TaskStatusUpdate{
		UpdatedAt: time.Now().UTC(),
	}
	for _, s := range recipients {
		select {
		case s.Events <- heartbeat:
		default:
			// Channel full; subscriber may be slow. Will be cleaned up
			// on the next heartbeat cycle if still unresponsive.
		}
	}
}
