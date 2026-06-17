package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/registry"
	"github.com/openagentplatform/openagentplatform/a2a/router"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/openagentplatform/openagentplatform/secrets"
	secretsauth "github.com/openagentplatform/openagentplatform/secrets/auth"
	"github.com/openagentplatform/openagentplatform/secrets/inject"
	"github.com/openagentplatform/openagentplatform/secrets/infisical"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
	"github.com/openagentplatform/openagentplatform/secrets/vault"
)

// Server bundles the HTTP server and all background event handlers so
// main.go can stay a thin entry point that only wires config and
// signals.
type Server struct {
	cfg             *config.Config
	log             *slog.Logger
	httpServer      *http.Server
	apiServer       *api.Server
	natsClient      *events.Client
	pool            *pgxpool.Pool
	tracerProvider  *sdktrace.TracerProvider
	heartbeat       *events.HeartbeatHandler
	dispatcher      *events.CheckDispatcher
	ingestor        *checks.ResultIngestor
	alertEngine     *alerts.AlertEngine
	policyEngine    *policy.PolicyEngine
	patchScheduler  *patches.PatchScheduler
	eventBridge     *bridge.Bridge
	rpcBridge       *bridge.RPCBridge
	// secretsSweeper cleans up expired credential injections. nil when
	// no resolver/injector was configured.
	secretsSweeper *inject.Sweeper
	// retentionPurger runs the daily two-phase (soft + hard) deletion
	// of audit_events and check_results rows whose age exceeds the
	// per-tenant retention policy.
	retentionPurger *tenancy.RetentionPurger
	// secretsRevocation holds the JWT revocation list for A2A tokens.
	secretsRevocation *secretsauth.RevocationList

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
	a2aGw, err := gateway.NewGateway(taskMgr, agentReg, a2aRouter, gateway.Config{})
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
	rootHandler := newA2ARouter(apiServer.Router(), a2aGw)

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
		rateLimiter:       rateLimiter,
		adapterBreaker:    adapterBreaker,
		graceful:          graceful,
	}, nil
}

// Start launches the HTTP server and all background event handlers in
// goroutines. It returns once they are all started; call Shutdown to
// stop them gracefully.
func (s *Server) Start(ctx context.Context) error {
	// Start event subscriptions after the HTTP server has had a chance to
	// bind so /api/v1/agents/register accepts first contact from agents
	// before any heartbeat traffic starts.
	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()

	if err := s.heartbeat.Start(hbCtx); err != nil {
		return errors.New("heartbeat handler start: " + err.Error())
	}
	if err := s.dispatcher.Start(hbCtx); err != nil {
		return errors.New("check dispatcher start: " + err.Error())
	}
	if err := s.ingestor.Start(hbCtx); err != nil {
		return errors.New("result ingestor start: " + err.Error())
	}
	if err := s.alertEngine.Start(hbCtx); err != nil {
		return errors.New("alert engine start: " + err.Error())
	}
	if err := s.policyEngine.Start(hbCtx); err != nil {
		return errors.New("policy engine start: " + err.Error())
	}
	if err := s.eventBridge.Start(); err != nil {
		return errors.New("a2a event bridge start: " + err.Error())
	}
	if err := s.rpcBridge.Start(); err != nil {
		return errors.New("a2a rpc bridge start: " + err.Error())
	}

	// Start the secrets TTL sweeper so expired credential injections
	// (env vars, temp files, stdin pipes) are cleaned up automatically.
	if s.secretsSweeper != nil {
		s.secretsSweeper.Start(hbCtx)
	}

	// Start the per-tenant retention purger (daily soft/hard delete).
	if s.retentionPurger != nil {
		s.retentionPurger.Start(hbCtx)
	}

	go s.patchScheduler.Run(hbCtx)

	go func() {
		s.log.Info("starting server", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("server error", "err", err)
		}
	}()

	return nil
}

