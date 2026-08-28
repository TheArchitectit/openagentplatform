// Command oap-relay is the OpenAgentPlatform managed A2A relay binary.
//
// RELAY-01 establishes the runnable foundation: a WSS listener that binds,
// terminates TLS, and upgrades to WebSocket. It deliberately does NOT forward or
// match legs (RELAY-03), perform identity admission (RELAY-02), meter (RELAY-04),
// or federate discovery (RELAY-05). Every upgraded session is closed without
// registration until identity admission lands.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/openagentplatform/openagentplatform/internal/relay"
	"github.com/openagentplatform/openagentplatform/pkg/logger"
)

func main() {
	flags, err := ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(2)
	}

	cfg, err := flags.relayConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}

	// I.3: load the issued-identity trust config (token verification key +
	// entitlement grants). Fail-closed — the relay must not start without it.
	trustCfg, err := relay.LoadTrustConfig(flags.TrustConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}

	log := logger.New("info")

	svc := relay.NewRelayService(cfg, log)
	svc.SetTrustConfig(trustCfg)

	// Discovery: local registry + optional federation (RELAY-05). When the
	// trust config contains a federation section with peers, the Federation
	// driver is started; otherwise the registry is local-only.
	relayID := "local" // TODO: derive from relay cert CN once relay-cert SAN convention lands
	registry := relay.NewDiscoveryRegistry(relayID, log)
	var fed *relay.Federation
	if trustCfg.Federation != nil && len(trustCfg.Federation.Peers) > 0 {
		fed = relay.NewFederation(relayID, registry, cfg.TLSConfig, trustCfg.Federation, log)
		registry.SetObserver(fed.Broadcast)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Bind the raw TCP listener; ListenAndServe wraps it in TLS (R.1 requires
	// WSS). Bind failures are fatal and reported before any goroutine starts.
	tcpLn, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Error("relay: listen failed", "addr", cfg.ListenAddr, "err", err)
		os.Exit(1)
	}

	// Start the admin listener (RELAY-04) in a goroutine. It binds a separate
	// loopback-default port and requires mTLS with a role SAN. A failure to
	// configure or bind the admin surface is fatal — the relay must not run
	// without a secured operator surface.
	adminErr := startAdmin(ctx, flags, svc, registry, log)

	log.Info("relay: starting WSS listener", "addr", cfg.ListenAddr, "max_connections", cfg.MaxConnections)

	// Start federation (push+pull+ping loops) before WSS so startup
	// reconciliation finishes before any agent connections arrive.
	if fed != nil {
		log.Info("relay: starting discovery federation", "peers", fed.PeerCount())
		go fed.Start(ctx)
	}
	if err := svc.ListenAndServe(ctx, tcpLn); err != nil {
		log.Error("relay: listener exited", "err", err)
		os.Exit(1)
	}

	// If the admin server errored, surface it after WSS shutdown.
	if err := <-adminErr; err != nil {
		log.Error("relay: admin listener exited", "err", err)
		os.Exit(1)
	}

	log.Info("relay: shutdown complete")
}

// startAdmin configures and serves the admin listener. It returns a channel
// that receives the server's exit error (nil on clean shutdown). It blocks on
// configuration and bind errors, which are fatal.
func startAdmin(ctx context.Context, flags *Flags, svc *relay.RelayService, registry *relay.DiscoveryRegistry, log *slog.Logger) <-chan error {
	errCh := make(chan error, 1)

	tlsCfg, err := flags.adminTLSConfig()
	if err != nil {
		errCh <- err
		return errCh
	}

	adminLn, err := net.Listen("tcp", flags.AdminAddr)
	if err != nil {
		errCh <- fmt.Errorf("relay: admin listen failed on %s: %w", flags.AdminAddr, err)
		return errCh
	}
	tlsLn := tls.NewListener(adminLn, tlsCfg)

	admin := relay.NewAdminServer(svc, log)
	admin.SetDiscoveryRegistry(registry)

	log.Info("relay: starting admin listener", "addr", flags.AdminAddr)
	go func() {
		errCh <- admin.Serve(ctx, tlsLn)
	}()
	return errCh
}
