package models

import "time"

// PowerSource identifies the kind of power monitor that produced a
// transition event — either a network-attached UPS daemon or the agent's
// own battery telemetry.
type PowerSource string

const (
	PowerSourceUPS     PowerSource = "ups"
	PowerSourceBattery PowerSource = "battery"
)

// String returns the canonical lowercase identifier.
func (s PowerSource) String() string { return string(s) }

// PowerEventType enumerates the power state transitions an OAP power
// monitor can emit. These are the canonical labels used in
// power_state_log.event_type and as the message of the corresponding
// oap.events.alerts payload.
type PowerEventType string

const (
	PowerEventOnBattery   PowerEventType = "on_battery"
	PowerEventOnLine      PowerEventType = "on_line"
	PowerEventLowBattery  PowerEventType = "low_battery"
	PowerEventBatteryCrit PowerEventType = "battery_critical"
	PowerEventCharging    PowerEventType = "charging"
	PowerEventDischarging PowerEventType = "discharging"
	PowerEventFull        PowerEventType = "full"
)

// String returns the canonical lowercase identifier.
func (t PowerEventType) String() string { return string(t) }

// PowerStateLog is one row per detected power-state transition. The table
// is append-only (no updates, no deletes in normal flow) so the audit
// trail is preserved across restarts. A state machine (alert engine) can
// derive "currently on battery" from the latest row per (agent, source).
type PowerStateLog struct {
	ID             string         `json:"id"`
	OrgID          string         `json:"org_id"`
	AgentID        string         `json:"agent_id"`
	Source         PowerSource    `json:"source"`
	EventType      PowerEventType `json:"event_type"`
	PreviousStatus string         `json:"previous_status,omitempty"`
	CurrentStatus  string         `json:"current_status"`
	BatteryPercent *int           `json:"battery_percent,omitempty"`
	OccurredAt     time.Time      `json:"occurred_at"`
}
