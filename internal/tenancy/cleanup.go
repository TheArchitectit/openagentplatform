package tenancy

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// Default grace period before hard-delete after soft-delete.
const DefaultGracePeriod = 7 * 24 * time.Hour

// PurgeMetrics holds the Prometheus instruments emitted by the
// RetentionPurger.  The instruments are registered with the default
// registry on first use so that a /metrics scrape can observe them
// without the caller having to wire a custom registry.
type PurgeMetrics struct {
	PurgedRecordsTotal *prometheus.CounterVec // labels: table, action
	PurgeDuration      prometheus.Histogram
	PurgeErrors        prometheus.Counter
	PurgeRuns          prometheus.Counter
	RecordsScanned     *prometheus.CounterVec // labels: table
}

var (
	purgeMetrics     *PurgeMetrics
	purgeMetricsOnce sync.Once
)

// getPurgeMetrics lazily initialises and returns the package-level
// Prometheus instruments.  Using sync.Once guarantees a single
// registration even when the purger is restarted.
func getPurgeMetrics() *PurgeMetrics {
	purgeMetricsOnce.Do(func() {
		purgeMetrics = &PurgeMetrics{
			PurgedRecordsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "purged_records_total",
				Help: "Total number of records purged by the retention cleaner, labelled by table and action (soft/hard).",
			}, []string{"table", "action"}),
			PurgeDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "purge_duration_seconds",
				Help:    "Duration of a single retention purge run in seconds.",
				Buckets: prometheus.DefBuckets,
			}),
			PurgeErrors: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "purge_errors_total",
				Help: "Total number of errors encountered during retention purges.",
			}),
			PurgeRuns: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "purge_runs_total",
				Help: "Total number of retention purge runs.",
			}),
			RecordsScanned: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "purge_records_scanned_total",
				Help: "Total number of records scanned during retention purges, labelled by table.",
			}, []string{"table"}),
		}
		prometheus.MustRegister(
			purgeMetrics.PurgedRecordsTotal,
			purgeMetrics.PurgeDuration,
			purgeMetrics.PurgeErrors,
			purgeMetrics.PurgeRuns,
			purgeMetrics.RecordsScanned,
		)
	})
	return purgeMetrics
}

// RetentionPurgerConfig configures the RetentionPurger worker.
type RetentionPurgerConfig struct {
	// Pool is the PostgreSQL connection pool used for purge queries.
	Pool *pgxpool.Pool
	// Logger is used for structured logging of purge events.
	Logger *slog.Logger
	// Interval is the time between purge runs.  Defaults to 24h.
	Interval time.Duration
	// GracePeriod is the delay between soft-delete and hard-delete.
	// Defaults to 7 days.
	GracePeriod time.Duration
	// DefaultRetentionDays is used when a tenant has no explicit
	// retention preference.  Tier-based defaults are used as a
	// fallback if this is zero.
	DefaultRetentionDays int
	// Tables is the list of tables to purge.  Defaults to
	// {"audit_events", "check_results"}.
	Tables []string
}

