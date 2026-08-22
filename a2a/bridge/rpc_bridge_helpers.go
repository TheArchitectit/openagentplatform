package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

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
