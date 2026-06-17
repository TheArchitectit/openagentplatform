package api

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

// diagResponse is the wire shape for /api/v1/diagnostics. The fields
// are aggregated counts and latencies, not row-level data, so it is
// safe to expose to admin-role callers.
type diagResponse struct {
	Timestamp      string         `json:"timestamp"`
	AgentsOnline   int            `json:"agents_online"`
	AlertsOpen     int            `json:"alerts_open"`
	ChecksFailing  int            `json:"checks_failing"`
	ActivePatchJobs int           `json:"active_patch_jobs"`
	ActiveSessions int            `json:"active_sessions"`
	DBLatencyMS    int64          `json:"db_latency_ms"`
	NATSLatencyMS  int64          `json:"nats_latency_ms"`
	AdapterHealth  map[string]any `json:"adapter_health"`
	UptimeSeconds  int64          `json:"uptime_seconds"`
	GoroutineCount int            `json:"goroutine_count"`
	MemoryUsageMB  float64        `json:"memory_usage_mb"`
	BuildInfo      any            `json:"build_info"`
}

// diagConnectionsResponse describes live connection state for WebSocket
// and NATS clients. Operators use this to spot leaked connections or
// detect when an agent fleet drops off.
type diagConnectionsResponse struct {
	Timestamp string         `json:"timestamp"`
	WebSocket map[string]any `json:"websocket"`
	NATS      map[string]any `json:"nats"`
}

// handleDiagnostics returns a system health summary for the admin
// dashboard. Each field is computed defensively — missing stores yield
// zero rather than a 500 so the dashboard can always render something
// useful.
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	resp := diagResponse{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		AdapterHealth:  s.collectAdapterHealth(ctx),
		UptimeSeconds:  int64(time.Since(s.startedAt).Seconds()),
		GoroutineCount: runtime.NumGoroutine(),
		MemoryUsageMB:  float64(m.Alloc) / (1024 * 1024),
		BuildInfo:      telemetry.GetBuildInfo(),
	}

	// Database round-trip latency — a simple Ping is enough to give
	// operators a feel for pool health.
	if s.db != nil {
		start := time.Now()
		if err := s.db.Ping(ctx); err == nil {
			resp.DBLatencyMS = time.Since(start).Milliseconds()
		} else {
			resp.DBLatencyMS = -1
		}
	}

	// NATS connection latency — IsConnected is O(1) so we report the
	// round-trip cost of a Status check as a proxy.
	if s.eventBus != nil {
		start := time.Now()
		if nc := s.eventBus.Conn(); nc != nil {
			_ = nc.Status() // touch the status code path
			resp.NATSLatencyMS = time.Since(start).Milliseconds()
		}
	}

	// Agent counts — only attempt when the DB is wired up. The
	// ListAgents call tolerates a missing table in pgAgentStore.
	if s.db != nil {
		store := s.agentStore()
		online := "online"
		agents, _, err := store.ListAgents(ctx, AgentListFilter{Status: online, Limit: 0})
		if err == nil {
			resp.AgentsOnline = len(agents)
		}
	}

	// Open alerts — sum across active states (firing + acknowledged).
	// The AlertFilter struct only supports a single State value, so we
	// issue one query per state and aggregate the totals.
	if s.alertStore != nil {
		for _, state := range []string{"firing", "acknowledged"} {
			_, total, err := s.alertStore.ListAlerts(ctx, alerts.AlertFilter{State: state, Limit: 0})
			if err == nil {
				resp.AlertsOpen += total
			}
		}
	}

	// Active patch jobs — any non-terminal state is considered active.
	if s.patchStore != nil {
		// Sum across the three in-flight states. Each call returns
		// the total count without loading rows.
		for _, state := range []string{"pending", "approved", "running"} {
			_, total, err := s.patchStore.ListPatchJobs(ctx, patches.PatchJobFilter{State: state, Limit: 0})
			if err == nil {
				resp.ActivePatchJobs += total
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDiagnosticsConnections returns live WebSocket and NATS
// connection state. This is a sibling of /api/v1/diagnostics that
// focuses on transport-layer visibility.
func (s *Server) handleDiagnosticsConnections(w http.ResponseWriter, _ *http.Request) {
	ws := map[string]any{}
	if s.wsHub != nil {
		ws["client_count"] = s.wsHub.clientCount()
		ws["subscriptions"] = s.wsHub.subscriptionCount()
	} else {
		ws["client_count"] = 0
		ws["subscriptions"] = 0
	}

	nats := map[string]any{}
	if s.eventBus != nil {
		if nc := s.eventBus.Conn(); nc != nil {
			nats["connected"] = nc.IsConnected()
			nats["server_url"] = nc.ConnectedUrl()
			nats["server_id"] = nc.ConnectedServerId()
			if rtt, err := nc.RTT(); err == nil {
				nats["rtt_ms"] = rtt.Milliseconds()
			}
		} else {
			nats["connected"] = false
		}
	} else {
		nats["connected"] = false
		nats["note"] = "event_bus_not_configured"
	}

	resp := diagConnectionsResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		WebSocket: ws,
		NATS:      nats,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// collectAdapterHealth summarises the health of each adapter registry
// entry. Adapters that fail their probe are included with status
// "down" so dashboards can render them as red.
func (s *Server) collectAdapterHealth(ctx context.Context) map[string]any {
	out := map[string]any{}

	// Adapters are wired in via s.adapters when present. We accept a
	// generic shape so this handler doesn't need to import every
	// adapter package.
	type adapterProbe interface {
		HealthCheck(ctx context.Context) error
	}
	if probes, ok := s.adapters.(map[string]adapterProbe); ok {
		for name, probe := range probes {
			entry := map[string]any{}
			if err := probe.HealthCheck(ctx); err != nil {
				entry["status"] = "down"
				entry["error"] = err.Error()
			} else {
				entry["status"] = "ok"
			}
			out[name] = entry
		}
		return out
	}

	// Fallback: report whatever aggregate state is available.
	if s.adapters == nil {
		out["status"] = "not_configured"
	} else {
		out["status"] = "unknown"
	}
	return out
}
