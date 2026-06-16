package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/remote"
	"github.com/openagentplatform/openagentplatform/internal/schema"
)

type Server struct {
	cfg           *config.Config
	log           *slog.Logger
	router        chi.Router
	oidcVerifier  *auth.Verifier
	sessionMinter *auth.SessionMinter
	db            *pgxpool.Pool
	audit         *audit.AuditService
	// eventBus is an optional publisher used to emit platform events
	// (e.g. AgentOnline, AgentOffline) from API handlers. May be nil.
	eventBus Publisher
	// alertStore is the alert persistence interface. May be nil.
	alertStore alerts.Store
	// alertEngine drives state-machine transitions. May be nil.
	alertEngine *alerts.AlertEngine
	// notifierReg is the notifier registry used to validate channel
	// configurations and dispatch test notifications. May be nil;
	// a default registry is used lazily when not set.
	notifierReg *notify.NotifierRegistry
	// prefStore is the alert preferences persistence interface. May
	// be nil; preference endpoints return 503 when unset.
	prefStore alerts.PreferenceStore
	// routingLinker is the alert_rule_channels junction interface
	// used by the rule-channel API endpoints. May be nil.
	routingLinker alerts.AlertRuleChannelLinker
	// policyStore is the policy persistence interface. May be nil.
	policyStore policy.Store
	// policyEngine evaluates Rego policies. May be nil.
	policyEngine *policy.PolicyEngine
	// patchStore is the patch job persistence interface. May be nil.
	patchStore patches.Store
	// patchScanner is the patch scan dispatcher that aggregates
	// per-agent scan results into a platform-wide catalog. May be
	// nil; catalog endpoints return 503 when unset.
	patchScanner *patches.PatchScanDispatcher
	// scriptStore is the script definition and run persistence interface.
	// May be nil; script endpoints return 503 when unset.
	scriptStore scriptStore
	// wsHub manages connected WebSocket clients and their
	// subscriptions. Lazily constructed on first upgrade.
	wsHub  *wsHub
	wsOnce sync.Once
	// remote is the remote-shell API handler. May be nil; remote
	// endpoints return 503 when unset.
	remote *RemoteHandler
	// recordingStore is the shell-session recording persistence
	// interface. May be nil; recording endpoints return 503 when
	// unset.
	recordingStore remote.SessionRecordingStore
	// recorderFactory produces a SessionRecorder for a given live
	// session id. Optional; when nil, live sessions are not recorded.
	recorderFactory func(sessionID string) (*remote.SessionRecorder, bool)
}

// Publisher is the subset of the events.Client interface used by API handlers.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// NewServer constructs the HTTP server. If OIDC_ISSUER_URL is configured,
// an OIDC verifier is initialised. The session minter is always created.
// db, eventBus, and audit may be nil; when nil, endpoints that require them
// return 503 Service Unavailable.
func NewServer(cfg *config.Config, log *slog.Logger, db *pgxpool.Pool, eventBus Publisher, auditSvc *audit.AuditService) *Server {
	s := &Server{cfg: cfg, log: log, db: db, eventBus: eventBus, audit: auditSvc}

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

// SetPatchStore wires the patch job persistence interface into the
// server. Called from main. May be nil; patch endpoints return 503
// when unset.
func (s *Server) SetPatchStore(store patches.Store) {
	s.patchStore = store
}

// SetPatchScanner wires the patch scan dispatcher into the server.
// Called from main after the dispatcher is constructed. May be nil;
// catalog endpoints return 503 when unset.
func (s *Server) SetPatchScanner(d *patches.PatchScanDispatcher) {
	s.patchScanner = d
}

// SetScriptStore wires the script definition and run persistence interface
// into the server. Called from main. May be nil; script endpoints return
// 503 when unset.
func (s *Server) SetScriptStore(store scriptStore) {
	s.scriptStore = store
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/healthz"))
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
