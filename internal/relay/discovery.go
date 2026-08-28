package relay

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// This file implements Phase 1 of the discovery federation successor sprint
// (RELAY-05 ADR §6 Phase 1): the local DiscoveryRegistry. It reuses the
// existing AgentCard model from the a2a-agent-registry spec (a2a/models),
// wraps it in a federation envelope (provenance + visibility + TTL + version),
// and resolves queries against per-tenant visibility rules. Federation RPCs
// (PushRecord/PullRecords/Ping) live in discovery_grpc.go; this file has no
// network I/O.

// VisibilityScope controls who may resolve a discovery record (RELAY-05 ADR
// §1.2, §4). Default is fully private; cross-tenant visibility is opt-in.
type VisibilityScope string

const (
	VisibilityTenantPrivate     VisibilityScope = "tenant_private"
	VisibilityTenantAllowlisted VisibilityScope = "tenant_allowlisted"
	VisibilityGlobalPublic      VisibilityScope = "global_public"
)

// Provenance records who published a discovery record, where, and when. The
// origin relay is authoritative for the record (ADR §1.3, §3).
type Provenance struct {
	OriginRelayID  string    `json:"origin_relay_id"`
	TenantID       string    `json:"tenant_id"`
	PublishedAt    time.Time `json:"published_at"`
	PublisherAgent string    `json:"publisher_agent"`
	Signature      []byte    `json:"signature,omitempty"` // Ed25519 by origin relay CA key
}

// Visibility bounds who may resolve the record (ADR §1.2, §4).
type Visibility struct {
	Scope     VisibilityScope `json:"scope"`
	Allowlist []string        `json:"allowlist,omitempty"`
}

// DiscoveryEnvelope wraps an AgentCard with federation metadata (ADR §1.2).
type DiscoveryEnvelope struct {
	Record     models.AgentCard `json:"record"`
	Provenance Provenance       `json:"provenance"`
	Visibility Visibility       `json:"visibility"`
	TTL        time.Duration    `json:"_"`
	Version    uint64           `json:"version"`
}

// DiscoveryRegistry is the in-memory discovery store. It is authoritative for
// records whose Provenance.OriginRelayID matches its own relay ID; records
// from other relays arrive later via federation (Phase 2).
type DiscoveryRegistry struct {
	mu                 sync.RWMutex
	relayID            string
	records            map[string]*DiscoveryEnvelope // key: Record.ID (agent ID)
	withdrawn          map[string]uint64             // agent ID -> withdraw version tombstone
	operatorAllowlists map[string][]string           // tenant ID -> tenant IDs it may see
	log                *slog.Logger
	onUpdate           func(env *DiscoveryEnvelope, withdraw bool) // federation fan-out (ADR §2.3)
}

// NewDiscoveryRegistry creates an empty registry bound to the given relay ID.
func NewDiscoveryRegistry(relayID string, log *slog.Logger) *DiscoveryRegistry {
	if log == nil {
		log = slog.Default()
	}
	return &DiscoveryRegistry{
		relayID:            relayID,
		records:            make(map[string]*DiscoveryEnvelope),
		withdrawn:          make(map[string]uint64),
		operatorAllowlists: make(map[string][]string),
		log:                log,
	}
}

// SetObserver installs a callback fired after a successful local Publish or
// Withdraw, so the federation layer can fan the change out to peers (ADR §2.3).
// It is invoked without holding the registry lock.
func (d *DiscoveryRegistry) SetObserver(fn func(env *DiscoveryEnvelope, withdraw bool)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onUpdate = fn
}

// SetOperatorAllowlists installs the MSP-wide per-tenant allowlists (ADR §4.2).
// These are broader grants configured in trust config; a record's explicit
// tenant_private scope always takes precedence over them.
func (d *DiscoveryRegistry) SetOperatorAllowlists(allowlists map[string][]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.operatorAllowlists = allowlists
}

// Publish stores a record. Fails on a missing agent ID, a version not strictly
// higher than the stored/tombstoned version (replay prevention, ADR §1.6), an
// over-long TTL (>24h, ADR §1.5), or a mismatch between the record owner and
// the caller's tenant (caller is pre-authenticated by the WSS/admin boundary).
func (d *DiscoveryRegistry) Publish(env *DiscoveryEnvelope) error {
	if env == nil {
		return errors.New("discovery: nil envelope")
	}
	id := env.Record.ID
	if id == "" {
		return errors.New("discovery: agent id required")
	}
	if env.Provenance.TenantID == "" {
		return errors.New("discovery: provenance tenant required")
	}
	if env.TTL <= 0 {
		return errors.New("discovery: ttl must be positive")
	}
	if env.TTL > 24*time.Hour {
		return errors.New("discovery: ttl exceeds 24h maximum")
	}
	if env.Version == 0 {
		return errors.New("discovery: version must be positive")
	}

	d.mu.Lock()
	if withdrawn := d.withdrawn[id]; env.Version <= withdrawn {
		d.mu.Unlock()
		return fmt.Errorf("discovery: version %d is not above withdrawn version %d", env.Version, withdrawn)
	}
	if existing, ok := d.records[id]; ok && env.Version <= existing.Version {
		d.mu.Unlock()
		return fmt.Errorf("discovery: version %d is not above stored version %d", env.Version, existing.Version)
	}

	cp := *env
	d.records[id] = &cp
	obs := d.onUpdate
	d.mu.Unlock()

	d.log.Info("discovery: record published",
		"agent_id", id, "tenant_id", env.Provenance.TenantID,
		"version", env.Version, "scope", string(env.Visibility.Scope))
	if obs != nil {
		obs(&cp, false)
	}
	return nil
}

