package main

// server_reports.go wires the enterprise reporting stack: pgx store,
// data aggregator, report engine, deliverer, and the cron scheduler.
// All failures are logged and non-fatal — /reports endpoints return 503
// when the scheduler is nil.

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/reports"
)

func wireReports(apiServer *api.Server, pool *pgxpool.Pool, log *slog.Logger) *reports.Scheduler {
	store := reports.NewPGStore(pool)
	if err := store.EnsureSchema(context.Background()); err != nil {
		log.Warn("reports: schema creation failed, reports endpoints will 503", "err", err)
		return nil
	}

	agg := reports.NewPGAggregator(pool)
	engine := reports.NewReportEngine(agg, log)

	deliverer := reports.NewDefaultDeliverer()
	deliverer.SMTPHost = os.Getenv("REPORTS_SMTP_HOST")
	deliverer.SMTPPort = envInt("REPORTS_SMTP_PORT", 587)
	deliverer.Username = os.Getenv("REPORTS_SMTP_USERNAME")
	deliverer.Password = os.Getenv("REPORTS_SMTP_PASSWORD")
	deliverer.FromAddress = os.Getenv("REPORTS_SMTP_FROM")
	if secret := os.Getenv("REPORTS_DOWNLOAD_SECRET"); secret != "" {
		deliverer.DownloadSecret = []byte(secret)
	}
	deliverer.BaseURL = os.Getenv("REPORTS_BASE_URL")

	scheduler := reports.NewScheduler(engine, store, deliverer, log)
	apiServer.SetReportsStore(store)
	apiServer.SetReportsScheduler(scheduler)
	apiServer.SetReportsDeliverer(deliverer)
	return scheduler
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
