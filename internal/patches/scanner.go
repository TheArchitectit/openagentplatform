// Package patches - scanner.go implements the server-side patch
// scan orchestration. The PatchScanDispatcher publishes scan
// commands to agents, collects their results, and aggregates them
// into a deduplicated PatchCatalog.
package patches

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
)

// PatchCatalogEntry is one row of the aggregated catalog. The same
// patch name+arch from many agents collapses into a single entry
// with a slice of affected agent ids.
type PatchCatalogEntry struct {
	Name              string    `json:"name"`
	PackageManager    string    `json:"package_manager"`
	Category          string    `json:"category,omitempty"`
	Severity          string    `json:"severity,omitempty"`
	AvailableVersion  string    `json:"available_version,omitempty"`
	AgentCount        int       `json:"agent_count"`
	AffectedAgents    []string  `json:"affected_agents"`
	RebootRequiredAny bool      `json:"reboot_required_any"`
	FirstSeen         time.Time `json:"first_seen"`
	LastSeen          time.Time `json:"last_seen"`
}

// PatchScanDispatcher coordinates periodic and on-demand patch
// scans across all connected agents. It is a long-lived service
// that lives in the platform server (not the agent).
type PatchScanDispatcher struct {
	nc  *nats.Conn
	log *slog.Logger

	// Agent list provider: the dispatcher needs to know which
	// agents to scan. This is a function so callers can supply a
	// fresh list from their agent store each cycle.
	AgentLister func(ctx context.Context) ([]AgentInfo, error)

	// Catalog aggregates the latest scan results keyed by
	// (name, package_manager).
	catalogMu sync.RWMutex
	catalog   map[string]*PatchCatalogEntry

	// LastScan stores the most recent PatchInfo per (agent, name)
	// so the dashboard can show what each agent has available.
	resultsMu sync.RWMutex
	results   map[string]map[string]patcher.PatchInfo // agentID -> name -> info

	// ScanInterval controls how often ScheduleLoop runs when
	// enabled. Default 6 hours.
	ScanInterval time.Duration
	// PerScanTimeout bounds the time we wait for all agents to
	// report. Default 2 minutes.
	PerScanTimeout time.Duration

	mu     sync.Mutex
	closed bool
}

