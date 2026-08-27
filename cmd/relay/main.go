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
	"fmt"
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

	log := logger.New("info")

	svc := relay.NewRelayService(cfg, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Bind the raw TCP listener; ListenAndServe wraps it in TLS (R.1 requires
	// WSS). Bind failures are fatal and reported before any goroutine starts.
	tcpLn, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Error("relay: listen failed", "addr", cfg.ListenAddr, "err", err)
		os.Exit(1)
	}

	log.Info("relay: starting WSS listener", "addr", cfg.ListenAddr, "max_connections", cfg.MaxConnections)
	if err := svc.ListenAndServe(ctx, tcpLn); err != nil {
		log.Error("relay: listener exited", "err", err)
		os.Exit(1)
	}

	log.Info("relay: shutdown complete")
}
