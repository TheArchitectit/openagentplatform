// Package telemetry – metrics.go.
//
// Prometheus metrics for the openagentplatform server.  Uses the standard
// prometheus/client_golang library so that all instruments can be scraped
// at /metrics in the well-known text exposition format.
//
// Metric naming follows the Prometheus convention:
//   - Counters end in _total
//   - Histograms end in _seconds
//   - Gauges are domain nouns
//
// The instrument definitions and registration live here; the thin
// nil-safe recorder wrappers live in metrics_record.go, and the
// JSON-summary roll-up snapshot helpers live in metrics_snapshot.go.
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// float64Atomic provides a minimal mutex-protected float64 so we can keep
// gauge roll-ups without pulling in a third-party atomic-float package.
type float64Atomic struct {
	mu sync.RWMutex
	v  float64
}

func (f *float64Atomic) Store(v float64) {
	f.mu.Lock()
	f.v = v
	f.mu.Unlock()
}

func (f *float64Atomic) Load() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.v
}

const metricsNamespace = "oap"

var (
	// Counters.
	APIRequestsTotal      *prometheus.CounterVec
	NATSMessagesTotal     *prometheus.CounterVec
	AgentHeartbeatsTotal  *prometheus.CounterVec
	CheckResultsTotal     *prometheus.CounterVec
	AlertTransitionsTotal *prometheus.CounterVec
	A2ATasksTotal         *prometheus.CounterVec
	DBQueriesTotal        *prometheus.CounterVec
	BytesByAdapterTotal   *prometheus.CounterVec

	// Histograms.
	HTTPRequestDurationSeconds    *prometheus.HistogramVec
	DBQueryDurationSeconds        *prometheus.HistogramVec
	CheckExecutionDurationSeconds *prometheus.HistogramVec
	AdapterInvokeDurationSeconds  *prometheus.HistogramVec

	// Gauges.
	AgentsOnline        *prometheus.GaugeVec
	ActiveAlerts        *prometheus.GaugeVec
	ActiveShellSessions prometheus.Gauge
	AdapterPoolProcs    *prometheus.GaugeVec
	CostTotalByAdapter  *prometheus.GaugeVec

	registry    *prometheus.Registry
	registryMu  sync.Mutex
	initialized bool

	// serviceName stores the service name passed to InitMeter for use as
	// a constant label on all metrics.
	serviceName string
)

// InitMeter registers the OAP Prometheus instruments on a private registry
// and returns an http.Handler that serves the standard text exposition
// format.  The registry is isolated from prometheus.DefaultRegisterer so
// tests can create multiple instances without colliding on global state.
//
// It is safe to call more than once – subsequent calls return the same
// handler.  serviceName is recorded as a constant label.
func InitMeter(_ context.Context, svc string) (http.Handler, error) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if initialized {
		return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}), nil
	}

	reg := prometheus.NewRegistry()

	// Counters ----------------------------------------------------------------
	APIRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "api_requests_total",
			Help:      "Total number of API requests received.",
		},
		[]string{"method", "path", "status"},
	)
	NATSMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "nats_messages_total",
			Help:      "Total NATS messages processed by subject and direction.",
		},
		[]string{"subject", "direction"},
	)
	AgentHeartbeatsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "agent_heartbeats_total",
			Help:      "Total agent heartbeats received.",
		},
		[]string{"agent_id", "status"},
	)
	CheckResultsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "check_results_total",
			Help:      "Total check executions by type and result status.",
		},
		[]string{"check_type", "status"},
	)
	AlertTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "alert_transitions_total",
			Help:      "Total alert state transitions.",
		},
		[]string{"from_state", "to_state"},
	)
	A2ATasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "a2a_tasks_total",
			Help:      "Total A2A tasks processed by adapter and status.",
		},
		[]string{"adapter", "status"},
	)
	DBQueriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "db_queries_total",
			Help:      "Total database queries executed by operation.",
		},
		[]string{"operation"},
	)
	BytesByAdapterTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "bytes_by_adapter_total",
			Help:      "Total bytes transferred by adapter and direction.",
		},
		[]string{"adapter", "direction"},
	)

	// Histograms --------------------------------------------------------------
	HTTPRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
	DBQueryDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "db_query_duration_seconds",
			Help:      "Duration of database queries in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
	CheckExecutionDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "check_execution_duration_seconds",
			Help:      "Duration of check executions in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"check_type"},
	)
	AdapterInvokeDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "adapter_invoke_duration_seconds",
			Help:      "Duration of adapter invocations in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"adapter"},
	)

	// Gauges ------------------------------------------------------------------
	AgentsOnline = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "agents_online",
			Help:      "Number of agents currently online.",
		},
		[]string{"agent_id"},
	)
	ActiveAlerts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "active_alerts",
			Help:      "Number of active alerts by severity.",
		},
		[]string{"severity"},
	)
	ActiveShellSessions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "active_shell_sessions",
			Help:      "Number of active remote shell sessions.",
		},
	)
	AdapterPoolProcs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "adapter_pool_processes",
			Help:      "Number of adapter pool processes by state.",
		},
		[]string{"state"},
	)
	CostTotalByAdapter = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "cost_total_by_adapter",
			Help:      "Total cost incurred by each adapter in USD.",
		},
		[]string{"adapter"},
	)

	collectors := []prometheus.Collector{
		APIRequestsTotal, NATSMessagesTotal, AgentHeartbeatsTotal,
		CheckResultsTotal, AlertTransitionsTotal, A2ATasksTotal,
		DBQueriesTotal, BytesByAdapterTotal,
		HTTPRequestDurationSeconds, DBQueryDurationSeconds,
		CheckExecutionDurationSeconds, AdapterInvokeDurationSeconds,
		AgentsOnline, ActiveAlerts, ActiveShellSessions,
		AdapterPoolProcs, CostTotalByAdapter,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("telemetry: register %s: %w", reflectName(c), err)
		}
	}

	registry = reg
	serviceName = svc
	initialized = true

	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}), nil
}

// reflectName returns the type name of a collector for error messages.
func reflectName(c prometheus.Collector) string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", c)
}
