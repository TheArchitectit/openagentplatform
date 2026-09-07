// correlator.go — EDR host identifier → OAP agent_id mapping.
//
// Each EDR event includes a host identifier in its payload (CrowdStrike
// `device_id`, Defender `machineId`, SentinelOne `agent_id`). The
// correlator maps these to an OAP `agent_id` via the edr_agent_mapping
// table. When no mapping exists, it creates a virtual agent in the
// agents table (Platform = "edr/<provider>") and records the mapping.
// This matches the auto-enrollment pattern used for cloud resources
// (see internal/cloud/reconciler.go).
package ingest

import (
	"context"
	"fmt"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// MappingStore is the minimal persistence seam for edr_agent_mapping rows.
type MappingStore interface {
	GetByEDRHostID(ctx context.Context, provider models.EDRProviderType, edrHostID string) (*models.EDRAgentMapping, error)
	Create(ctx context.Context, m *models.EDRAgentMapping) error
	UpdateLastSeen(ctx context.Context, id string) error
}

// AgentStore is the minimal agent persistence seam needed for
// virtual-agent creation on first sight of an EDR host.
type AgentStore interface {
	GetByHostname(ctx context.Context, orgID, hostname string) (*models.Agent, error)
	CreateVirtual(ctx context.Context, a *models.Agent) error
	Get(ctx context.Context, id string) (*models.Agent, error)
}

// Correlator maps EDR events to OAP agents.
type Correlator struct {
	mappings MappingStore
	agents   AgentStore
}

// NewCorrelator builds a Correlator from the two stores.
func NewCorrelator(m MappingStore, a AgentStore) *Correlator {
	return &Correlator{mappings: m, agents: a}
}

// Correlate maps an EDR event to an OAP agent_id. If no mapping exists,
// it creates a virtual agent and records the mapping. Returns the empty
// string when the event payload has no host identifier (the caller
// treats this as "no agent to attribute to" and may still persist the
// event unattributed).
func (c *Correlator) Correlate(ctx context.Context, ev *models.SecurityEvent) (string, error) {
	hostID := eventHostID(ev)
	hostname := eventHostname(ev)
	if hostID == "" {
		return "", nil
	}

	// 1. Existing mapping?
	m, err := c.mappings.GetByEDRHostID(ctx, ev.Provider, hostID)
	if err == nil && m != nil {
		_ = c.mappings.UpdateLastSeen(ctx, m.ID)
		return m.AgentID, nil
	}

	// 2. Match by hostname against real agents.
	var agentID string
	if hostname != "" {
		existing, err := c.agents.GetByHostname(ctx, ev.OrgID, hostname)
		if err == nil && existing != nil {
			agentID = existing.ID
		}
	}

	// 3. Create a virtual agent if no match.
	if agentID == "" {
		agentID = fmt.Sprintf("edr-%s-%s", ev.Provider, hostID)
		if err := c.agents.CreateVirtual(ctx, &models.Agent{
			ID:              agentID,
			OrgID:           ev.OrgID,
			Hostname:        hostname,
			OperatingSystem: string(ev.Provider),
			Platform:        fmt.Sprintf("edr/%s", ev.Provider),
			Status:          "virtual",
			Tags:            []string{fmt.Sprintf("edr:provider:%s", ev.Provider)},
		}); err != nil {
			return "", err
		}
	}

	// 4. Persist the mapping so subsequent events find it.
	if err := c.mappings.Create(ctx, &models.EDRAgentMapping{
		ID:        ev.OrgID + "-" + string(ev.Provider) + "-" + hostID,
		OrgID:     ev.OrgID,
		Provider:  ev.Provider,
		EDRHostID: hostID,
		AgentID:   agentID,
		Hostname:  hostname,
	}); err != nil {
		return "", err
	}
	return agentID, nil
}

// eventHostID extracts the EDR-native host identifier from the event
// payload. Provider-specific keys are tried in order; the first one
// present wins.
func eventHostID(ev *models.SecurityEvent) string {
	for _, k := range []string{"device_id", "machine_id", "agent_id"} {
		if v, ok := ev.Payload[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// eventHostname extracts a human-readable hostname from the event
// payload. Used to match EDR events to real agents by hostname.
func eventHostname(ev *models.SecurityEvent) string {
	for _, k := range []string{"hostname", "agentHostName"} {
		if v, ok := ev.Payload[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
