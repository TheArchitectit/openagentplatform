// Package bridge - rpc.go implements the RPC bridge that ties the
// AdapterClient (Python adapter service) to the A2A Gateway. It
// translates A2A task lifecycle events into adapter invocations and
// streams adapter responses back through the Gateway's SSE hub.
package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// Errors
// ============================================================

var (
	// ErrNilAdapterClient is returned when a nil AdapterClient is provided.
	ErrNilAdapterClient = errors.New("bridge: nil adapter client")

	// ErrNilGatewayForRPC is returned when a nil Gateway is provided.
	ErrNilGatewayForRPC = errors.New("bridge: nil gateway")

	// ErrTaskNotFound is returned when a task ID is not found.
	ErrTaskNotFound = errors.New("bridge: task not found")

	// ErrNoPreferredAdapter is returned when no adapter is specified and routing fails.
	ErrNoPreferredAdapter = errors.New("bridge: no preferred adapter available")
)

// ============================================================
// RPCBridge configuration
// ============================================================

// RPCConfig holds tuning parameters for the RPCBridge.
type RPCConfig struct {
	// CardSyncInterval is how often the bridge refreshes the adapter
	// list into the A2A registry. Default: 60s.
	CardSyncInterval time.Duration

	// Logger is an optional structured logger.
	Logger *slog.Logger
}

// Default RPC bridge configuration values.
const (
	defaultCardSyncInterval = 60 * time.Second
)

// ============================================================
// RPCBridge
// ============================================================

// RPCBridge ties the AdapterClient and Gateway together. It handles:
//
//   - Invoking adapters when A2A tasks are created
//   - Streaming adapter responses to Gateway SSE subscribers
//   - Cancelling adapter tasks when A2A tasks are cancelled
//   - Periodically syncing AgentCards from the adapter service
//     into the A2A registry
type RPCBridge struct {
	client *AdapterClient
	gw     *gateway.Gateway
	log    *slog.Logger
	cfg    RPCConfig

	mu      sync.Mutex
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// activeStreams tracks in-flight streaming invocations by task ID
	// so that Cancel can abort them.
	activeStreams   map[string]context.CancelFunc
	activeStreamsMu sync.Mutex
}

// NewRPCBridge constructs an RPCBridge.
func NewRPCBridge(client *AdapterClient, gw *gateway.Gateway, cfg RPCConfig) (*RPCBridge, error) {
	if client == nil {
		return nil, ErrNilAdapterClient
	}
	if gw == nil {
		return nil, ErrNilGatewayForRPC
	}

	interval := cfg.CardSyncInterval
	if interval <= 0 {
		interval = defaultCardSyncInterval
	}

	return &RPCBridge{
		client:       client,
		gw:           gw,
		log:          cfg.Logger,
		cfg:          RPCConfig{CardSyncInterval: interval, Logger: cfg.Logger},
		stopCh:       make(chan struct{}),
		activeStreams: make(map[string]context.CancelFunc),
	}, nil
}

// Start begins the periodic AgentCard sync loop.
func (rb *RPCBridge) Start() error {
	rb.mu.Lock()
	if rb.started {
		rb.mu.Unlock()
		return errors.New("bridge: rpc bridge already started")
	}
	rb.started = true
	rb.mu.Unlock()

	// Perform an initial sync immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := rb.SyncAgentCards(ctx); err != nil {
		if rb.log != nil {
			rb.log.Warn("bridge: initial card sync failed", "err", err)
		}
	}
	cancel()

	rb.wg.Add(1)
	go rb.cardSyncLoop()

	if rb.log != nil {
		rb.log.Info("bridge: rpc bridge started",
			"sync_interval", rb.cfg.CardSyncInterval,
		)
	}
	return nil
}

