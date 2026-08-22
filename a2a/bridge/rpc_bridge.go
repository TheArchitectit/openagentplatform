package bridge

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

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
		client:        client,
		gw:            gw,
		log:           cfg.Logger,
		cfg:           RPCConfig{CardSyncInterval: interval, Logger: cfg.Logger},
		stopCh:        make(chan struct{}),
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
		current, err = rb.gw.GetTaskInternal(ctx, task.ID)
		if err != nil {
			if rb.log != nil {
				rb.log.Warn("bridge: get task after adding response",
					"task_id", task.ID,
					"err", err,
				)
			}
		}
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

