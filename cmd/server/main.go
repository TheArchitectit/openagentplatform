package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/checklib"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/db"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/logger"
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

	// --- Server -----------------------------------------------------------
	srv, err := NewServer(cfg, log, pool, natsClient)
	if err != nil {
		log.Error("server init failed", "err", err)
		os.Exit(1)
	}

	// Seed built-in compliance policies on first boot. Idempotent.
	seedCtx2, seedCancel2 := context.WithTimeout(rootCtx, 15*time.Second)
	if seeded, skipped, err := srv.policyEngine.SeedDefaults(seedCtx2); err != nil {
		log.Warn("policy seeder failed", "err", err)
	} else {
		log.Info("policy defaults seeded", "seeded", seeded, "skipped", skipped)
	}
	seedCancel2()

	if err := srv.Start(rootCtx); err != nil {
		log.Error("server start failed", "err", err)
		os.Exit(1)
	}

	// --- Shutdown ---------------------------------------------------------
	<-rootCtx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
}