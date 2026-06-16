// Package collectors provides compliance data collectors that agents
// execute on their hosts. Each collector gathers evidence about a
// specific compliance domain (antivirus, firewall, encryption, etc.)
// and returns a typed ComplianceData payload that the policy engine
// stores and uses for re-evaluation.
package collectors

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ComplianceData is the structured evidence produced by a collector.
// It is JSON-serialisable and stored in the agent_compliance_data table.
// The Collector field is always set; the rest of the fields are
// collector-specific and stored as opaque data for policy evaluation.
type ComplianceData struct {
	// Collector is the name of the collector that produced this data.
	Collector string `json:"collector"`
	// AgentID is set by the registry wrapper to identify the source.
	AgentID string `json:"agent_id"`
	// CollectedAt is the timestamp the data was gathered.
	CollectedAt time.Time `json:"collected_at"`
	// Platform identifies the OS that produced the data.
	Platform string `json:"platform"`
	// Compliant is a hint set by simple collectors; policy engines may
	// ignore it and derive their own verdict from the fields below.
	Compliant bool `json:"compliant"`
	// Message is a human-readable summary.
	Message string `json:"message,omitempty"`
	// Fields holds the structured evidence. Policies typically read
	// this map via Rego input or a custom builtin.
	Fields map[string]interface{} `json:"fields"`
}

// Collector is the interface every compliance collector implements.
// Agents register collectors on startup and the policy engine
// dispatches collection requests to the appropriate agent.
type Collector interface {
	// Name returns the unique collector name (e.g. "antivirus",
	// "firewall"). Used as a key in the registry and as the
	// oap.agents.<id>.compliance request payload.
	Name() string
	// Collect gathers compliance evidence from the host. The agentID
	// is the OAP-assigned identifier of the agent running the
	// collector; it is included in the returned data for traceability.
	Collect(ctx context.Context, agentID string) (*ComplianceData, error)
}

// CollectorRegistry holds the set of collectors available to an agent.
// It is safe for concurrent use.
type CollectorRegistry struct {
	mu        sync.RWMutex
	collectors map[string]Collector
}

// NewCollectorRegistry creates an empty registry.
func NewCollectorRegistry() *CollectorRegistry {
	return &CollectorRegistry{
		collectors: make(map[string]Collector),
	}
}

// Register adds a collector. Duplicate names overwrite; the caller is
// expected to register each collector once at startup.
func (r *CollectorRegistry) Register(c Collector) error {
	if c == nil {
		return fmt.Errorf("collectors: nil collector")
	}
	name := c.Name()
	if name == "" {
		return fmt.Errorf("collectors: collector name required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors[name] = c
	return nil
}

// Get returns a collector by name, or an error if not registered.
func (r *CollectorRegistry) Get(name string) (Collector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.collectors[name]
	if !ok {
		return nil, fmt.Errorf("collectors: unknown collector: %s", name)
	}
	return c, nil
}

// List returns the names of all registered collectors, sorted by
// registration order (insertion order via map iteration is
// non-deterministic; callers should sort if needed).
func (r *CollectorRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.collectors))
	for name := range r.collectors {
		out = append(out, name)
	}
	return out
}

// Collect dispatches a collection request to the named collector and
// stamps the returned data with the agent ID and timestamp.
func (r *CollectorRegistry) Collect(ctx context.Context, name, agentID string) (*ComplianceData, error) {
	c, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	data, err := c.Collect(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("collectors: %s: %w", name, err)
	}
	if data == nil {
		return nil, fmt.Errorf("collectors: %s: nil result", name)
	}
	if data.Collector == "" {
		data.Collector = name
	}
	if data.AgentID == "" {
		data.AgentID = agentID
	}
	if data.CollectedAt.IsZero() {
		data.CollectedAt = time.Now()
	}
	return data, nil
}
