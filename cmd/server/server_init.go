package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/registry"
	"github.com/openagentplatform/openagentplatform/a2a/router"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	secretsauth "github.com/openagentplatform/openagentplatform/secrets/auth"
	"github.com/openagentplatform/openagentplatform/secrets/inject"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
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
	eventBridge    *bridge.Bridge
	rpcBridge      *bridge.RPCBridge
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

	// --- Alert engine -----------------------------------------------------
	alertStore := alerts.NewPGStore(pool)
	alertEngine := alerts.New(alerts.Config{
		Client:    natsClient,
		Store:     alertStore,
		Publisher: natsClient,
		Logger:    log,
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

	// --- A2A Event-to-Task bridge ---------------------------------------
	// Build the A2A gateway components and the bridge that converts
	// internal NATS events into A2A tasks. The bridge runs as an
	// internal service and does not require an HTTP identity.
	taskMgr := manager.NewTaskManager(pool)
	cardStore := registry.NewPGCardStore(pool)
	agentReg, err := registry.NewRegistry(context.Background(), cardStore, registry.Config{})
	if err != nil {
		return nil, errors.New("a2a registry: " + err.Error())
	}
	a2aRouter, err := router.NewRouter(agentReg)
	if err != nil {
		return nil, errors.New("a2a router: " + err.Error())
	}
	a2aGw, err := gateway.NewGateway(taskMgr, agentReg, a2aRouter, gateway.Config{RequireAuth: true})
	if err != nil {
		return nil, errors.New("a2a gateway: " + err.Error())
	}
	eventBridge, err := bridge.NewBridge(natsClient.Conn(), a2aGw, log, bridge.Config{
		QueueGroup: "a2a-bridge",
	})
	if err != nil {
		return nil, errors.New("a2a bridge: " + err.Error())
	}

	// --- A2A RPC Bridge (Python adapter service) -----------------------
	// Create the HTTP client for the Python adapter service and wire it
	// to the A2A Gateway via the RPCBridge. The RPC bridge handles task
	// dispatch, response streaming, cancellation, and periodic AgentCard
	// refresh.
	adapterClient := bridge.NewAdapterClient(bridge.ClientConfig{
		BaseURL: "http://localhost:8001",
	})
	rpcBridge, err := bridge.NewRPCBridge(adapterClient, a2aGw, bridge.RPCConfig{
		Logger: log,
	})
	if err != nil {
		return nil, errors.New("a2a rpc bridge: " + err.Error())
	}

	// --- Secrets module wiring ----------------------------------------
	// Build a registry of secret backends based on environment variables,
	// create a resolver with an LRU cache, and wire the credential
	// injector + TTL sweeper. The API server is updated so the
	// /api/v1/secrets/* endpoints become available.
	secretRegistry, registeredNames := buildSecretBackends(log)
	secretResolver := resolver.New(secretRegistry, log, auditSvc)

	// Credential injector and TTL sweeper. The sweeper periodically
	// removes expired env-var / file / stdin injections.
	secretsInjector := inject.NewInjector(secretResolver, &resolver.AuthContext{}, log, auditSvc)
	secretsSweeper := inject.NewSweeper(log, auditSvc, secretResolver)
	_ = secretsInjector // retained for future handler-level integration

	// JWT revocation list for A2A auth tokens.
	secretsRevocation := secretsauth.NewRevocationList()

	// Share the resolver and backend list with the API server so the
	// secrets HTTP endpoints can dispatch to it.
	apiServer.SetSecretsResolver(secretResolver, registeredNames)

	// --- Billing & metering -----------------------------------------------
	// Billing is optional: it is only wired when STRIPE_SECRET_KEY is set.
	// Without it the /billing and /usage endpoints return 503 (the handlers
	// nil-check the services). When configured we construct the Stripe
	// client + BillingService/MeteringService façades and attach them to the
	// API server. The flush/sync loops are started in Run() (alongside the
	// other background workers, sharing hbCtx) and the metering queue is
	// flushed during graceful shutdown.
	var (
		stripeClient *billing.StripeClient
		billingSvc   *billing.BillingService
		meteringSvc  *billing.MeteringService
	)
	if sc, err := billing.NewStripeClient(); err != nil {
		log.Info("billing: STRIPE_SECRET_KEY not set; billing endpoints disabled")
	} else {
		stripeClient = sc
		billingSvc = billing.NewBillingService(sc, log)
		meteringSvc = billing.NewMeteringService(sc, log)
		apiServer.SetBilling(stripeClient, billingSvc, meteringSvc)
		log.Info("billing: Stripe billing + metering enabled")
	}

	// --- Resilience layer wiring -----------------------------------------
	// Rate limiter: 100 req/s sustained, 200 burst, with health and
	// metrics endpoints exempted from throttling.
	rateLimiter := resilience.NewRateLimiter(resilience.RateLimitConfig{
		Rate:            100,
		Burst:           200,
		Enabled:         true,
		IdleTTL:         5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		SkipPaths:       []string{"/healthz", "/readyz", "/metrics"},
	})
	log.Info("resilience: rate limiter enabled", "rate", 100, "burst", 200)

	// Circuit breaker for the Python adapter service.  Trips after 5
	// consecutive failures, stays open for 30s, then allows a single
	// half-open probe.
	adapterBreaker := resilience.NewCircuitBreaker(resilience.BreakerConfig{
		Name:         "adapter",
		MaxFailures:  5,
		OpenDuration: 30 * time.Second,
		HalfOpenMax:  1,
		Logger:       log,
	})
	log.Info("resilience: adapter circuit breaker enabled",
		"max_failures", 5, "open_duration", "30s")

	// Graceful shutdown coordinator.  All dependencies are registered
	// here so that Shutdown() can drain them in order.
	graceful := resilience.NewGracefulShutdown(resilience.ShutdownConfig{
		Timeout: 30 * time.Second,
		Logger:  log,
	})

	// --- HTTP server with A2A routes mounted ---------------------------
	// Build a top-level router that delegates the API to apiServer.Router()
	// and mounts the A2A gateway handlers under /a2a/.
	//
	// The A2A gateway runs with RequireAuth=true, so its per-RPC authorize()
	// checks require an identity. We build a gateway Authenticator whose
	// token validator reuses the API server's SessionMinter, so callers
	// authenticate against the same session JWT the REST API accepts.
	a2aAuth := gateway.NewAuthenticator(gateway.Config{RequireAuth: true})
	if sm := apiServer.SessionMinter(); sm != nil {
		a2aAuth.SetTokenValidator(func(token string) (*gateway.Identity, error) {
			claims, err := sm.Parse(token)
			if err != nil || claims == nil {
				return nil, gateway.ErrInvalidCredentials
			}
			md := map[string]string{
				"email": claims.Email,
				"role":  claims.Role,
			}
			if claims.OrgID != "" {
				md["org_id"] = claims.OrgID
			}
			// Map the session role to A2A permission scopes: viewers get
			// a2a:read; admin/technician/operator also get a2a:send + a2a:admin.
			scopes := []string{gateway.PermRead}
			switch claims.Role {
			case auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator:
				scopes = append(scopes, gateway.PermSend, gateway.PermAdmin)
			}
			return &gateway.Identity{
				Subject:  claims.Subject,
				Method:   gateway.AuthBearer,
				Scopes:   scopes,
				Metadata: md,
			}, nil
		})
	}
	rootHandler := newA2ARouter(apiServer.Router(), a2aGw, a2aAuth)

	// Wrap with the OpenTelemetry HTTP middleware so every request gets a
	// server span.  Health-check endpoints are skipped inside the middleware.
	tracedHandler := withTracing(rootHandler)

	// Wrap with the rate-limit middleware (outermost).  This is applied
	// after tracing so 429 responses still receive a span.
	rateLimitedHandler := rateLimiter.Middleware()(tracedHandler)

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           rateLimitedHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- Tenancy retention purger ---------------------------------------
	// Background worker that soft-deletes and then hard-deletes old
	// audit_events and check_results rows on a daily cadence.
	retentionPurger := tenancy.NewRetentionPurger(tenancy.RetentionPurgerConfig{
		Pool:   pool,
		Logger: log,
		Tables: []string{"audit_events", "check_results"},
	})

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
		eventBridge:       eventBridge,
		rpcBridge:         rpcBridge,
		secretsSweeper:    secretsSweeper,
		secretsRevocation: secretsRevocation,
		retentionPurger:   retentionPurger,
		billingSvc:        billingSvc,
		meteringSvc:       meteringSvc,
		rateLimiter:       rateLimiter,
		adapterBreaker:    adapterBreaker,
		graceful:          graceful,
	}, nil
}

// Start launches the HTTP server and all background event handlers in
// goroutines. It returns once they are all started; call Shutdown to
// stop them gracefully.
