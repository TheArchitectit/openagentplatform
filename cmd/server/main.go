package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/checklib"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/db"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/logger"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Database ---------------------------------------------------------
	poolCtx, poolCancel := context.WithTimeout(rootCtx, 10*time.Second)
	pool, err := db.NewPool(poolCtx, cfg.PostgresDSN)
	poolCancel()
	if err != nil {
		log.Error("db pool init failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("db pool ready")

	// --- Seed built-in check library -------------------------------------
	// Idempotent: inserts one disabled check_definitions row per built-in
	// template (ping, cpu, memory, disk, service) if no matching row already
	// exists. Safe to run on every boot.
	seedCtx, seedCancel := context.WithTimeout(rootCtx, 15*time.Second)
	if seedRes, err := checklib.Seed(seedCtx, pool, log); err != nil {
		log.Warn("check library seeder failed", "err", err)
	} else {
		log.Info("check library seeded",
			"seeded", len(seedRes.Seeded),
			"skipped", len(seedRes.Skipped),
			"total", seedRes.TotalChecks,
		)
	}
	seedCancel()

	// --- NATS event bus ---------------------------------------------------
	natsClient, err := events.NewClient(cfg.NATSURL, cfg.NATSCertFile, cfg.NATSKeyFile, cfg.NATSCAFile, log)
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer natsClient.Close()
	log.Info("nats client ready", "url", cfg.NATSURL)

	// --- API server -------------------------------------------------------
	auditSvc := audit.New(pool)
	srv := api.NewServer(cfg, log, pool, natsClient, auditSvc)

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- Background event handlers --------------------------------------
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

	// Start event subscriptions after the HTTP server has had a chance to
	// bind so /api/v1/agents/register accepts first contact from agents
	// before any heartbeat traffic starts.
	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()
	if err := heartbeat.Start(hbCtx); err != nil {
		log.Error("heartbeat handler start failed", "err", err)
	}
	if err := dispatcher.Start(hbCtx); err != nil {
		log.Error("check dispatcher start failed", "err", err)
	}
	if err := ingestor.Start(hbCtx); err != nil {
		log.Error("result ingestor start failed", "err", err)
	}

	// --- HTTP server goroutine -------------------------------------------
	go func() {
		log.Info("starting server", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "err", err)
		}
	}()

	// --- Shutdown ---------------------------------------------------------
	<-rootCtx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Stop background handlers first so they don't try to write to a
	// closed pool.
	heartbeat.Stop()
	dispatcher.Stop()
	ingestor.Stop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
}

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
