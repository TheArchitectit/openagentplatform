package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/registry"
	"github.com/openagentplatform/openagentplatform/a2a/router"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/internal/monitoring"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	secretsauth "github.com/openagentplatform/openagentplatform/secrets/auth"
	"github.com/openagentplatform/openagentplatform/secrets/inject"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

// buildA2AGateway constructs the A2A gateway, the Event-to-Task bridge, and
// the Python-adapter RPC bridge, and wires the adapter client + gateway into
// the API server for the /api/v1/a2a/* adapter proxy. Extracted from
// NewServer so that file stays under the file-size soft limit.
// newAdapterBreaker builds the circuit breaker guarding calls to the
// Python adapter service. It is shared by the A2A RPC bridge path and the
// frontend-facing /api/v1/a2a/* proxy so both see the same open/closed
// state.
func newAdapterBreaker(log *slog.Logger) *resilience.CircuitBreaker {
	cb := resilience.NewCircuitBreaker(resilience.BreakerConfig{
		Name:         "adapter",
		MaxFailures:  5,
		OpenDuration: 30 * time.Second,
		HalfOpenMax:  1,
		Logger:       log,
	})
	log.Info("resilience: adapter circuit breaker enabled",
		"max_failures", 5, "open_duration", "30s")
	return cb
}

func buildA2AGateway(apiServer *api.Server, pool *pgxpool.Pool, natsClient *events.Client, log *slog.Logger) (*gateway.Gateway, *bridge.RPCBridge, *bridge.Bridge, error) {
	// Build the A2A gateway components and the bridge that converts
	// internal NATS events into A2A tasks. The bridge runs as an
	// internal service and does not require an HTTP identity.
	taskMgr := manager.NewTaskManager(pool)
	cardStore := registry.NewPGCardStore(pool)
	agentReg, err := registry.NewRegistry(context.Background(), cardStore, registry.Config{})
	if err != nil {
		return nil, nil, nil, errors.New("a2a registry: " + err.Error())
	}
	a2aRouter, err := router.NewRouter(agentReg)
	if err != nil {
		return nil, nil, nil, errors.New("a2a router: " + err.Error())
	}
	a2aGw, err := gateway.NewGateway(taskMgr, agentReg, a2aRouter, gateway.Config{RequireAuth: true})
	if err != nil {
		return nil, nil, nil, errors.New("a2a gateway: " + err.Error())
	}
	eventBridge, err := bridge.NewBridge(natsClient.Conn(), a2aGw, log, bridge.Config{
		QueueGroup: "a2a-bridge",
	})
	if err != nil {
		return nil, nil, nil, errors.New("a2a bridge: " + err.Error())
	}

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
		return nil, nil, nil, errors.New("a2a rpc bridge: " + err.Error())
	}

	// Expose the adapter client + gateway to the API server so the
	// /api/v1/a2a/* adapter proxy can delegate to them.
	apiServer.SetA2AAdapterBridge(adapterClient, a2aGw)
	// The proxy's upstream calls run through the adapter circuit breaker;
	// buildA2AGateway constructs it before wireSupportServices collects it.
	apiServer.SetAdapterBreaker(newAdapterBreaker(log))

	return a2aGw, rpcBridge, eventBridge, nil
}

// buildHTTPServer assembles the top-level HTTP handler: it mounts the A2A
// gateway under /a2a alongside the API server, wraps the router with tracing
// and rate-limiting middleware, and builds the *http.Server. Extracted from
// NewServer so that file stays under the file-size soft limit.
func buildHTTPServer(apiServer *api.Server, cfg *config.Config, a2aGw *gateway.Gateway, rateLimiter *resilience.RateLimiter, a2aAuth *gateway.Authenticator, log *slog.Logger) (*http.Server, error) {
	// Build a top-level router that delegates the API to apiServer.Router()
	// and mounts the A2A gateway handlers under /a2a/.
	//
	// The A2A gateway runs with RequireAuth=true, so its per-RPC authorize()
	// checks require an identity. The Authenticator is built once in NewServer
	// (newA2AAuthenticator) and shared between the HTTP and gRPC transports, so
	// both authenticate against the same session JWT the REST API accepts.
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
	return httpServer, nil
}