// Withdraw removes a record and records a tombstone at the given version so
// stale re-publishes are suppressed (ADR §1.5). Returns an error if the agent
// does not exist or the withdraw version is not above the current version.
func (d *DiscoveryRegistry) Withdraw(agentID string, tenantID string, version uint64) error {
	if agentID == "" {
		return errors.New("discovery: agent id required")
	}
	if version == 0 {
		return errors.New("discovery: version must be positive")
	}

	d.mu.Lock()
	existing, ok := d.records[agentID]
	if !ok {
		d.mu.Unlock()
		return errors.New("discovery: record_not_found")
	}
	if existing.Provenance.TenantID != tenantID {
		d.mu.Unlock()
		return errors.New("discovery: tenant_not_owner")
	}
	if version <= existing.Version {
		d.mu.Unlock()
		return fmt.Errorf("discovery: withdraw version %d not above stored version %d", version, existing.Version)
	}

	delete(d.records, agentID)
	d.withdrawn[agentID] = version
	obs := d.onUpdate
	d.mu.Unlock()

	d.log.Info("discovery: record withdrawn",
		"agent_id", agentID, "tenant_id", tenantID, "version", version)
	if obs != nil {
		// synthesizes the withdraw envelope peers need for a tombstone
		// (ADR §1.5: withdraw carries the same provenance + higher version).
		obs(&DiscoveryEnvelope{
			Record: models.AgentCard{ID: agentID},
			Provenance: Provenance{
				OriginRelayID: d.relayID,
				TenantID:      tenantID,
			},
			Version: version,
		}, true)
	}
	return nil
}

// Resolve returns records visible to the caller's tenant, optionally filtered
// by skill (ADR §4.4). Records whose owning tenant matches the caller are
// always included; cross-tenant records are included only when the record's
// scope and the owner-side operator allowlist permit. Sorted by matching skill
// (exact ID first, name second, none last), then higher version first.
func (d *DiscoveryRegistry) Resolve(tenantID string, skill string) []*DiscoveryEnvelope {
	d.mu.RLock()
	defer d.mu.RUnlock()

	results := make([]*DiscoveryEnvelope, 0, len(d.records))
	for _, env := range d.records {
		if env.Provenance.TenantID != tenantID &&
			!d.crossTenantAllowed(tenantID, env) {
			continue
		}
		if skillMatches(env, skill) {
			results = append(results, env)
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := envSkillRank(results[i], skill), envSkillRank(results[j], skill)
		if ri != rj {
			return ri < rj
		}
		return results[i].Version > results[j].Version
	})
	return results
}

// crossTenantAllowed evaluates the rule for a record owned by a tenant other
// than the caller (ADR §4.4 steps a–c). The record's explicit scope wins over
// operator allowlists; tenant_private is never visible cross-tenant.
func (d *DiscoveryRegistry) crossTenantAllowed(callerTenant string, env *DiscoveryEnvelope) bool {
	owning := env.Provenance.TenantID
	switch env.Visibility.Scope {
	case VisibilityGlobalPublic:
		return true
	case VisibilityTenantAllowlisted:
		for _, t := range env.Visibility.Allowlist {
			if t == callerTenant {
				return true
			}
		}
		// Owner-side operator allowlist: the owning tenant's allowlist names
		// caller tenants it permits (broader grant, ADR §4.2).
		for _, t := range d.operatorAllowlists[owning] {
			if t == callerTenant {
				return true
			}
		}
		return false
	default: // tenant_private
		return false
	}
}

func skillMatches(env *DiscoveryEnvelope, skill string) bool {
	if skill == "" {
		return true
	}
	for _, s := range env.Record.Skills {
		if s.ID == skill || s.Name == skill {
			return true
		}
	}
	return false
}

func envSkillRank(env *DiscoveryEnvelope, skill string) int {
	if skill == "" {
		return 1
	}
	for _, s := range env.Record.Skills {
		if s.ID == skill {
			return 0
		}
		if s.Name == skill {
			return 1
		}
	}
	return 2
}

// Expire removes records past their TTL (ADR §1.5 self-cleaning). Returns the
// number dropped.
func (d *DiscoveryRegistry) Expire(now time.Time) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	dropped := 0
	for id, env := range d.records {
		if now.Sub(env.Provenance.PublishedAt) >= env.TTL {
			delete(d.records, id)
			dropped++
			d.log.Info("discovery: record expired",
				"agent_id", id, "tenant_id", env.Provenance.TenantID)
		}
	}
	return dropped
}

// Snapshot returns a copy of all records (for the admin listing route).
func (d *DiscoveryRegistry) Snapshot() map[string]*DiscoveryEnvelope {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]*DiscoveryEnvelope, len(d.records))
	for id, env := range d.records {
		cp := *env
		out[id] = &cp
	}
	return out
}
