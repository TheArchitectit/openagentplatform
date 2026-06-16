// Command oap-agent is the OpenAgentPlatform endpoint agent.
//
// It runs as a long-lived daemon on a managed endpoint (Windows, Linux, macOS).
// The agent connects to the platform's NATS server with mTLS, registers with
// the platform API, publishes heartbeats, and responds to check + script
// commands received on its per-agent subjects.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/agent"
	"github.com/openagentplatform/openagentplatform/pkg/logger"
)

var (
	configPath string
	doRegister bool
	showVer    bool
)

func main() {
	flag.StringVar(&configPath, "config", "", "Path to agent config (YAML or JSON). Defaults to OS-specific path.")
	flag.BoolVar(&doRegister, "register", false, "Run one-shot registration, then exit (does not run the daemon).")
	flag.BoolVar(&showVer, "version", false, "Print version and exit.")
	flag.Parse()

	if showVer {
		fmt.Printf("oap-agent %s\n", agent.AgentVersion)
		return
	}

	cfg, err := agent.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)
	log.Info("oap-agent starting",
		"version", agent.AgentVersion,
		"site_id", cfg.SiteID,
		"agent_id", cfg.AgentID,
		"api_url", cfg.APIURL,
		"nats_url", cfg.NATSURL,
		"config_path", cfg.ConfigPath,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Collect host info early — used for both registration and heartbeats.
	hi, err := agent.CollectHostInfo()
	if err != nil {
		log.Warn("hostinfo collect failed", "err", err)
		hi = &agent.HostInfo{AgentVersion: agent.AgentVersion}
	}

	apiClient := agent.NewAPIClient(cfg.APIURL, cfg.AuthToken, cfg.APIInsec, log)

	// If we have no agent_id/token, perform registration (one-shot or interactive).
	if cfg.AgentID == "" || cfg.AuthToken == "" {
		if err := agent.RegisterAgent(ctx, cfg, apiClient, hi, log); err != nil {
			log.Error("registration failed", "err", err)
			os.Exit(1)
		}
		if doRegister {
			log.Info("register-only mode complete", "agent_id", cfg.AgentID)
			return
		}
	}

	if doRegister {
		// Already registered above, exit cleanly.
		return
	}

	// Connect to NATS with mTLS.
	natsClient, err := agent.ConnectNATS(ctx, cfg.NATSURL, cfg.NATSCAFile, cfg.NATSCert, cfg.NATSKey, log)
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	// Subscribe to check + script subjects.
	checksSub, err := agent.RunChecksHandler(ctx, cfg.AgentID, natsClient, log)
	if err != nil {
		log.Error("checks handler failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		_ = checksSub.Unsubscribe()
	}()

	scriptsSub, err := agent.RunScriptsHandler(ctx, cfg.AgentID, cfg.ScriptTimeoutSec, natsClient, log)
	if err != nil {
		log.Error("scripts handler failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		_ = scriptsSub.Unsubscribe()
	}()

	// Heartbeat goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.RunHeartbeat(ctx, cfg.AgentID, cfg.HeartbeatIntervalSec, natsClient, log)
	}()

	log.Info("oap-agent running",
		"agent_id", cfg.AgentID,
		"checks_subject", agent.ChecksSubject(cfg.AgentID),
		"scripts_subject", agent.ScriptsSubject(cfg.AgentID),
		"heartbeat_subject", agent.HeartbeatSubject(cfg.AgentID),
	)

	<-ctx.Done()
	log.Info("shutdown signal received, draining...")

	// Give in-flight work a moment to finish, then bail out.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
		log.Info("clean shutdown complete")
	case <-shutdownCtx.Done():
		log.Warn("shutdown deadline reached, exiting")
	}
}
