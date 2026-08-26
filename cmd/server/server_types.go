package main

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/reports"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/scheduled"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	secretsauth "github.com/openagentplatform/openagentplatform/secrets/auth"
	"github.com/openagentplatform/openagentplatform/secrets/inject"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
)

// Server bundles the HTTP server and all background event handlers so
// main.go can stay a thin entry point that only wires config and
// signals.
type Server struct {
	cfg            *config.Config
	log            *slog.Logger
	httpServer     *http.Server
	apiServer      *api.Server
	natsClient     *events.Client
	pool           *pgxpool.Pool
	tracerProvider *sdktrace.TracerProvider
	heartbeat      *events.HeartbeatHandler
	dispatcher     *events.CheckDispatcher
	ingestor       *checks.ResultIngestor
	alertEngine    *alerts.AlertEngine
	policyEngine   *policy.PolicyEngine
	patchScheduler *patches.PatchScheduler
	// reportScheduler runs scheduled report generation on a 30s tick.
	// nil when the report schema could not be created (logged, non-fatal).
	reportScheduler *reports.Scheduler
	// scheduledScheduler runs scheduled automated tasks (RMM-06) on a 30s
	// tick. nil when the scheduled schema could not be created (logged,
	// non-fatal).
	scheduledScheduler *scheduled.Scheduler
	eventBridge     *bridge.Bridge
	rpcBridge       *bridge.RPCBridge
	// grpcServer serves the A2A gRPC transport on cfg.GRPCPort. nil when
	// the gRPC transport fails to bind (logged, non-fatal).
	grpcServer   *grpc.Server
	grpcListener net.Listener
	// secretsSweeper cleans up expired credential injections. nil when
	// no resolver/injector was configured.
	secretsSweeper *inject.Sweeper
	// retentionPurger runs the daily two-phase (soft + hard) deletion
	// of audit_events and check_results rows whose age exceeds the
	// per-tenant retention policy.
	retentionPurger *tenancy.RetentionPurger
	// secretsRevocation holds the JWT revocation list for A2A tokens.
	secretsRevocation *secretsauth.RevocationList
	// billingSvc / meteringSvc are nil when STRIPE_SECRET_KEY is unset
	// (billing disabled). When set, their loops are started in Run() and
	// the metering queue is flushed in Shutdown().
	billingSvc  *billing.BillingService
	meteringSvc *billing.MeteringService

	// --- Resilience layer ----------------------------------------------
	// rateLimiter throttles per-IP and per-user request rates.
	rateLimiter *resilience.RateLimiter
	// adapterBreaker protects downstream adapter calls with a circuit
	// breaker.  Failures in the adapter service trip the breaker and
	// short-circuit subsequent calls until it recovers.
	adapterBreaker *resilience.CircuitBreaker
	// graceful orchestrates an ordered, timeout-bounded teardown.
	graceful *resilience.GracefulShutdown
}
