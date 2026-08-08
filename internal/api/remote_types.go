package api

import (
	"log/slog"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/remote"
)

// PermissionRemoteShell is the RBAC permission required to create a
// remote shell session. Admin/operator roles are expected to grant
// this.
const PermissionRemoteShell = "remote:shell"

// RemoteHandler bundles the dependencies the remote-shell API needs.
// It is wired into Server via SetRemoteHandler.

type RemoteHandler struct {
	Manager   *remote.ShellManager
	CredStore *remote.CredentialStore
	Resolver  *remote.Resolver
	Logger    *slog.Logger
	// BaseURL is the public URL the WebSocket client should connect
	// to. It is read by HandleCreateShellSession to build ws_url.
	BaseURL string
	// SessionMinter is used to verify session JWTs in the WebSocket
	// upgrade path. If nil the handler refuses to accept WS upgrades
	// (except in the dev fallback).
	SessionMinter *auth.SessionMinter
	// CookieName is the session cookie to read from on the upgrade.
	CookieName string
	// NATSConn is the connection used to subscribe to per-session
	// stdout subjects. May be nil in dev/test mode.
	NATSConn NATSConn
}

// NATSConn is the subset of *nats.Conn used by the shell bridge.
type NATSConn interface {
	Subscribe(subj string, cb natsMsgHandler) (NATSSub, error)
	Publish(subj string, data []byte) error
}

// natsMsgHandler matches nats.MsgHandler.
type natsMsgHandler func(*natsMsg)

// NATSSub is the subset of *nats.Subscription used here.
type NATSSub interface {
	Unsubscribe() error
}

// NewRemoteHandler constructs a handler with safe defaults.
func NewRemoteHandler(log *slog.Logger) *RemoteHandler {
	return &RemoteHandler{
		Logger:     log,
		CookieName: "oap_session",
	}
}

// SetRemoteHandler wires the remote-handler dependencies into the
// server. The handler may be nil — endpoints will then return 503.
