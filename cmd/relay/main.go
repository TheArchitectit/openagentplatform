// Command oap-relay is the OpenAgentPlatform managed A2A relay binary.
//
// It runs the full approved relay stack: a WSS listener with mTLS + signed
// bearer-token admission (RELAY-02 I.3), entitlement-gated matching and blind
// forwarding (RELAY-03, E.4), per-tenant metering and idle reaping (RELAY-04),
// discovery federation (RELAY-05), and the operator admin surface (RELAY-04).
// A session is registered only after admission passes; denied clients are closed
// without registration.
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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openagentplatform/openagentplatform/internal/db"
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

	// ADR §1.4: seed provenance signing (local records) and peer verification
	// keys (inbound records) from the trust config. Fail-closed on a malformed
	// peer public key.
	if trustCfg.SigningKey() != nil {
		registry.SetSigningKey(trustCfg.SigningKey())
	}
	verifyKeys, err := trustCfg.PeerVerifyKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}
	registry.SetPeerVerifyKeys(verifyKeys)

	var fed *relay.Federation
	if trustCfg.Federation != nil && len(trustCfg.Federation.Peers) > 0 {
		fed = relay.NewFederation(relayID, registry, cfg.TLSConfig, trustCfg.Federation, log)
		registry.SetObserver(fed.Broadcast)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Durable state (a2a-relay §8.7): optional Postgres-backed ledger. The
	// relay is the sole schema consumer of its own tables, so it applies the
	// embedded migration set itself at boot when a store DSN is configured.
	var store relay.Store
	if flags.StoreDSN != "" {
		if err := db.Migrate(ctx, flags.StoreDSN, log); err != nil {
			fmt.Fprintf(os.Stderr, "relay: store migrate: %v\n", err)
			os.Exit(1)
		}
		pool, err := pgxpool.New(ctx, flags.StoreDSN)
		if err != nil {
			fmt.Fprintf(os.Stderr, "relay: store pool: %v\n", err)
			os.Exit(1)
		}
		store = relay.NewPGStore(pool)
		if err := svc.SetStore(ctx, store); err != nil {
			fmt.Fprintf(os.Stderr, "relay: store install: %v\n", err)
			os.Exit(1)
		}
		log.Info("relay: durable state enabled", "store", "postgres")
	}

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

	// Periodic byte-meter flush (§8.4); final flush on shutdown below.
	if store != nil {
		svc.StartFlushLoop(ctx)
	}

	if err := svc.ListenAndServe(ctx, tcpLn); err != nil {
		log.Error("relay: listener exited", "err", err)
		if store != nil {
			svc.FlushPendingBytes(context.Background())
			store.Close()
		}
		os.Exit(1)
	}

	// If the admin server errored, surface it after WSS shutdown.
	if err := <-adminErr; err != nil {
		log.Error("relay: admin listener exited", "err", err)
		if store != nil {
			svc.FlushPendingBytes(context.Background())
			store.Close()
		}
		os.Exit(1)
	}

	// Graceful shutdown: flush buffered metering deltas before exit (§8.4),
	// bounded so a wedged store can't hang shutdown forever.
	if store != nil {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		svc.FlushPendingBytes(flushCtx)
		store.Close()
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