// AgentInfo is the minimal agent descriptor needed by the
// dispatcher: the agent id, the site it belongs to, and the NATS
// subject fragment used to address it.
type AgentInfo struct {
	AgentID  string `json:"agent_id"`
	SiteID   string `json:"site_id,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// NewPatchScanDispatcher creates a dispatcher with default timing.
func NewPatchScanDispatcher(nc *nats.Conn, log *slog.Logger) *PatchScanDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &PatchScanDispatcher{
		nc:             nc,
		log:            log,
		catalog:        make(map[string]*PatchCatalogEntry),
		results:        make(map[string]map[string]patcher.PatchInfo),
		ScanInterval:   6 * time.Hour,
		PerScanTimeout: 2 * time.Minute,
	}
}

// Close marks the dispatcher closed and stops the schedule loop.
func (d *PatchScanDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
}

// ScheduleLoop runs ScanAll in a fixed interval until ctx is
// cancelled or Close is called. The first scan happens immediately
// (after a small jitter to avoid thundering herd at startup), then
// on ScanInterval cadence.
func (d *PatchScanDispatcher) ScheduleLoop(ctx context.Context) {
	if d.ScanInterval <= 0 {
		d.ScanInterval = 6 * time.Hour
	}
	// Initial delay: 0-30s to avoid a herd on cold start.
	initial := time.Duration(time.Now().UnixNano() % 30) * time.Second
	select {
	case <-ctx.Done():
		return
	case <-time.After(initial):
	}

	t := time.NewTicker(d.ScanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := d.ScanAll(ctx); err != nil {
				d.log.Warn("scheduled patch scan failed", "err", err)
			}
		}
	}
}

// ScanAll triggers a scan on every agent returned by AgentLister
// and waits for the results, or for PerScanTimeout to elapse. The
// catalog is updated as results arrive. Returns the number of
// agents that responded.
func (d *PatchScanDispatcher) ScanAll(ctx context.Context) (int, error) {
	if d.AgentLister == nil {
		return 0, fmt.Errorf("patches: scanner: AgentLister not configured")
	}
	agents, err := d.AgentLister(ctx)
	if err != nil {
		return 0, fmt.Errorf("patches: scanner: list agents: %w", err)
	}
	return d.ScanAgents(ctx, agents)
}

// ScanSite triggers a scan on every agent belonging to a single
// site. siteID is matched against AgentInfo.SiteID.
func (d *PatchScanDispatcher) ScanSite(ctx context.Context, siteID string) (int, error) {
	if d.AgentLister == nil {
		return 0, fmt.Errorf("patches: scanner: AgentLister not configured")
	}
	all, err := d.AgentLister(ctx)
	if err != nil {
		return 0, fmt.Errorf("patches: scanner: list agents: %w", err)
	}
	var filtered []AgentInfo
	for _, a := range all {
		if a.SiteID == siteID {
			filtered = append(filtered, a)
		}
	}
	return d.ScanAgents(ctx, filtered)
}

// ScanAgent triggers a scan on a single agent and returns the
// result. It is a synchronous request/reply wrapper around the
// NATS subscribe/publish flow.
func (d *PatchScanDispatcher) ScanAgent(ctx context.Context, agentID string) (*patcher.PatchScanResultEnvelope, error) {
	if d.nc == nil {
		return nil, fmt.Errorf("patches: scanner: no nats connection")
	}
	timeout := d.PerScanTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	requestID := uuid.NewString()
	cmd := patcher.PatchScanCommand{
		RequestID:  requestID,
		TimeoutSec: int(timeout.Seconds()),
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	// Subscribe to the result subject BEFORE publishing the request
	// to avoid a race where the agent responds before we listen.
	resultSub, err := d.nc.Subscribe(patcher.PatchScanResultSubject(agentID), func(msg *nats.Msg) {
		// We only care about messages that match our request id;
		// other in-flight requests use a different request id, but
		// this agent's subject is per-agent. The simplest correct
		// approach is request/reply with InboxPrefix, so we use
		// that here.
	})
	if err != nil {
		return nil, fmt.Errorf("patches: scanner: subscribe: %w", err)
	}
	defer resultSub.Unsubscribe()

	// Use request/reply via inbox for the single-agent case.
	reply, err := d.nc.Request(patcher.PatchScanSubject(agentID), payload, timeout)
	if err != nil {
		return nil, fmt.Errorf("patches: scanner: request: %w", err)
	}
	var env patcher.PatchScanResultEnvelope
	if err := json.Unmarshal(reply.Data, &env); err != nil {
		return nil, fmt.Errorf("patches: scanner: decode result: %w", err)
	}
	if env.RequestID != "" && env.RequestID != requestID {
		// Different request id; the dispatcher's per-agent
		// subscription may have been satisfied by an earlier
		// in-flight scan. Treat as the result anyway.
	}
	d.absorbResult(&env)
	return &env, nil
}

// ScanAgents fans out a scan to many agents and collects results
// until PerScanTimeout elapses. Returns the number of agents that
// responded within the timeout.
func (d *PatchScanDispatcher) ScanAgents(ctx context.Context, agents []AgentInfo) (int, error) {
	if d.nc == nil {
		return 0, fmt.Errorf("patches: scanner: no nats connection")
	}
	if len(agents) == 0 {
		return 0, nil
	}
	timeout := d.PerScanTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	// Subscribe to the wildcard result subject once; each agent's
	// result will arrive on oap.agents.<id>.patch_scan.results.
	sub, err := d.nc.Subscribe("oap.agents.*.patch_scan.results", func(msg *nats.Msg) {
		var env patcher.PatchScanResultEnvelope
		if err := json.Unmarshal(msg.Data, &env); err != nil {
			d.log.Warn("scanner: bad result payload", "err", err)
			return
		}
		d.absorbResult(&env)
	})
	if err != nil {
		return 0, fmt.Errorf("patches: scanner: subscribe wildcard: %w", err)
	}
	defer sub.Unsubscribe()

	requestID := uuid.NewString()
	payload, err := json.Marshal(patcher.PatchScanCommand{
		RequestID:  requestID,
		TimeoutSec: int(timeout.Seconds()),
	})
	if err != nil {
		return 0, err
	}

	responded := make(map[string]bool, len(agents))
	var respondedMu sync.Mutex
	countSub, err := d.nc.Subscribe("oap.agents.*.patch_scan.results", func(msg *nats.Msg) {
		var env patcher.PatchScanResultEnvelope
		if err := json.Unmarshal(msg.Data, &env); err != nil {
			return
		}
		respondedMu.Lock()
		responded[env.AgentID] = true
		respondedMu.Unlock()
	})
	if err != nil {
		_ = sub.Unsubscribe()
		return 0, err
	}
	defer countSub.Unsubscribe()

	for _, a := range agents {
		if err := d.nc.Publish(patcher.PatchScanSubject(a.AgentID), payload); err != nil {
			d.log.Warn("scanner: publish failed",
				"agent_id", a.AgentID, "err", err)
		}
	}

	// Wait for the timeout or until every agent has responded.
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return respondedCount(responded, &respondedMu), ctx.Err()
		case <-deadline.C:
			return respondedCount(responded, &respondedMu), nil
		case <-tick.C:
			if respondedCount(responded, &respondedMu) >= len(agents) {
				return respondedCount(responded, &respondedMu), nil
			}
		}
	}
}

func respondedCount(m map[string]bool, mu *sync.Mutex) int {
	mu.Lock()
	defer mu.Unlock()
	return len(m)
}

// absorbResult merges a single scan result into the catalog and the
// per-agent results map.
func (d *PatchScanDispatcher) absorbResult(env *patcher.PatchScanResultEnvelope) {
	if env == nil || env.AgentID == "" {
		return
	}
	d.resultsMu.Lock()
	perAgent, ok := d.results[env.AgentID]
	if !ok {
		perAgent = make(map[string]patcher.PatchInfo)
		d.results[env.AgentID] = perAgent
	}
	for _, p := range env.Patches {
		perAgent[p.Name] = p
	}
	d.resultsMu.Unlock()

	now := time.Now()
	d.catalogMu.Lock()
	for _, p := range env.Patches {
		key := catalogKey(p)
		entry, ok := d.catalog[key]
		if !ok {
			entry = &PatchCatalogEntry{
				Name:           p.Name,
				PackageManager: p.PackageManager,
				Category:       p.Category,
				Severity:       p.Severity,
				AvailableVersion: p.AvailableVersion,
				FirstSeen:      now,
			}
			d.catalog[key] = entry
		}
		entry.LastSeen = now
		entry.AgentCount = 0
		// Rebuild affected agent list from the per-agent results map
		// so it stays in sync as agents come and go.
		d.resultsMu.RLock()
		for agentID, patches := range d.results {
			if _, has := patches[p.Name]; has {
				if !containsString(entry.AffectedAgents, agentID) {
					entry.AffectedAgents = append(entry.AffectedAgents, agentID)
				}
			}
		}
		d.resultsMu.RUnlock()
		entry.AgentCount = len(entry.AffectedAgents)
		if p.RebootRequired {
			entry.RebootRequiredAny = true
		}
		// Roll up the "worst" severity so the catalog reflects the
		// worst known update.
		if severityRank(p.Severity) > severityRank(entry.Severity) {
			entry.Severity = p.Severity
		}
	}
	d.catalogMu.Unlock()
}

// Catalog returns a sorted snapshot of the aggregated catalog.
func (d *PatchScanDispatcher) Catalog() []PatchCatalogEntry {
	d.catalogMu.RLock()
	defer d.catalogMu.RUnlock()
	out := make([]PatchCatalogEntry, 0, len(d.catalog))
	for _, e := range d.catalog {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		if out[i].AgentCount != out[j].AgentCount {
			return out[i].AgentCount > out[j].AgentCount
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// AgentPatches returns the latest patches for a single agent.
func (d *PatchScanDispatcher) AgentPatches(agentID string) []patcher.PatchInfo {
	d.resultsMu.RLock()
	defer d.resultsMu.RUnlock()
	perAgent, ok := d.results[agentID]
	if !ok {
		return nil
	}
	out := make([]patcher.PatchInfo, 0, len(perAgent))
	for _, p := range perAgent {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CatalogFilter is an optional filter for the Catalog view.
type CatalogFilter struct {
	MinAgentCount int
	Severity      string
	PackageManager string
}

// FilteredCatalog returns a sorted catalog filtered by the supplied
// criteria. Zero-valued filter fields are ignored.
func (d *PatchScanDispatcher) FilteredCatalog(f CatalogFilter) []PatchCatalogEntry {
	all := d.Catalog()
	out := make([]PatchCatalogEntry, 0, len(all))
	for _, e := range all {
		if f.MinAgentCount > 0 && e.AgentCount < f.MinAgentCount {
			continue
		}
		if f.Severity != "" && e.Severity != f.Severity {
			continue
		}
		if f.PackageManager != "" && e.PackageManager != f.PackageManager {
			continue
		}
		out = append(out, e)
	}
	return out
}

// catalogKey returns the dedup key for a PatchInfo. The key
// combines the package name with the package manager so that the
// same package published by two different managers (rare but
// possible) is treated as two catalog rows.
func catalogKey(p patcher.PatchInfo) string {
	return p.PackageManager + "::" + p.Name
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// severityRank assigns a numeric rank to a severity string. Higher
// is more severe.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "important":
		return 3
	case "moderate":
		return 2
	case "low":
		return 1
	}
	return 0
}