// Stop halts the card sync loop and cancels all active streams.
func (rb *RPCBridge) Stop() {
	rb.mu.Lock()
	if !rb.started {
		rb.mu.Unlock()
		return
	}
	rb.started = false
	rb.mu.Unlock()

	close(rb.stopCh)
	rb.wg.Wait()

	// Cancel all active streams
	rb.activeStreamsMu.Lock()
	for id, cancel := range rb.activeStreams {
		cancel()
		delete(rb.activeStreams, id)
	}
	rb.activeStreamsMu.Unlock()

	if rb.log != nil {
		rb.log.Info("bridge: rpc bridge stopped")
	}
}

// ============================================================
// Task dispatch
// ============================================================

// DispatchTask invokes the appropriate adapter for an A2A task.
// The adapter is selected from task metadata (key: "preferred_adapter")
// or, if absent, the task's AgentID field is used as the adapter name.
//
// On success, the task is updated with the adapter response and an SSE
// status update is published. On streaming tasks, events are forwarded
// to subscribers in real-time.
func (rb *RPCBridge) DispatchTask(ctx context.Context, task *models.Task) error {
	if task == nil {
		return errors.New("bridge: nil task")
	}

	adapter := rb.resolveAdapter(task)
	if adapter == "" {
		return ErrNoPreferredAdapter
	}

	// Transition to working
	working, err := rb.gw.UpdateTaskStatus(ctx, task.ID, "start", int(task.Version))
	if err != nil {
		return fmt.Errorf("bridge: transition to working: %w", err)
	}
	rb.publishUpdate(working)

	// Check if this is a streaming task
	isStreaming := false
	if task.Metadata != nil {
		isStreaming = task.Metadata["streaming"] == "true" || task.Metadata["streaming"] == "1"
	}

	if isStreaming {
		return rb.dispatchStreaming(ctx, working, adapter)
	}
	return rb.dispatchSync(ctx, working, adapter)
}

// resolveAdapter determines which adapter to use for a task.
// Checks metadata["preferred_adapter"] first, then AgentID.
// Falls back to "ozore" (the default hosted LLM agent) if neither is set.
func (rb *RPCBridge) resolveAdapter(task *models.Task) string {
	if task.Metadata != nil {
		if pref := task.Metadata["preferred_adapter"]; pref != "" {
			return pref
		}
	}
	if task.AgentID != "" {
		return task.AgentID
	}
	return "ozore" // default adapter
}

// dispatchSync handles a non-streaming invocation.
func (rb *RPCBridge) dispatchSync(ctx context.Context, task *models.Task, adapter string) error {
	messages := messageToParts(task.Message)
	resp, err := rb.client.Invoke(ctx, adapter, messages)
	if err != nil {
		// Mark task as failed
		failed, failErr := rb.gw.UpdateTaskStatus(ctx, task.ID, "fail", int(task.Version))
		if failErr != nil {
			if rb.log != nil {
				rb.log.Error("bridge: mark task failed",
					"task_id", task.ID,
					"err", failErr,
				)
			}
		}
		if failed != nil {
			rb.publishUpdate(failed)
		}
		return fmt.Errorf("bridge: invoke adapter %q: %w", adapter, err)
	}

	// Update task with response messages
	current, err := rb.gw.GetTaskInternal(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("bridge: get task after invoke: %w", err)
	}

	// Convert response Parts to a single message and add it
	if len(resp.Messages) > 0 {
		respMsg := models.Message{
			Role:  "agent",
			Parts: partsToModelsParts(resp.Messages),
		}
		if _, err := rb.gw.AddMessage(ctx, current.ID, respMsg, int(current.Version)); err != nil {
			if rb.log != nil {
				rb.log.Warn("bridge: add response message",
					"task_id", task.ID,
					"err", err,
				)
			}
		}
		current, _ = rb.gw.GetTaskInternal(ctx, task.ID)
	}

	// Transition to completed (or failed if error)
	event := "complete"
	if resp.ErrorMessage != "" {
		event = "fail"
	}
	if current == nil {
		updated, err := rb.gw.UpdateTaskStatus(ctx, task.ID, event, int(task.Version))
		if err != nil {
			return fmt.Errorf("bridge: transition after invoke: %w", err)
		}
		rb.publishUpdate(updated)
		return nil
	}
	updated, err := rb.gw.UpdateTaskStatus(ctx, task.ID, event, int(current.Version))
	if err != nil {
		return fmt.Errorf("bridge: transition after invoke: %w", err)
	}
	rb.publishUpdate(updated)

	return nil
}