// Shutdown gracefully stops all background handlers and the HTTP server.
// It blocks until the shutdown completes or the context is cancelled.
//
// The shutdown sequence is:
//  1. Stop accepting new HTTP requests and wait for in-flight ones to
//     drain (delegated to the resilience.GracefulShutdown coordinator).
//  2. Stop background handlers in reverse-initialisation order.
//  3. Close the NATS client (drains subscriptions).
//  4. Close the database pool.
//  5. Shut down the OpenTelemetry tracer provider.
func (s *Server) Shutdown(ctx context.Context) error {
	// Register all closers with the graceful shutdown coordinator.
	// They are closed in LIFO order after the HTTP server has drained.
	//
	// 1. Background workers and engines.
	s.graceful.Register("heartbeat", resilience.CloserFunc(func(_ context.Context) error {
		s.heartbeat.Stop()
		return nil
	}))
	s.graceful.Register("dispatcher", resilience.CloserFunc(func(_ context.Context) error {
		s.dispatcher.Stop()
		return nil
	}))
	s.graceful.Register("ingestor", resilience.CloserFunc(func(_ context.Context) error {
		s.ingestor.Stop()
		return nil
	}))
	s.graceful.Register("alert-engine", resilience.CloserFunc(func(_ context.Context) error {
		s.alertEngine.Stop()
		return nil
	}))
	s.graceful.Register("policy-engine", resilience.CloserFunc(func(_ context.Context) error {
		s.policyEngine.Stop()
		return nil
	}))
	s.graceful.Register("event-bridge", resilience.CloserFunc(func(_ context.Context) error {
		s.eventBridge.Stop()
		return nil
	}))
	s.graceful.Register("rpc-bridge", resilience.CloserFunc(func(_ context.Context) error {
		s.rpcBridge.Stop()
		return nil
	}))
	s.graceful.Register("patch-scheduler", resilience.CloserFunc(func(_ context.Context) error {
		s.patchScheduler.Close()
		return nil
	}))

	// 2. Secrets sweeper.
	if s.secretsSweeper != nil {
		s.graceful.Register("secrets-sweeper", resilience.CloserFunc(func(_ context.Context) error {
			s.secretsSweeper.Stop()
			return nil
		}))
	}

	// 2b. Tenancy retention purger.
	if s.retentionPurger != nil {
		s.graceful.Register("retention-purger", resilience.CloserFunc(func(_ context.Context) error {
			s.retentionPurger.Stop()
			return nil
		}))
	}

	// 3. Rate limiter janitor.
	s.graceful.Register("rate-limiter", resilience.CloserFunc(func(_ context.Context) error {
		s.rateLimiter.Stop()
		return nil
	}))

	// 4. NATS client (drains subscriptions internally).
	if s.natsClient != nil {
		s.graceful.Register("nats-client", resilience.CloserFunc(func(_ context.Context) error {
			s.natsClient.Close()
			return nil
		}))
	}

	// 5. Database pool.
	if s.pool != nil {
		s.graceful.Register("db-pool", resilience.CloserFunc(func(_ context.Context) error {
			s.pool.Close()
			return nil
		}))
	}

	// 6. Tracer provider (flushes spans).
	s.graceful.Register("tracer-provider", resilience.CloserFunc(func(_ context.Context) error {
		_ = telemetry.Shutdown(ctx, s.tracerProvider)
		return nil
	}))

	// Execute the full shutdown sequence: HTTP drain, then dependency teardown.
	return s.graceful.ShutdownAll(s.httpServer)
}

// newAuditService is a thin constructor for the audit service. Kept here
// so NewServer stays self-contained.
func newAuditService(pool *pgxpool.Pool) *audit.AuditService {
	return audit.New(pool)
}

// buildSecretBackends inspects environment variables and registers the
// appropriate secret backends. Returns the populated registry and the
// list of backend names that were actually registered.
func buildSecretBackends(log *slog.Logger) (*secrets.BackendRegistry, []string) {
	registry := secrets.NewBackendRegistry()
	var names []string

	// Vault takes precedence when VAULT_ADDR is set.
	if addr := os.Getenv("VAULT_ADDR"); addr != "" {
		token := os.Getenv("VAULT_TOKEN")
		v, err := vault.New(context.Background(), vault.Config{
			Address:    addr,
			AuthMethod: vault.AuthToken,
			Token:      token,
		})
		if err != nil {
			log.Warn("vault backend init failed; skipping", "err", err)
		} else {
			registry.Register("vault", v)
			names = append(names, "vault")
			log.Info("secrets: registered vault backend", "addr", addr)
		}
	}

	// Infisical is registered when INFISICAL_CLIENT_ID is set.
	if clientID := os.Getenv("INFISICAL_CLIENT_ID"); clientID != "" {
		clientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")
		i, err := infisical.New(context.Background(), infisical.Config{
			SiteURL:      getEnvDefault("INFISICAL_SITE_URL", "https://app.infisical.com"),
			ProjectID:    os.Getenv("INFISICAL_PROJECT_ID"),
			Environment:  getEnvDefault("INFISICAL_ENVIRONMENT", "dev"),
			AuthMethod:   infisical.AuthUniversal,
			ClientID:     clientID,
			ClientSecret: clientSecret,
		})
		if err != nil {
			log.Warn("infisical backend init failed; skipping", "err", err)
		} else {
			registry.Register("infisical", i)
			names = append(names, "infisical")
			log.Info("secrets: registered infisical backend")
		}
	}

	// Kubernetes CSI driver when running inside a cluster.
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		ns := getEnvDefault("OAP_K8S_NAMESPACE", "default")
		k := secrets.NewK8sCSIBackend(secrets.K8sCSIConfig{
			Namespace: ns,
			MountPath: getEnvDefault("OAP_K8S_MOUNT_PATH", "/var/secrets/oap"),
		})
		registry.Register("k8s-csi", k)
		names = append(names, "k8s-csi")
		log.Info("secrets: registered k8s-csi backend", "namespace", ns)
	}

	// Default: env-var backend (development / fallback).
	env := secrets.NewEnvBackend("OAP_SECRET_")
	registry.Register("env", env)
	names = append(names, "env")
	log.Info("secrets: registered env backend (default)")

	return registry, names
}

