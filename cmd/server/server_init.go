package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"google.golang.org/grpc"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/reports"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	secretsauth "github.com/openagentplatform/openagentplatform/secrets/auth"
	"github.com/openagentplatform/openagentplatform/secrets/inject"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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
	eventBridge    *bridge.Bridge
	rpcBridge      *bridge.RPCBridge
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

// NewServer wires all dependencies (DB pool, NATS, API server, background
// handlers) for the application server. It does not start any goroutines;
// call Start to begin serving.
func NewServer(cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, natsClient *events.Client) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("server: nil config")
	}
	if log == nil {
		return nil, errors.New("server: nil logger")
	}
	if pool == nil {
		return nil, errors.New("server: nil pool")
	}
	if natsClient == nil {
		return nil, errors.New("server: nil nats client")
	}

	// --- Tracing ------------------------------------------------------
	// Initialise the global TracerProvider.  If OTEL_EXPORTER_OTLP_ENDPOINT
	// is not set, telemetry.InitTracer installs a no-op provider and returns
	// a SDK provider so Shutdown is always safe to call.
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	tp, err := telemetry.InitTracer(context.Background(), "openagentplatform", otlpEndpoint)
	if err != nil {
		log.Warn("tracing init failed, continuing without tracing", "error", err)
	}
	if tp != nil {
		pool = telemetry.TraceDB(pool)
	}

	// --- Metrics ------------------------------------------------------
	// Initialise the Prometheus exporter.  InitMeter returns a handler
	// that the API layer serves at /metrics.  If initialisation fails
	// (e.g. registry conflict in tests) we log a warning and continue –
	// the /metrics endpoint will return 503 until the handler is set.
	promHandler, mErr := telemetry.InitMeter(context.Background(), "openagentplatform")
	if mErr != nil {
		log.Warn("metrics init failed, /metrics will return 503", "error", mErr)
	} else if promHandler != nil {
		api.SetPrometheusHandler(promHandler)
	}

	auditSvc := newAuditService(pool)
	apiServer := api.NewServer(cfg, log, pool, natsClient, auditSvc)

	// Tier resolution from the platform license file (OAP_LICENSE_FILE +
	// OAP_LICENSE_PUBLIC_KEY). Falls back to Community for all orgs when
	// no valid license is present.
	apiServer.SetTierResolver(newTierResolver(log))

	agentStore := newAgentStoreAdapter(pool)
	heartbeat := events.NewHeartbeatHandler(natsClient, agentStore, log)
	dispatcher := events.NewCheckDispatcher(natsClient, agentStore, nil, log)
	ingestor := checks.NewResultIngestor(checks.ResultIngestorConfig{
		Client:    natsClient,
		Store:     agentStore,
		Checks:    agentStore,
		Evaluator: checks.NewThresholdEvaluator(checks.ThresholdConfig{}),
		Logger:    log,
	})

	// --- Notifications ----------------------------------------------------
	// Registry of channel notifiers (email, slack, webhook). Shared by the
	// alert engine (real alert dispatch) and the API server (channel
	// validation + /test). Without it dispatchNotifications is a no-op.
	notifierReg := notify.InitDefaultRegistry()
	apiServer.SetNotifierRegistry(notifierReg)

	// --- Alert engine -----------------------------------------------------
	alertStore := alerts.NewPGStore(pool)
	alertEngine := alerts.New(alerts.Config{
		Client:           natsClient,
		Store:            alertStore,
		Publisher:        natsClient,
		Logger:           log,
		NotifierRegistry: notifierReg,
	})
	apiServer.SetAlertStore(alertStore)
	apiServer.SetAlertEngine(alertEngine)

	// --- Policy engine ---------------------------------------------------
	policyStore := policy.NewPGStore(pool)
	opaEngine := policy.NewOPAEngine(policy.OPACfg{Logger: log})
	policyResolver := newPolicyResolver(pool, agentStore)
	policyEngine := policy.NewEngine(policy.Config{
		Store:      policyStore,
		OPA:        opaEngine,
		Publisher:  natsClient,
		Client:     natsClient,
		Resolver:   policyResolver,
		Logger:     log,
		Interval:   cfg.PolicyEvalInterval,
		QueueGroup: "oap-policy-engine",
	})
	apiServer.SetPolicyStore(policyStore)
	apiServer.SetPolicyEngine(policyEngine)

	// --- Patch deployer and scheduler -----------------------------------
	patchStore := patches.NewPGStore(pool)
	patchDeployer := patches.NewPatchDeployer(patches.PatchDeployerConfig{
		SuccessThreshold:   0.95,
		MaxRetries:         3,
		StageWaitDuration:  15 * time.Minute,
		InstallTimeout:     10 * time.Minute,
		HealthCheckTimeout: 60 * time.Second,
		RebootStagger:      30 * time.Second,
		CanaryCount:        1,
		IsAgentOnlineFn: func(_ context.Context, agentID string) bool {
			ag, err := agentStore.GetAgent(context.Background(), "", agentID)
			if err != nil || ag == nil {
				return false
			}
			return ag.Status == "online"
		},
		Logger: log,
	}, natsClient.Conn())

	patchScheduler := patches.NewPatchScheduler(patches.PatchSchedulerConfig{
		MaxConcurrency: 10,
		Logger:         log,
	}, patchDeployer, patchStore)

	// --- Script library ---------------------------------------------------
	scriptStore := api.NewPGScriptStore(pool)
	apiServer.SetScriptStore(scriptStore)

	// --- Enterprise reporting ---------------------------------------------
	// Store + engine + scheduler. The schema is created idempotently at
	// startup; if the database is unreachable or the DDL fails we log and
	// continue so /reports endpoints return 503 rather than crashing.
	reportScheduler := wireReports(apiServer, pool, log)

	// --- Remote shell ------------------------------------------------------
	// Session manager + WS handler + credential store + recording store.
	wireShell(apiServer, pool, natsClient.Conn(), log)

	// --- A2A gateway + RPC bridge ----------------------------------------
	a2aGw, rpcBridge, eventBridge, err := buildA2AGateway(apiServer, pool, natsClient, log)
	if err != nil {
		return nil, err
	}

	// --- Secrets, billing, and resilience wiring ------------------------
	svc, err := wireSupportServices(apiServer, pool, log, auditSvc)
	if err != nil {
		return nil, err
	}

	// --- HTTP server with A2A routes mounted ---------------------------
	a2aAuth := newA2AAuthenticator(apiServer)
	httpServer, err := buildHTTPServer(apiServer, cfg, a2aGw, svc.rateLimiter, a2aAuth, log)
	if err != nil {
		return nil, err
	}

	// --- gRPC server for the A2A transport ------------------------------
	// The gRPC transport is optional: if the port is already in use we log
	// and continue with REST+JSON-RPC only.
	var grpcServer *grpc.Server
	var grpcListener net.Listener
	if gs, lis, gErr := buildGRPCServer(a2aGw, a2aAuth, cfg.GRPCPort, log); gErr != nil {
		log.Warn("grpc server not started", "port", cfg.GRPCPort, "error", gErr)
	} else {
		grpcServer, grpcListener = gs, lis
	}

	// --- Tenancy retention purger ---------------------------------------
	// Background worker that soft-deletes and then hard-deletes old
	// audit_events and check_results rows on a daily cadence.
	retentionPurger := tenancy.NewRetentionPurger(tenancy.RetentionPurgerConfig{
		Pool:   pool,
		Logger: log,
		Tables: []string{"audit_events", "check_results"},
	})

	// --- Tenant isolation migrations (opt-in) ----------------------------
	// Applies org_id indexes and row-level-security policies. Opt-in via
	// OAP_ENABLE_TENANT_MIGRATIONS=1 because it mutates shared schema at
	// startup; the migrator uses database/sql over a stdlib bridge to the
	// same Postgres DSN as the pool.
	if os.Getenv("OAP_ENABLE_TENANT_MIGRATIONS") == "1" {
		sqldb, err := sql.Open("pgx", cfg.PostgresDSN)
		if err != nil {
			log.Warn("tenant migrations: cannot open db", "error", err)
		} else {
			migrator := tenancy.NewTenantMigrator(sqldb)
			mCtx, mCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer mCancel()
			if err := migrator.Migrate(mCtx); err != nil {
				log.Warn("tenant migrations failed", "error", err)
			} else {
				log.Info("tenant migrations applied")
			}
			// The migrator is one-shot at startup; close its handle.
			_ = sqldb.Close()
		}
	}

	return &Server{
		cfg:               cfg,
		log:               log,
		httpServer:        httpServer,
		apiServer:         apiServer,
		natsClient:        natsClient,
		pool:              pool,
		tracerProvider:    tp,
		heartbeat:         heartbeat,
		dispatcher:        dispatcher,
		ingestor:          ingestor,
		alertEngine:       alertEngine,
		policyEngine:      policyEngine,
		patchScheduler:    patchScheduler,
		reportScheduler:   reportScheduler,
		eventBridge:       eventBridge,
		rpcBridge:         rpcBridge,
		secretsSweeper:    svc.secretsSweeper,
		secretsRevocation: svc.secretsRevocation,
		retentionPurger:   retentionPurger,
		billingSvc:        svc.billingSvc,
		meteringSvc:       svc.meteringSvc,
		rateLimiter:       svc.rateLimiter,
		adapterBreaker:    svc.adapterBreaker,
		graceful:          svc.graceful,
		grpcServer:        grpcServer,
		grpcListener:      grpcListener,
	}, nil
}

// Start launches the HTTP server and all background event handlers in
// goroutines. It returns once they are all started; call Shutdown to
// stop them gracefully.
