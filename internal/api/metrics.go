// Package api – metrics.go.
//
// Exposes a Prometheus text-format endpoint at GET /metrics and a
// lightweight JSON summary at GET /api/v1/metrics/summary for the
// dashboard.  The handler is provided by internal/telemetry.
package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

// promHandler is set once during boot and reused for every scrape so the
// underlying registry stays warm.
var (
	promHandler   http.Handler
	promHandlerMu sync.RWMutex
)

// MetricsResponse is the shape returned by /api/v1/metrics/summary.  The
// fields are best-effort roll-ups of the underlying counters and gauges; the
// full series are still available at /metrics.
type MetricsResponse struct {
	GeneratedAt   time.Time          `json:"generated_at"`
	UptimeSeconds float64            `json:"uptime_seconds"`
	Counters      map[string]int64   `json:"counters,omitempty"`
	Gauges        map[string]float64 `json:"gauges,omitempty"`
}

// SetPrometheusHandler stores the http.Handler produced by
// telemetry.InitMeter so the API layer can serve /metrics without
// depending on telemetry's internal state.
func SetPrometheusHandler(h http.Handler) {
	promHandlerMu.Lock()
	defer promHandlerMu.Unlock()
	promHandler = h
}

// handlePrometheusMetrics serves the Prometheus text-format output.  When
// the exporter is not configured (e.g. tests) the endpoint responds with
// 503 Service Unavailable and a plain text body.
func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	promHandlerMu.RLock()
	h := promHandler
	promHandlerMu.RUnlock()

	if h == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, "# prometheus exporter not initialised\n")
		return
	}
	h.ServeHTTP(w, nil)
}

// handleMetricsSummary returns a small JSON document with roll-up values
// for the dashboard.  It reports the time the API came online, uptime, and
// whatever roll-ups the telemetry package can supply.  The full series are
// still available at /metrics.
func (s *Server) handleMetricsSummary(w http.ResponseWriter, _ *http.Request) {
	startMu.RLock()
	startedAt := startTime
	startMu.RUnlock()

	resp := MetricsResponse{
		GeneratedAt:   time.Now().UTC(),
		UptimeSeconds: time.Since(startedAt).Seconds(),
		Counters:      map[string]int64{},
		Gauges:        map[string]float64{},
	}

	if data := telemetry.SnapshotCounters(); data != nil {
		for k, v := range data {
			resp.Counters[k] = v
		}
	}
	if data := telemetry.SnapshotGauges(); data != nil {
		for k, v := range data {
			resp.Gauges[k] = v
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// processStart tracks the moment the API came online for uptime reporting.
var (
	startTime time.Time
	startMu   sync.RWMutex
)

// markProcessStart records the current time as the moment the API came
// online.  It is invoked from Server.Run.
func (s *Server) markProcessStart() {
	startMu.Lock()
	startTime = time.Now().UTC()
	startMu.Unlock()
}

// metricsRouter returns a chi.Router exposing the metrics endpoints.  The
// router is mounted under the public mux (no auth) because Prometheus
// scrapers use their own network-level controls.
func (s *Server) metricsRouter(r chi.Router) {
	r.Get("/metrics", s.handlePrometheusMetrics)
	r.Route("/api/v1/metrics", func(r chi.Router) {
		r.Get("/summary", s.handleMetricsSummary)
	})
}