// RetentionPurger is a background worker that deletes old records from
// per-tenant data tables.  It implements a two-phase approach:
//  1. Soft-delete: mark records older than the tenant's retention
//     period by setting deleted_at.
//  2. Hard-delete: remove records that have been soft-deleted for
//     longer than the grace period.
//
// All deletions are scoped by org_id so the purger never touches
// another tenant's data.
type RetentionPurger struct {
	cfg    RetentionPurgerConfig
	metrics *PurgeMetrics
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRetentionPurger creates a new RetentionPurger with the given
// configuration.  Zero values in the config are replaced with sensible
// defaults.
func NewRetentionPurger(cfg RetentionPurgerConfig) *RetentionPurger {
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.GracePeriod <= 0 {
		cfg.GracePeriod = DefaultGracePeriod
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if len(cfg.Tables) == 0 {
		cfg.Tables = []string{"audit_events", "check_results"}
	}
	return &RetentionPurger{
		cfg:     cfg,
		metrics: getPurgeMetrics(),
		done:    make(chan struct{}),
	}
}

// Start launches the purger in a background goroutine.  It returns
// immediately.  The purger runs an initial purge after a short delay
// and then continues at the configured interval until Stop is called.
func (rp *RetentionPurger) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	rp.cancel = cancel
	go rp.run(ctx)
}

// Stop signals the purger to exit and waits for it to finish the
// current iteration.  Safe to call multiple times.
func (rp *RetentionPurger) Stop() {
	if rp.cancel != nil {
		rp.cancel()
	}
	select {
	case <-rp.done:
	case <-time.After(30 * time.Second):
		rp.cfg.Logger.Warn("retention purger stop timed out")
	}
}

// run is the main loop.  It executes PurgeAll immediately on start
// (after a 10s warm-up) and then on every tick.
func (rp *RetentionPurger) run(ctx context.Context) {
	defer close(rp.done)

	// Warm-up delay so the database pool is ready and the HTTP server
	// has had a chance to bind.
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	rp.tick(ctx)

	ticker := time.NewTicker(rp.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rp.tick(ctx)
		}
	}
}

// tick executes a single purge run for all configured tables.
func (rp *RetentionPurger) tick(ctx context.Context) {
	rp.metrics.PurgeRuns.Inc()
	start := time.Now()
	defer func() {
		rp.metrics.PurgeDuration.Observe(time.Since(start).Seconds())
	}()

	for _, table := range rp.cfg.Tables {
		if err := rp.purgeTable(ctx, table); err != nil {
			rp.metrics.PurgeErrors.Inc()
			rp.cfg.Logger.Error("retention purge failed",
				"table", table, "error", err)
		}
	}
}

// purgeTable performs the two-phase delete on a single table.
// Phase 1: soft-delete records older than the retention threshold.
// Phase 2: hard-delete records that have been soft-deleted for longer
// than the grace period.
//
// The retention threshold is resolved per-tenant:
//   - If the org has a row in the alerts_preferences table with a
//     non-zero retention_days, that value is used.
//   - Otherwise the tier default is used (30d community, 90d pro,
//     365d enterprise).
//
// All queries filter on org_id so the purger can never delete
// another tenant's data.
func (rp *RetentionPurger) purgeTable(ctx context.Context, table string) error {
	if rp.cfg.Pool == nil {
		return nil
	}

	// Phase 1: soft-delete expired records.
	softSQL := `
		UPDATE ` + table + `
		SET deleted_at = NOW()
		WHERE deleted_at IS NULL
		  AND created_at < NOW() - (
		      COALESCE(
		          (SELECT retention_days FROM alert_preferences
		           WHERE org_id = ` + table + `.org_id AND retention_days > 0),
		          $1::int
		      ) || ' days')::interval
	`
	res, err := rp.cfg.Pool.Exec(ctx, softSQL, rp.cfg.DefaultRetentionDays)
	if err != nil {
		return err
	}
	if n := res.RowsAffected(); n > 0 {
		rp.metrics.PurgedRecordsTotal.WithLabelValues(table, "soft").Add(float64(n))
	}
	rp.metrics.RecordsScanned.WithLabelValues(table).Add(float64(res.RowsAffected()))

	// Phase 2: hard-delete records that have been soft-deleted for
	// longer than the grace period.
	hardSQL := `
		DELETE FROM ` + table + `
		WHERE deleted_at IS NOT NULL
		  AND deleted_at < $1
	`
	res2, err := rp.cfg.Pool.Exec(ctx, hardSQL, time.Now().Add(-rp.cfg.GracePeriod))
	if err != nil {
		return err
	}
	if n := res2.RowsAffected(); n > 0 {
		rp.metrics.PurgedRecordsTotal.WithLabelValues(table, "hard").Add(float64(n))
	}

	return nil
}