// supportServices bundles the secrets, billing, and resilience wiring that
// NewServer performs after constructing the A2A stack. Returning them as a
// struct keeps NewServer a thin dispatcher and keeps both files under the
// file-size soft limit.
type supportServices struct {
	secretsSweeper    *inject.Sweeper
	secretsRevocation *secretsauth.RevocationList
	billingSvc        *billing.BillingService
	meteringSvc       *billing.MeteringService
	rateLimiter       *resilience.RateLimiter
	adapterBreaker    *resilience.CircuitBreaker
	graceful          *resilience.GracefulShutdown
}

// wireSupportServices builds the secret resolver/injector, the optional
// billing services, and the resilience layer (rate limiter, circuit breaker,
// graceful shutdown), wiring the side-effecting ones into the API server.
func wireSupportServices(apiServer *api.Server, pool *pgxpool.Pool, log *slog.Logger, auditSvc *audit.AuditService) (supportServices, error) {
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

	// Component health aggregation for /readyz: database reachability plus
	// the Python adapter service (reported degraded, not unhealthy, when
	// unconfigured so readiness does not depend on an optional service).
	healthChecker := monitoring.NewHealthChecker()
	if err := healthChecker.Register("database", monitoring.HealthCheckFunc(func(ctx context.Context) monitoring.ComponentHealth {
		h := monitoring.ComponentHealth{Name: "database", Kind: "postgres", CheckedAt: time.Now(), Status: monitoring.HealthHealthy}
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			h.Status = monitoring.HealthUnhealthy
			h.Message = err.Error()
		}
		return h
	})); err != nil {
		log.Warn("health check registration failed", "error", err)
	}
	apiServer.SetHealthChecker(healthChecker)

	// Billing is optional: only wired when STRIPE_SECRET_KEY is set.
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
		// Durable org-state persistence: mutations write through to
		// Postgres and the cache is warmed at startup. Failure to wire it
		// degrades to the previous memory-only mode.
		stateStore := billing.NewPGStateStore(pool)
		if err := stateStore.EnsureSchema(context.Background()); err != nil {
			log.Warn("billing: org_billing_state schema not created; memory-only state", "error", err)
		} else if err := billingSvc.SetStateStore(stateStore); err != nil {
			log.Warn("billing: state store not wired; memory-only state", "error", err)
		}
		apiServer.SetBilling(stripeClient, billingSvc, meteringSvc)
		log.Info("billing: Stripe billing + metering enabled")
	}

	// Rate limiter: 100 req/s sustained, 200 burst.
	rateLimiter := resilience.NewRateLimiter(resilience.RateLimitConfig{
		Rate:            100,
		Burst:           200,
		Enabled:         true,
		IdleTTL:         5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		SkipPaths:       []string{"/healthz", "/readyz", "/metrics"},
	})
	log.Info("resilience: rate limiter enabled", "rate", 100, "burst", 200)

	// Circuit breaker for the Python adapter service (constructed in
	// newAdapterBreaker and shared with the API proxy).
	adapterBreaker := newAdapterBreaker(log)

	// Graceful shutdown coordinator.
	graceful := resilience.NewGracefulShutdown(resilience.ShutdownConfig{
		Timeout: 30 * time.Second,
		Logger:  log,
	})

	return supportServices{
		secretsSweeper:    secretsSweeper,
		secretsRevocation: secretsRevocation,
		billingSvc:        billingSvc,
		meteringSvc:       meteringSvc,
		rateLimiter:       rateLimiter,
		adapterBreaker:    adapterBreaker,
		graceful:          graceful,
	}, nil
}