// dispatchStreaming handles a streaming invocation. Events are forwarded
// to Gateway SSE subscribers in real-time.
func (rb *RPCBridge) dispatchStreaming(ctx context.Context, task *models.Task, adapter string) error {
	messages := messageToParts(task.Message)
	events, cancelStream, err := rb.client.Stream(ctx, adapter, messages)
	if err != nil {
		failed, _ := rb.gw.UpdateTaskStatus(ctx, task.ID, "fail", int(task.Version))
		if failed != nil {
			rb.publishUpdate(failed)
		}
		return fmt.Errorf("bridge: start stream for adapter %q: %w", adapter, err)
	}

	// Register the cancel function for later cancellation
	rb.activeStreamsMu.Lock()
	rb.activeStreams[task.ID] = cancelStream
	rb.activeStreamsMu.Unlock()

	defer func() {
		cancelStream()
		rb.activeStreamsMu.Lock()
		delete(rb.activeStreams, task.ID)
		rb.activeStreamsMu.Unlock()
	}()

	current := task

	for event := range events {
		// Process the event
		switch event.EventType {
		case "delta":
			// Forward delta content to SSE subscribers
			if event.Delta != nil {
				rb.publishUpdateRaw(current.ID, models.TaskStatusWorking, event.Delta.Text)
			}
		case "status":
			// Update task status
			if event.Status != "" {
				rb.publishUpdate(current)
			}
		case "error":
			failed, _ := rb.gw.UpdateTaskStatus(ctx, current.ID, "fail", int(current.Version))
			if failed != nil {
				rb.publishUpdate(failed)
			}
			return fmt.Errorf("bridge: stream error: %s", event.ErrorMessage)
		case "done":
			// Stream completed naturally
			return nil
		}

		// Forward status update to subscribers
		rb.publishUpdate(current)
	}

	return nil
}

// ============================================================
// Cancellation
// ============================================================

// CancelTask cancels both the A2A task and the underlying adapter task.
func (rb *RPCBridge) CancelTask(ctx context.Context, taskID string) error {
	// Cancel the active stream if any
	rb.activeStreamsMu.Lock()
	if cancel, ok := rb.activeStreams[taskID]; ok {
		cancel()
		delete(rb.activeStreams, taskID)
	}
	rb.activeStreamsMu.Unlock()

	// Get the current task
	task, err := rb.gw.GetTaskInternal(ctx, taskID)
	if err != nil {
		return fmt.Errorf("bridge: get task for cancel: %w", err)
	}

	adapter := rb.resolveAdapter(task)

	// Cancel the adapter task
	if adapter != "" && task.ID != "" {
		if _, err := rb.client.Cancel(ctx, adapter, task.ID); err != nil {
			if rb.log != nil {
				rb.log.Warn("bridge: adapter cancel",
					"task_id", taskID,
					"adapter", adapter,
					"err", err,
				)
			}
		}
	}

	// Cancel the A2A task via gateway
	identity := &gateway.Identity{
		Subject: "rpc-bridge",
		Method:  gateway.AuthNone,
		Scopes:  []string{gateway.PermSend},
	}
	if err := rb.gw.CancelTask(ctx, identity, taskID); err != nil {
		return fmt.Errorf("bridge: cancel task: %w", err)
	}

	return nil
}

// ============================================================
// AgentCard sync
// ============================================================

