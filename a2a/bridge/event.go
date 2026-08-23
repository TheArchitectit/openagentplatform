// Package bridge — event.go defines the 8 RMM event types and their
// mapping to A2A skill tags. Mappings are configurable: an operator can
// disable delegation for any event type without a code change.
package bridge

import (
	"fmt"
	"time"
)

// ============================================================
// RMM Event Types
// ============================================================

// RMMEventType enumerates the eight event types the bridge understands.
type RMMEventType string

const (
	EventCheckFailure        RMMEventType = "check_failure"
	EventAlertFired          RMMEventType = "alert_fired"
	EventPatchAvailable      RMMEventType = "patch_available"
	EventPatchApprovalNeeded RMMEventType = "patch_approval_needed"
	EventScriptError         RMMEventType = "script_error"
	EventAgentOffline        RMMEventType = "agent_offline"
	EventComplianceViolation RMMEventType = "compliance_violation"
	EventSecurityEvent       RMMEventType = "security_event"
)

// AllEventTypes returns the full set of recognized event types.
func AllEventTypes() []RMMEventType {
	return []RMMEventType{
		EventCheckFailure,
		EventAlertFired,
		EventPatchAvailable,
		EventPatchApprovalNeeded,
		EventScriptError,
		EventAgentOffline,
		EventComplianceViolation,
		EventSecurityEvent,
	}
}

// ============================================================
// Event → Skill Mapping
// ============================================================

// EventMapping defines how a single RMM event type maps to an A2A skill tag.
type EventMapping struct {
	// EventType is the RMM event type (e.g. "check_failure").
	EventType RMMEventType `json:"event_type"`

	// SkillTag is the A2A skill tag to attach to created tasks (e.g. "alert.triage").
	SkillTag string `json:"skill_tag"`

	// Enabled controls whether this mapping produces tasks.
	Enabled bool `json:"enabled"`

	// Description is a human-readable explanation (for docs / dashboards).
	Description string `json:"description,omitempty"`
}

// DefaultMappings returns the 8 standard event→skill mappings, all enabled.
func DefaultMappings() []EventMapping {
	return []EventMapping{
		{EventType: EventCheckFailure, SkillTag: "alert.triage", Enabled: true,
			Description: "Check result crosses failure threshold"},
		{EventType: EventAlertFired, SkillTag: "alert.correlation", Enabled: true,
			Description: "New alert created"},
		{EventType: EventPatchAvailable, SkillTag: "patch.planning", Enabled: true,
			Description: "New patches detected by scan"},
		{EventType: EventPatchApprovalNeeded, SkillTag: "patch.risk_assessment", Enabled: true,
			Description: "Patch awaiting approval"},
		{EventType: EventScriptError, SkillTag: "script.debugging", Enabled: true,
			Description: "Script execution failed"},
		{EventType: EventAgentOffline, SkillTag: "agent.recovery", Enabled: true,
			Description: "Agent heartbeat TTL exceeded"},
		{EventType: EventComplianceViolation, SkillTag: "compliance.remediation", Enabled: true,
			Description: "Policy violation detected"},
		{EventType: EventSecurityEvent, SkillTag: "security.investigation", Enabled: true,
			Description: "Security-related alert"},
	}
}

// ============================================================
// RMM Event (incoming)
// ============================================================

// RMMEvent is the envelope published by the RMM alert engine onto NATS.
type RMMEvent struct {
	// ID is the unique event identifier (used for deduplication).
	ID string `json:"id"`

	// Type is the event type (one of the 8 recognized types).
	Type RMMEventType `json:"type"`

	// CorrelationID links related events (e.g. recurring check failures).
	CorrelationID string `json:"correlation_id,omitempty"`

	// DedupKey is the alert-engine dedup key; the bridge reuses it.
	DedupKey string `json:"dedup_key,omitempty"`

	// Severity is the event severity (critical, high, medium, low, info).
	Severity string `json:"severity"`

	// SourceAgentID identifies the agent that generated the event.
	SourceAgentID string `json:"source_agent_id"`

	// OrgID is the organization scope.
	OrgID string `json:"org_id,omitempty"`

	// ClientID is the client scope.
	ClientID string `json:"client_id,omitempty"`

	// SiteID is the site scope.
	SiteID string `json:"site_id,omitempty"`

	// AffectedResource is the resource the event pertains to.
	AffectedResource string `json:"affected_resource,omitempty"`

	// Payload carries event-specific structured data.
	Payload map[string]any `json:"payload,omitempty"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`
}

// ============================================================
// Mapping Registry
// ============================================================

// MappingRegistry holds the active event→skill mappings and provides lookup.
type MappingRegistry struct {
	mappings map[RMMEventType]EventMapping
}

// NewMappingRegistry creates a registry from the given mappings.
func NewMappingRegistry(mappings []EventMapping) *MappingRegistry {
	r := &MappingRegistry{mappings: make(map[RMMEventType]EventMapping, len(mappings))}
	for _, m := range mappings {
		r.mappings[m.EventType] = m
	}
	return r
}

// Lookup returns the mapping for an event type and whether it exists and is enabled.
func (r *MappingRegistry) Lookup(et RMMEventType) (EventMapping, bool) {
	m, ok := r.mappings[et]
	if !ok || !m.Enabled {
		return EventMapping{}, false
	}
	return m, true
}

// SetEnabled toggles a mapping on or off at runtime.
func (r *MappingRegistry) SetEnabled(et RMMEventType, enabled bool) error {
	m, ok := r.mappings[et]
	if !ok {
		return fmt.Errorf("mapping: unknown event type %q", et)
	}
	m.Enabled = enabled
	r.mappings[et] = m
	return nil
}

// Types returns all registered event types.
func (r *MappingRegistry) Types() []RMMEventType {
	out := make([]RMMEventType, 0, len(r.mappings))
	for et := range r.mappings {
		out = append(out, et)
	}
	return out
}
