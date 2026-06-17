package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

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
	"github.com/openagentplatform/openagentplatform/pkg/models"
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
	heartbeat       *events.HeartbeatHandler
	dispatcher      *events.CheckDispatcher
	ingestor        *checks.ResultIngestor
	alertEngine     *alerts.AlertEngine
	policyEngine    *policy.PolicyEngine
	patchScheduler  *patches.PatchScheduler
	eventBridge     *bridge.Bridge
	rpcBridge       *bridge.RPCBridge
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
			ag, err := agentStore.GetAgent(context.Background(), agentID)
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

	// --- HTTP server with A2A routes mounted ---------------------------
	// Build a top-level router that delegates the API to apiServer.Router()
	// and mounts the A2A gateway handlers under /a2a/.
	rootHandler := newA2ARouter(apiServer.Router(), a2aGw)

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &Server{
		cfg:            cfg,
		log:            log,
		httpServer:     httpServer,
		apiServer:      apiServer,
		natsClient:     natsClient,
		pool:           pool,
		heartbeat:      heartbeat,
		dispatcher:     dispatcher,
		ingestor:       ingestor,
		alertEngine:    alertEngine,
		policyEngine:   policyEngine,
		patchScheduler: patchScheduler,
		eventBridge:    eventBridge,
		rpcBridge:      rpcBridge,
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
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop background handlers first so they don't try to write to a
	// closed pool.
	s.heartbeat.Stop()
	s.dispatcher.Stop()
	s.ingestor.Stop()
	s.alertEngine.Stop()
	s.policyEngine.Stop()
	s.eventBridge.Stop()
	s.rpcBridge.Stop()
	s.patchScheduler.Close()

	return s.httpServer.Shutdown(ctx)
}

// newAuditService is a thin constructor for the audit service. Kept here
// so NewServer stays self-contained.
func newAuditService(pool *pgxpool.Pool) *audit.AuditService {
	return audit.New(pool)
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

func (a *eventStoreAdapter) GetAgent(ctx context.Context, id string) (*models.Agent, error) {
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