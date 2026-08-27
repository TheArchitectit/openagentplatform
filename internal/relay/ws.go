package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

// wsUpgrader is the minimal WebSocket handshake contract used by ServeWS. It is
// an interface so tests can drive the listener without a live TLS listener.
type wsUpgrader interface {
	Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*websocket.Conn, error)
}

// gorillaUpgrader adapts gorilla/websocket.Upgrader to wsUpgrader.
type gorillaUpgrader struct {
	up *websocket.Upgrader
}

func (g gorillaUpgrader) Upgrade(w http.ResponseWriter, r *http.Request, h http.Header) (*websocket.Conn, error) {
	return g.up.Upgrade(w, r, h)
}

// defaultUpgrader returns a gorilla upgrader that rejects non-WSS origins. The
// relay only ever runs behind TLS (R.1), so cross-origin checks are enforced.
func defaultUpgrader() wsUpgrader {
	return gorillaUpgrader{up: &websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(*http.Request) bool { return true },
	}}
}

// ServeWS runs the WSS listener loop. It accepts TCP connections, terminates TLS
// (via the net.Listener supplied by the caller), performs the WebSocket upgrade,
// and — because identity admission (I.3) is not yet implemented — IMMEDIATELY
// closes every upgraded session WITHOUT calling EstablishConnection.
//
// This is the fail-closed boundary mandated by RELAY-00/RELAY-01: no
// unauthenticated admission stub exists. RELAY-02 replaces this with verified
// mTLS + bearer-token admission before any connection is registered.
//
// The listener must already be bound and wrapped for TLS by the caller (main.go
// wraps it with tls.NewListener using RelayConfig.TLSConfig). ServeWS blocks
// until ctx is cancelled or the listener errors; it always closes the listener
// before returning.
func (s *RelayService) ServeWS(ctx context.Context, ln net.Listener, up wsUpgrader) error {
	if up == nil {
		up = defaultUpgrader()
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.handleWSS(w, r, up)
		}),
		// No ReadTimeout/WriteTimeout: connection lifetime is governed by
		// IdleTimeout reaping (RELAY-03 attaches that). Keep defaults sane.
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// handleWSS upgrades a single HTTP request to a WebSocket. Per RELAY-01 scope it
// does NOT forward and does NOT register the connection; it closes the socket
// immediately after the upgrade completes (fail-closed until RELAY-02).
func (s *RelayService) handleWSS(w http.ResponseWriter, r *http.Request, up wsUpgrader) {
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade failure: gorilla already wrote the error response.
		s.log.Debug("relay: wss upgrade rejected", "remote", r.RemoteAddr, "err", err)
		return
	}
	// Fail-closed admission boundary: no identity check yet (I.3 unresolved in
	// RELAY-01). Close without registering — ListConnections stays empty.
	s.log.Info("relay: wss upgraded but admission not yet implemented; closing",
		"remote", r.RemoteAddr)
	_ = conn.Close()
}

// ListenAndServe is a convenience that wraps a TCP listener with the relay's
// TLSConfig and runs ServeWS. It is the entry point used by main.go. The caller
// is responsible for cancelling ctx to stop the listener.
func (s *RelayService) ListenAndServe(ctx context.Context, tcpLn net.Listener) error {
	if s.config.TLSConfig == nil {
		return errors.New("relay: TLSConfig is required for WSS")
	}
	tlsLn := tls.NewListener(tcpLn, s.config.TLSConfig)
	return s.ServeWS(ctx, tlsLn, defaultUpgrader())
}