// getEnvDefault returns the value of the environment variable named by
// key, or def if it is empty.
func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// --- Shared adapter types (used by both server.go and main.go) ---

// eventStoreAdapter bridges the events package's narrow HeartbeatStore /
// CheckStore interfaces to the api package's pgAgentStore. We re-use the
// api package's store directly through this thin wrapper to avoid
// duplicating the SQL.
type eventStoreAdapter struct {
	pool *pgxpool.Pool
}

func newAgentStoreAdapter(pool *pgxpool.Pool) *eventStoreAdapter {
	return &eventStoreAdapter{pool: pool}
}

// The methods below intentionally duplicate a small subset of the SQL
// expressed in internal/api/agent_store.go so the events package can stay
// dependency-free. Keep them in sync.
func (a *eventStoreAdapter) UpdateAgentHeartbeat(ctx context.Context, agentID string, status string, lastSeen any, cpu, mem, disk float64) error {
	if a.pool == nil {
		return nil
	}
	const q = `
		UPDATE agents
		SET status = $2,
		    last_seen = $3,
		    last_cpu_percent = $4,
		    last_mem_percent = $5,
		    last_disk_percent = $6,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := a.pool.Exec(ctx, q, agentID, status, lastSeen, cpu, mem, disk)
	return err
}

func (a *eventStoreAdapter) GetAgent(ctx context.Context, _, id string) (*models.Agent, error) {
	if a.pool == nil {
		return nil, nil
	}
	const q = `SELECT id, COALESCE(status, 'offline') FROM agents WHERE id = $1 LIMIT 1`
	ag := &models.Agent{}
	err := a.pool.QueryRow(ctx, q, id).Scan(&ag.ID, &ag.Status)
	return ag, err
}

func (a *eventStoreAdapter) MarkStaleAgentsOffline(ctx context.Context, threshold any) ([]string, error) {
	if a.pool == nil {
		return nil, nil
	}
	const q = `
		UPDATE agents
		SET status = 'offline', updated_at = NOW()
		WHERE status = 'online' AND last_seen < $1
		RETURNING id
	`
	rows, err := a.pool.Query(ctx, q, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (a *eventStoreAdapter) InsertCheckResult(ctx context.Context, r *models.CheckResult) error {
	if a.pool == nil {
		return nil
	}
	const q = `
		INSERT INTO check_results (agent_id, check_id, timestamp, status, value, message, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := a.pool.Exec(ctx, q, r.AgentID, r.CheckID, r.Timestamp, r.Status, r.Value, r.Message, r.Metadata)
	return err
}

// ListRecentResults returns the most recent N check results for the
// given (agent_id, check_id) pair, ordered from oldest to newest. It
// satisfies the checks.ResultStore interface used by the threshold
// evaluator.
func (a *eventStoreAdapter) ListRecentResults(ctx context.Context, agentID, checkID string, limit int) ([]models.CheckResult, error) {
	if a.pool == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	const q = `
		SELECT agent_id, check_id, COALESCE(timestamp, 'epoch'::timestamptz),
		       COALESCE(status,''), COALESCE(value, 0), COALESCE(message,''), metadata
		FROM check_results
		WHERE agent_id = $1 AND check_id = $2
		ORDER BY timestamp DESC
		LIMIT $3
	`
	rows, err := a.pool.Query(ctx, q, agentID, checkID, limit)
	if err != nil {
		return []models.CheckResult{}, nil
	}
	defer rows.Close()
	out := make([]models.CheckResult, 0, limit)
	for rows.Next() {
		var r models.CheckResult
		if err := rows.Scan(
			&r.AgentID, &r.CheckID, &r.Timestamp, &r.Status, &r.Value, &r.Message, &r.Metadata,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	// Reverse so callers see oldest -> newest.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// GetCheck fetches a check definition by id. It satisfies the
// checks.CheckDefinitionLookup interface used by the threshold
// evaluator to compute the flap-detection window.
func (a *eventStoreAdapter) GetCheck(ctx context.Context, id string) (*models.CheckDefinition, error) {
	if a.pool == nil {
		return nil, nil
	}
	const q = `
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       check_type, COALESCE(interval_seconds, 60),
		       COALESCE(timeout_seconds, 30), COALESCE(enabled, true)
		FROM check_definitions
		WHERE id = $1
		LIMIT 1
	`
	c := &models.CheckDefinition{}
	err := a.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.OrgID, &c.Name, &c.Description, &c.CheckType,
		&c.IntervalSeconds, &c.TimeoutSeconds, &c.Enabled,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// policyResolver is a thin adapter that backs the oap.* OPA builtins
// from PostgreSQL. Each method looks up a small piece of agent state
// and returns it; errors are returned (not swallowed) so policies can
// use Rego's default rules to handle missing data gracefully.
type policyResolver struct {
	pool *pgxpool.Pool
}

func newPolicyResolver(pool *pgxpool.Pool, _ *eventStoreAdapter) *policyResolver {
	return &policyResolver{pool: pool}
}

func (r *policyResolver) AgentStatus(ctx context.Context, agentID string) (string, error) {
	if r.pool == nil {
		return "", nil
	}
	const q = `SELECT COALESCE(status, 'offline') FROM agents WHERE id = $1 LIMIT 1`
	var s string
	if err := r.pool.QueryRow(ctx, q, agentID).Scan(&s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "offline", nil
		}
		return "", err
	}
	return s, nil
}

func (r *policyResolver) AgentHasCheck(ctx context.Context, agentID, checkType string) (bool, error) {
	if r.pool == nil {
		return false, nil
	}
	const q = `
		SELECT 1
		FROM check_assignments ca
		JOIN check_definitions cd ON cd.id = ca.check_id
		WHERE ca.agent_id = $1 AND cd.check_type = $2
		LIMIT 1
	`
	var n int
	err := r.pool.QueryRow(ctx, q, agentID, checkType).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *policyResolver) CheckLastResult(ctx context.Context, agentID, checkID string) (map[string]any, error) {
	if r.pool == nil {
		return nil, nil
	}
	const q = `
		SELECT COALESCE(status, ''), value, COALESCE(message, ''), metadata
		FROM check_results
		WHERE agent_id = $1 AND check_id = $2
		ORDER BY timestamp DESC
		LIMIT 1
	`
	out := map[string]any{
		"agent_id": agentID,
		"check_id": checkID,
		"status":   "",
		"value":    0.0,
		"message":  "",
	}
	var status, message string
	var value float64
	var meta []byte
	err := r.pool.QueryRow(ctx, q, agentID, checkID).Scan(&status, &value, &message, &meta)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, err
	}
	out["status"] = status
	out["value"] = value
	out["message"] = message
	if len(meta) > 0 {
		var d map[string]any
		if json.Unmarshal(meta, &d) == nil {
			out["details"] = d
		}
	}
	return out, nil
}

func (r *policyResolver) AgentPatchLevel(ctx context.Context, agentID string) (string, error) {
	if r.pool == nil {
		return "", nil
	}
	const q = `SELECT COALESCE(metadata->>'patch_level', '') FROM agents WHERE id = $1 LIMIT 1`
	var s string
	if err := r.pool.QueryRow(ctx, q, agentID).Scan(&s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return s, nil
}

func (r *policyResolver) AgentOSVersion(ctx context.Context, agentID string) (string, error) {
	if r.pool == nil {
		return "", nil
	}
	const q = `SELECT COALESCE(os, '') || ' ' || COALESCE(platform, '') FROM agents WHERE id = $1 LIMIT 1`
	var s string
	if err := r.pool.QueryRow(ctx, q, agentID).Scan(&s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return s, nil
}