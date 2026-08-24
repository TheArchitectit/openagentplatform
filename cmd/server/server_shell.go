package main

// server_shell.go wires the remote-shell stack: session manager,
// credential store + resolver, WebSocket handler, recording store, and
// the live session-recorder factory. Failures of optional parts are
// logged and non-fatal; the shell endpoints return 503 when the core
// manager cannot be constructed.

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/remote"
)

func wireShell(apiServer *api.Server, pool *pgxpool.Pool, nc *nats.Conn, log *slog.Logger) {
	// Agent inventory lookup so start requests carry a real SSH host.
	store := newAgentStoreAdapter(pool)
	hostResolver := func(agentID string) string {
		ag, err := store.GetAgent(context.Background(), "", agentID)
		if err != nil || ag == nil {
			return ""
		}
		if ag.Hostname != "" {
			return ag.Hostname
		}
		return ag.PublicIP
	}

	// Credential store: AES-GCM encrypted in-memory store keyed from
	// SHELL_CREDENTIAL_KEY (min 16 bytes). Without a key we still wire
	// the manager — storing credentials will just be unavailable.
	var credStore *remote.CredentialStore
	if key := os.Getenv("SHELL_CREDENTIAL_KEY"); key != "" {
		cs, err := remote.NewCredentialStore([]byte(key))
		if err != nil {
			log.Warn("shell: credential store init failed", "err", err)
		} else {
			credStore = cs
		}
	} else {
		log.Info("shell: SHELL_CREDENTIAL_KEY not set, credential storage disabled")
	}

	manager := remote.NewShellManager(remote.DefaultShellManagerConfig(), nc, log)
	manager.SetHostResolver(hostResolver)
	handler := api.NewRemoteHandler(log)
	handler.Manager = manager
	handler.CredStore = credStore
	if credStore != nil {
		handler.Resolver = remote.NewResolver(credStore)
	}
	handler.NATSConn = api.NewShellNATSConn(nc)
	handler.SessionMinter = apiServer.SessionMinter()
	handler.CookieName = "oap_session"

	apiServer.SetRemoteHandler(handler)

	// Recording store + live recorder factory. Schema creation is
	// idempotent; failure only disables the recordings endpoints.
	recStore := remote.NewPGStore(pool)
	if err := recStore.EnsureSchema(context.Background()); err != nil {
		log.Warn("shell: recording store unavailable, recordings endpoints 503", "err", err)
	} else {
		apiServer.SetRecordingStore(recStore)
		apiServer.SetSessionRecorderFactory(func(sessionID string) (*remote.SessionRecorder, bool) {
			sess := manager.Get(sessionID)
			if sess == nil {
				return nil, false
			}
			return remote.NewSessionRecorder(sess, recStore, log, remote.RecorderConfig{}), true
		})
	}

	go manager.Run(context.Background())
}
