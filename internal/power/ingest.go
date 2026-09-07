// Package power implements the power/UPS state ingest pipeline. It
// detects power state transitions in check results, appends them to
// the append-only power_state_log table, and emits alerts via a
// pluggable sink (the default is a NATS publisher).
package power

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// PowerStateLogStore appends entries to the audit trail.
type PowerStateLogStore interface {
	Append(ctx context.Context, entry *models.PowerStateLog) error
}

// EventSink is the seam the Ingestor uses to publish power events to
// downstream consumers (alert engine, SIEM forwarders, etc.). The
// default server wiring plugs in a NATS publisher; tests use a no-op.
type EventSink interface {
	PublishPowerEvent(ctx context.Context, event PowerEvent)
}

// PowerEvent is the published event shape.
type PowerEvent struct {
	OrgID          string                `json:"org_id"`
	AgentID        string                `json:"agent_id"`
	Source         models.PowerSource    `json:"source"`
	EventType      models.PowerEventType `json:"event_type"`
	PreviousStatus string                `json:"previous_status,omitempty"`
	CurrentStatus  string                `json:"current_status"`
	BatteryPercent *int                  `json:"battery_percent,omitempty"`
	OccurredAt     time.Time             `json:"occurred_at"`
}

// Ingestor is the per-result power-state transition detector.
type Ingestor struct {
	store PowerStateLogStore
	sink  EventSink
	log   *slog.Logger
}

func NewIngestor(store PowerStateLogStore, sink EventSink, log *slog.Logger) *Ingestor {
	if log == nil {
		log = slog.Default()
	}
	return &Ingestor{store: store, sink: sink, log: log}
}

type powerTransition struct {
	Source         string `json:"source"`
	PowerTransition string `json:"power_transition"`
	PreviousStatus string `json:"previous_status"`
	CurrentStatus  string `json:"current_status"`
	BatteryPercent *int   `json:"battery_percent,omitempty"`
}

// HandleResult inspects a check result payload for a power transition.
// If the result contains a power_transition, the Ingestor appends to
// the state log and emits an event via the sink. Safe to call on every
// result; the body is parsed only when data is non-empty.
func (i *Ingestor) HandleResult(ctx context.Context, orgID, agentID, checkID string, data []byte) {
	if len(data) == 0 {
		return
	}
	var p powerTransition
	if err := json.Unmarshal(data, &p); err != nil {
		return
	}
	if p.PowerTransition == "" {
		return
	}

	source := models.PowerSourceUPS
	if p.Source == "battery" {
		source = models.PowerSourceBattery
	}
	eventType := mapTransitionToEventType(source, p.PowerTransition, p.BatteryPercent)
	now := time.Now().UTC()

	entry := &models.PowerStateLog{
		ID:             orgID + "-" + agentID + "-" + now.Format(time.RFC3339Nano),
		OrgID:          orgID,
		AgentID:        agentID,
		Source:         source,
		EventType:      eventType,
		PreviousStatus: p.PreviousStatus,
		CurrentStatus:  p.CurrentStatus,
		BatteryPercent: p.BatteryPercent,
		OccurredAt:     now,
	}
	if i.store != nil {
		if err := i.store.Append(ctx, entry); err != nil {
			i.log.Warn("power: state log append failed", "agent", agentID, "err", err)
		}
	}

	if i.sink != nil {
		i.sink.PublishPowerEvent(ctx, PowerEvent{
			OrgID:          orgID,
			AgentID:        agentID,
			Source:         source,
			EventType:      eventType,
			PreviousStatus: p.PreviousStatus,
			CurrentStatus:  p.CurrentStatus,
			BatteryPercent: p.BatteryPercent,
			OccurredAt:     now,
		})
	}

	i.log.Info("power: transition",
		"agent", agentID, "check", checkID,
		"source", source, "event_type", eventType,
		"previous", p.PreviousStatus, "current", p.CurrentStatus,
	)
}

// mapTransitionToEventType maps a raw transition string from a check
// result to a canonical PowerEventType.
func mapTransitionToEventType(source models.PowerSource, transition string, percent *int) models.PowerEventType {
	switch transition {
	case "on_battery":
		return models.PowerEventOnBattery
	case "on_line":
		return models.PowerEventOnLine
	case "discharging":
		if percent != nil && *percent < 20 {
			return models.PowerEventLowBattery
		}
		return models.PowerEventDischarging
	case "charging":
		return models.PowerEventCharging
	case "full":
		return models.PowerEventFull
	case "critical":
		return models.PowerEventBatteryCrit
	default:
		return models.PowerEventType(transition)
	}
}
