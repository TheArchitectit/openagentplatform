package api

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/bootstrap"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/license"
	"github.com/openagentplatform/openagentplatform/internal/licensing"
	"github.com/openagentplatform/openagentplatform/internal/mesh"
	"github.com/openagentplatform/openagentplatform/internal/monitoring"
	"github.com/openagentplatform/openagentplatform/internal/notify"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/policy"
	"github.com/openagentplatform/openagentplatform/internal/remote"
	"github.com/openagentplatform/openagentplatform/internal/reports"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/scheduled"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

type Server struct {
	cfg           *config.Config
	log           *slog.Logger
	router        chi.Router
	oidcVerifier  *auth.Verifier
	sessionMinter *auth.SessionMinter
	db            *pgxpool.Pool
	audit         *audit.AuditService
	startedAt     time.Time
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
	// patchDeployer orchestrates patch delivery and reboot
	// coordination. May be nil; the reboot endpoint returns 503 when
	// unset.
	patchDeployer *patches.PatchDeployer
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
	// secretsResolver resolves OAP secret reference URIs. May be nil;
	// secrets endpoints return 503 when unset.
	secretsResolver *resolver.SecretResolver
	// secretsBackends lists the names of registered secret backends
	// for the /api/v1/secrets/backends endpoint. May be nil.
	secretsBackends []string
	// adapters is an optional registry of adapter health probes used
	// by /api/v1/diagnostics. May be any type that can be type-asserted
	// to map[string]adapterProbe; a nil value is treated as
	// "not_configured".
	adapters any
	// BillingService is the commercial-tier billing façade. May be nil;
	// billing endpoints return 503 when unset.
	BillingService *billing.BillingService
	// MeteringService tracks per-org usage and reports to Stripe meters.
	// May be nil; usage endpoints return 503 when unset.
	MeteringService *billing.MeteringService
	// gater enforces commercial-tier feature gating on routes. May be
	// nil; when nil, tier-gated routes are unavailable (503).
	gater *licensing.Gater
	// StripeClient wraps the Stripe SDK for direct API calls (e.g.
	// webhook signature verification). May be nil; webhook endpoint
	// returns 503 when unset.
	StripeClient *billing.StripeClient
	// reportsStore is the enterprise reporting persistence interface.
	// May be nil; report endpoints return 503 when unset.
	reportsStore reports.Store
	// reportsScheduler triggers scheduled report runs. May be nil;
	// report generation/scheduling endpoints return 503 when unset.
	reportsScheduler *reports.Scheduler
	// scheduledStore is the scheduled automation persistence interface.
	// May be nil; scheduled automation endpoints return 503 when unset.
	scheduledStore scheduled.Store
	// scheduledScheduler triggers scheduled automated tasks. May be nil;
	// scheduled generation endpoints return 503 when unset.
	scheduledScheduler *scheduled.Scheduler
	// reportsDeliverer verifies presigned download tokens for the
	// /reports/runs/{id}/download endpoint. May be nil; download
	// returns 503 when unset.
	reportsDeliverer *reports.DefaultDeliverer
	// a2aClient is the HTTP client to the Python adapter service. When
	// set, the /api/v1/a2a/* adapter proxy routes become available and
	// delegate to the adapter service. May be nil; the proxy returns 503
	// when unset (e.g. adapter service not deployed).
	a2aClient *bridge.AdapterClient
	// a2aGateway is the A2A gateway used for native task CRUD + SSE. May
	// be nil; task routes return 503 when unset.
	a2aGateway *gateway.Gateway
	// hitlManager is the Human-in-the-Loop approval engine (a2a/hitl).
	// May be nil; approval routes return 503 when unset.
	hitlManager *hitl.ApprovalManager
	// hitlStream fans approval lifecycle actions to SSE subscribers for
	// the approval queue UI (spec R6.5). May be nil; the events route
	// returns 503 when unset.
	hitlStream *ApprovalEventStream
	// tierResolver resolves the commercial tier for an org ID from the
	// platform license file. When nil, all orgs default to Community.
	tierResolver func(orgID string) license.Tier
	// adapterBreaker trips open when the Python adapter service keeps
	// failing, failing fast instead of queuing 10s-timeout upstream calls.
	// May be nil; proxy calls then run unbroken.
	adapterBreaker *resilience.CircuitBreaker
	// healthChecker supplies extra component checks for /readyz beyond
	// the built-in database and NATS probes. May be nil.
	healthChecker *monitoring.HealthChecker
	// meshAdmission mints operator tunnel sessions (WireGuard + SSH).
	// May be nil; mesh endpoints return 503 when unset.
	meshAdmission MeshAdmission
	// meshReleaseStore persists Ed25519-signed agent release records for
	// self-update. May be nil; release endpoints return 503 when unset.
	meshReleaseStore MeshReleaseStore
	// bootstrapStore persists the one-time first-boot org latch and the
	// subject→org bindings consulted at login (auth-rbac spec §14). May be
	// nil; the bootstrap endpoint returns 503 and login org resolution falls
	// back to ID-token claims only.
	bootstrapStore *bootstrap.Store
}

// MeshReleaseStore is the persistence contract used by the mesh release API.
// Implemented by *mesh.Store (via pgxStore); stubbed in tests.
type MeshReleaseStore interface {
	InsertAgentRelease(ctx context.Context, orgID, version, platform, sha256, signature string, pinned bool) (*mesh.AgentRelease, error)
	ListAgentReleases(ctx context.Context, orgID string, onlyPinned bool) ([]*mesh.AgentRelease, error)
	PinAgentRelease(ctx context.Context, orgID, version string, pinned bool) error
}

// MeshAdmission is the interface the mesh API handlers use to open, close,
// and list tunnel sessions. Implemented by *mesh.Admission; stubbed in tests.
type MeshAdmission interface {
	RequestSession(ctx context.Context, orgID, operatorID, agentID, purpose string) (*mesh.SessionGrant, error)
	CloseSession(ctx context.Context, orgID, sessionID string) error
	ListSessions(ctx context.Context, orgID, operatorID string) ([]*mesh.MeshSession, error)
}

// Publisher is the subset of the events.Client interface used by API handlers.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
	Conn() *nats.Conn
}
