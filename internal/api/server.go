package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/license"
	"github.com/openagentplatform/openagentplatform/internal/licensing"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/schema"
)

// NewServer constructs the HTTP server. If OIDC_ISSUER_URL is configured,
// an OIDC verifier is initialised. The session minter is always created.
// db, eventBus, and audit may be nil; when nil, endpoints that require them
// return 503 Service Unavailable.
func NewServer(cfg *config.Config, log *slog.Logger, db *pgxpool.Pool, eventBus Publisher, auditSvc *audit.AuditService) *Server {
	s := &Server{cfg: cfg, log: log, db: db, eventBus: eventBus, audit: auditSvc, startedAt: time.Now()}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.OIDCIssuerURL != "" {
		v, err := auth.NewVerifier(ctx, cfg.OIDCIssuerURL, cfg.OIDCClientID)
		if err != nil {
			log.Error("oidc verifier init failed", "err", err)
		} else {
			s.oidcVerifier = v
		}
	}

	sm, err := auth.NewSessionMinterFromFile(
		cfg.SessionIssuer,
		cfg.SessionAudience,
		time.Hour,
		cfg.SessionKeyPath,
	)
	if err != nil {
		log.Error("session minter init failed", "err", err)
		// Fall back to an ephemeral key so the server can still start
		// (sessions will not survive a restart).
		sm, _ = auth.NewSessionMinter(cfg.SessionIssuer, cfg.SessionAudience, time.Hour, "")
	}
	s.sessionMinter = sm

	s.router = s.buildRouter()
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

// SessionMinter returns the session JWT minter used to mint and parse the
// internal session tokens. It may be nil when session auth is not configured.
// Callers (e.g. the A2A gateway authenticator) use it to validate bearer
// tokens against the same credentials the REST API accepts.
func (s *Server) SessionMinter() *auth.SessionMinter {
	return s.sessionMinter
}

// resolveOrgTier returns the commercial tier for the given org ID.
// It is used by the tenancy middleware to populate the TenantContext
// with quota limits and feature flags. The tier comes from the
// platform-wide license file resolved at startup (SetTierResolver);
// when no resolver is wired every org is Community.
func (s *Server) resolveOrgTier(orgID string) license.Tier {
	if s.tierResolver != nil {
		if t := s.tierResolver(orgID); t != "" {
			return t
		}
	}
	return license.TierCommunity
}

// SetTierResolver wires the license-file-backed tier resolution. The
// resolver receives the org ID for future per-org licensing; today the
// license is platform-wide.
func (s *Server) SetTierResolver(resolve func(orgID string) license.Tier) {
	s.tierResolver = resolve
}

// currentOrgUsage reports the org's API-call count for the current
// month, as tracked by the billing metering service. It backs the
// tenancy QuotaMiddleware; with no metering service wired, usage is 0
// and quotas never trip.
func (s *Server) currentOrgUsage(orgID string) int64 {
	if s.MeteringService == nil {
		return 0
	}
	return s.MeteringService.GetUsage(orgID).Counts[billing.MetricAPICallCount]
}

// SetAlertStore wires the alert persistence interface into the server.
// Called from main after the pool is ready. May be nil.
func (s *Server) SetAlertStore(store alerts.Store) {
	s.alertStore = store
}

// SetAlertEngine wires the alert state-machine engine into the server.
// Called from main after the engine is constructed. May be nil.
func (s *Server) SetAlertEngine(engine *alerts.AlertEngine) {
	s.alertEngine = engine
}

// SetGater wires the commercial-tier feature gater into the server.
// Called from main after the license validator and loader are ready.
// May be nil; tier-gated routes return 503 when unset.
func (s *Server) SetGater(g *licensing.Gater) {
	s.gater = g
}

// SetNotifierRegistry wires the notifier registry used to validate
// channel configurations and dispatch test notifications. Called from
// main after the registry is initialised. May be nil; the server
// falls back to a default registry on demand.
func (s *Server) SetNotifierRegistry(reg *notify.NotifierRegistry) {
	s.notifierReg = reg
}

// SetPreferenceStore wires the alert-preferences persistence layer
// into the server. Called from main after the store is initialised.
// May be nil; preference endpoints return 503 when unset.
func (s *Server) SetPreferenceStore(store alerts.PreferenceStore) {
	s.prefStore = store
}

// SetRoutingLinker wires the alert_rule_channels junction interface
// used by the rule-channel API endpoints. Called from main.
func (s *Server) SetRoutingLinker(linker alerts.AlertRuleChannelLinker) {
	s.routingLinker = linker
}

// SetPolicyStore wires the policy persistence interface into the
// server. Called from main. May be nil; policy endpoints return 503
// when unset.
func (s *Server) SetPolicyStore(store policy.Store) {
	s.policyStore = store
}

// SetPolicyEngine wires the policy evaluation engine into the server.
// Called from main. May be nil; evaluation endpoints return 503 when
// unset.
func (s *Server) SetPolicyEngine(engine *policy.PolicyEngine) {
	s.policyEngine = engine
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	// Security response headers (nosniff, frame-ancestors, HSTS) applied
	// first so they cover every response including errors and health checks.
	r.Use(securityHeadersMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/healthz"))
	// Cap request body size to bound memory use on mutating endpoints.
	r.Use(bodyLimitMiddleware)
	// Rate limiting is applied once, at the outer HTTP server layer
	// (buildHTTPServer) which owns the limiter lifecycle (Stop on shutdown)
	// and skip-paths for health/metrics. The inner per-Server limiter was
	// removed: with identical 100/200 defaults both would fire and halve
	// effective capacity for bursty clients.
	// Metrics middleware records request count and latency for every
	// request.  It is installed before audit so it sees the final status
	// code and response size; the middleware itself skips /metrics to
	// avoid polluting the request rate.
	r.Use(metricsMiddleware)
	// Audit middleware wraps the whole router so it captures every API
	// call regardless of whether the request was authenticated. The
	// middleware itself filters out /health, /docs, and /ws paths.
	r.Use(audit.Middleware(s.audit, s.log))

	s.registerRoutes(r)
	schema.MountSwagger(r)

	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// errSessionUnavailable is returned when the session minter is nil.
var errSessionUnavailable = errors.New("session minter not configured")