// SyncAgentCards fetches the adapter list from the Python service and
// registers each as an AgentCard in the A2A registry.
func (rb *RPCBridge) SyncAgentCards(ctx context.Context) error {
	adapters, err := rb.client.ListAdapters(ctx)
	if err != nil {
		return fmt.Errorf("bridge: list adapters: %w", err)
	}

	identity := &gateway.Identity{
		Subject: "rpc-bridge",
		Method:  gateway.AuthNone,
		Scopes:  []string{gateway.PermAdmin},
	}

	synced := 0
	for i := range adapters {
		info := &adapters[i]

		// Each AdapterInfo has a nested AgentCard from the Python contract.
		// Use the nested card directly as the registration source.
		card := info.AgentCard
		if card == nil {
			card = AgentCardFromAdapter(info)
		}

		// Ensure the card has an ID and Name from the adapter name
		// if the nested card is missing them.
		if card.ID == "" {
			card.ID = info.Name
		}
		if card.Name == "" {
			card.Name = info.Name
		}

		// Fetch the full card for richer metadata, overriding the
		// nested card fields if available.
		fullCard, err := rb.client.GetAdapterCard(ctx, info.Name)
		if err == nil && fullCard != nil {
			card = fullCard
			if card.ID == "" {
				card.ID = info.Name
			}
			if card.Name == "" {
				card.Name = info.Name
			}
		}

		if err := rb.gw.RegisterAgent(ctx, identity, card); err != nil {
			if rb.log != nil {
				rb.log.Warn("bridge: register agent card",
					"adapter", info.Name,
					"err", err,
				)
			}
			continue
		}
		synced++
	}

	if rb.log != nil {
		rb.log.Info("bridge: agent cards synced",
			"total", len(adapters),
			"synced", synced,
		)
	}
	return nil
}

// cardSyncLoop runs the periodic AgentCard sync.
func (rb *RPCBridge) cardSyncLoop() {
	defer rb.wg.Done()

	ticker := time.NewTicker(rb.cfg.CardSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rb.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := rb.SyncAgentCards(ctx); err != nil {
				if rb.log != nil {
					rb.log.Warn("bridge: periodic card sync", "err", err)
				}
			}
			cancel()
		}
	}
}

// ============================================================
// Helpers
// ============================================================

// publishUpdate publishes a status update derived from the current task state.
func (rb *RPCBridge) publishUpdate(task *models.Task) {
	if task == nil {
		return
	}
	rb.gw.Hub().Publish(task.ID, models.TaskStatusUpdate{
		TaskID:    task.ID,
		Status:    task.Status,
		UpdatedAt: task.UpdatedAt,
	})
}

// publishUpdateRaw publishes a status update with a custom status and message.
func (rb *RPCBridge) publishUpdateRaw(taskID, status, message string) {
	rb.gw.Hub().Publish(taskID, models.TaskStatusUpdate{
		TaskID:    taskID,
		Status:    status,
		Message:   message,
		UpdatedAt: time.Now().UTC(),
	})
}

// generateTaskID is a helper to generate a UUID v4 task ID.
func generateTaskID() string {
	return uuid.NewString()
}

// messageToParts converts an A2A models.Message into the bridge Part slice
// that the Python adapter service expects.
func messageToParts(msg models.Message) []Part {
	if len(msg.Parts) == 0 {
		return nil
	}
	parts := make([]Part, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		bp := Part{Type: "text", Text: p.Text}
		if p.File != nil {
			bp.Type = "file"
			bp.FileURL = p.File.URI
			bp.FileMIME = p.File.MimeType
		}
		parts = append(parts, bp)
	}
	return parts
}

// partsToModelsParts converts bridge Parts back into a2a models.Parts.
func partsToModelsParts(parts []Part) []models.Part {
	result := make([]models.Part, 0, len(parts))
	for _, p := range parts {
		mp := models.Part{Text: p.Text}
		if p.Type == "file" && p.FileURL != "" {
			mp.File = &models.FileRef{
				URI:      p.FileURL,
				MimeType: p.FileMIME,
			}
		}
		result = append(result, mp)
	}
	return result
}
