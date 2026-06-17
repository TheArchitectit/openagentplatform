package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/pprof"
	"runtime"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

// healthResponse is the wire shape for /status and /readyz. It is kept
// intentionally small so it can be consumed by ops dashboards without
// a schema dependency.
type healthResponse struct {
	Status     string            `json:"status"`
	Timestamp  string            `json:"timestamp"`
	Components map[string]string `json:"components,omitempty"`
	Details    map[string]any    `json:"details,omitempty"`
}

// handleHealthz is a liveness probe. It intentionally performs no
// dependency checks — if this handler responds the process is alive and
// the HTTP server can serve requests. Kubernetes uses this to decide
// whether to restart the pod.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleReadyz is a readiness probe. It returns 200 only when every
// critical dependency (DB, NATS) is reachable. Load balancers and
// service meshes should route traffic away from instances that return
// non-200 here. Unlike /healthz this is allowed to do real work.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	resp := healthResponse{
		Status:     "ok",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Components: make(map[string]string),
	}
	allOK := true

	// Database readiness — PingContext is lightweight and verifies the
	// pool can hand out a working connection.
	if s.db != nil {
		if err := s.db.Ping(ctx); err != nil {
			resp.Components["database"] = "down: " + err.Error()
			allOK = false
		} else {
			resp.Components["database"] = "ok"
		}
	} else {
		resp.Components["database"] = "not_configured"
	}

	// NATS readiness — check that the underlying connection is in a
	// connected state. We do not perform a round-trip publish here so
	// that readiness does not flap during normal reconnects.
	if s.eventBus != nil {
		if nc := s.eventBus.Conn(); nc != nil && nc.IsConnected() {
			resp.Components["nats"] = "ok"
		} else {
			resp.Components["nats"] = "down"
			allOK = false
		}
	} else {
		resp.Components["nats"] = "not_configured"
	}

	w.Header().Set("Content-Type", "application/json")
	if !allOK {
		resp.Status = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// handleStatus returns a detailed runtime snapshot. It is the endpoint
// operators hit first when triaging an incident. Fields are chosen to
// be cheap to compute (no DB scans) but informative enough to spot
// obvious problems such as goroutine leaks or a stale binary.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	info := telemetry.GetBuildInfo()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	details := map[string]any{
		"uptime_seconds":  int64(time.Since(s.startedAt).Seconds()),
		"build_info":      info,
		"goroutine_count": runtime.NumGoroutine(),
		"memory_usage_mb": float64(m.Alloc) / (1024 * 1024),
		"sys_memory_mb":   float64(m.Sys) / (1024 * 1024),
		"num_gc":          m.NumGC,
		"environment":     s.cfg.Env,
	}

	components := map[string]string{}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			components["database"] = "down"
		} else {
			components["database"] = "ok"
		}
	}
	if s.eventBus != nil {
		if nc := s.eventBus.Conn(); nc != nil && nc.IsConnected() {
			components["nats"] = "ok"
		} else {
			components["nats"] = "down"
		}
	}

	resp := healthResponse{
		Status:     "ok",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Components: components,
		Details:    details,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleVersion serves the build identity at /version. It is separate
// from /status so that lightweight version checks don't need to read
// runtime memory stats.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(telemetry.GetBuildInfo())
}

// redactedConfig returns a copy of the server configuration with secret
// fields blanked. The returned shape mirrors the wire layout used by
// the platform team when reviewing support tickets.
func redactedConfig(cfg any) map[string]any {
	// We intentionally don't import the config package here to keep
	// this handler free of a tight coupling. Instead we walk the
	// exported JSON tags and blank any field whose name suggests a
	// credential. This is intentionally conservative — false
	// positives just leak "REDACTED" instead of a real secret.
	raw, err := json.Marshal(cfg)
	if err != nil {
		return map[string]any{"error": "failed to serialize config"}
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return map[string]any{"error": "failed to parse config"}
	}
	for k := range generic {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "secret") ||
			strings.Contains(lk, "password") ||
			strings.Contains(lk, "dsn") ||
			strings.Contains(lk, "token") ||
			strings.Contains(lk, "key") {
			if _, ok := generic[k].(string); ok {
				generic[k] = "REDACTED"
			}
		}
	}
	return generic
}

// handleDebugConfig returns a redacted configuration dump. It is gated
// to debug-mode builds by the caller. Operators use this to verify
// environment variables were loaded correctly without leaking
// credentials into shell history or ticket attachments.
func (s *Server) handleDebugConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"redacted_config": redactedConfig(s.cfg),
		"environment":     s.cfg.Env,
	})
}

// mountPprofRoutes attaches the standard library pprof handlers under
// /debug/pprof/. It is called only when debug mode is enabled so that
// profile data is never exposed in production. The indexes route is
// overridden so it returns JSON instead of the default HTML page, which
// keeps the surface consistent with the rest of our diagnostic API.
func (s *Server) mountPprofRoutes(r interface {
	Get(pattern string, handler http.HandlerFunc)
}) {
	r.Get("/debug/pprof/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"pprof enabled; visit /debug/pprof/heap, /debug/pprof/goroutine, /debug/pprof/profile"}`))
	})
	r.Get("/debug/pprof/cmdline", pprof.Cmdline)
	r.Get("/debug/pprof/profile", pprof.Profile)
	r.Get("/debug/pprof/symbol", pprof.Symbol)
	r.Get("/debug/pprof/trace", pprof.Trace)
	r.Get("/debug/pprof/{action}", pprof.Index)
}
