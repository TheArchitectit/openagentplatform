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
	"sort"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/agent"
	"github.com/openagentplatform/openagentplatform/pkg/agent/checkers"
	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
	"github.com/openagentplatform/openagentplatform/pkg/logger"
)

var (
	configPath    string
	doRegister    bool
	showVer       bool
	listCheckers  bool
)

func main() {
	flag.StringVar(&configPath, "config", "", "Path to agent config (YAML or JSON). Defaults to OS-specific path.")
	flag.BoolVar(&doRegister, "register", false, "Run one-shot registration, then exit (does not run the daemon).")
	flag.BoolVar(&showVer, "version", false, "Print version and exit.")
	flag.BoolVar(&listCheckers, "list-checkers", false, "Print available checkers (with metadata) and exit.")
	flag.Parse()

	if showVer {
		fmt.Printf("oap-agent %s\n", agent.AgentVersion)
		return
	}

	if listCheckers {
		printCheckers()
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

	// Log every registered checker with its metadata.
	logRegisteredCheckers(log)

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

	// Subscribe to compliance collection requests from the server.
	complianceHandler := agent.NewComplianceHandler(cfg.AgentID, natsClient, log)
	complianceSub, err := complianceHandler.Run(ctx)
	if err != nil {
		log.Error("compliance handler failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		complianceHandler.Close()
		_ = complianceSub.Unsubscribe()
	}()

	// Subscribe to patch scan + install commands from the server.
	patchHandler := patcher.NewHandler(cfg.AgentID, natsClient.Conn(), log)
	patchScanSub, err := patchHandler.RunScanHandler(ctx)
	if err != nil {
		log.Error("patch scan handler failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		_ = patchScanSub.Unsubscribe()
	}()
	patchInstallSub, err := patchHandler.RunInstallHandler(ctx)
	if err != nil {
		log.Error("patch install handler failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		patchHandler.Close()
		_ = patchInstallSub.Unsubscribe()
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
		"compliance_subject", "oap.agents."+cfg.AgentID+".compliance",
		"patch_scan_subject", patcher.PatchScanSubject(cfg.AgentID),
		"patch_install_subject", patcher.PatchInstallSubject(cfg.AgentID),
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

// logRegisteredCheckers logs each registered checker with its metadata at startup.
func logRegisteredCheckers(log *slog.Logger) {
	metas := checkers.AllMetadata()
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	for _, m := range metas {
		log.Info("checker registered",
			"name", m.Name,
			"version", m.Version,
			"description", m.Description,
			"supported_platforms", m.SupportedPlatforms,
		)
	}
	log.Info("checkers registered", "count", len(metas))
}

// printCheckers writes a human-readable table of available checkers to stdout.
func printCheckers() {
	metas := checkers.AllMetadata()
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "NAME\tVERSION\tPLATFORMS\tDESCRIPTION\n")
	for _, m := range metas {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			m.Name,
			m.Version,
			joinOrAny(m.SupportedPlatforms),
			m.Description,
		)
	}
	_ = tw.Flush()
}

func joinOrAny(platforms []string) string {
	if len(platforms) == 0 {
		return "any"
	}
	out := ""
	for i, p := range platforms {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
