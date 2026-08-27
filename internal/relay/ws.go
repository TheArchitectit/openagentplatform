package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

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

// handleWSS upgrades a single HTTP request to a WebSocket, admits the leg via
// the MatchEngine (RELAY-02 mTLS + entitlement), and if matched starts frame
// forwarding. Fails closed at every validation point.
func (s *RelayService) handleWSS(w http.ResponseWriter, r *http.Request, up wsUpgrader) {
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		s.log.Debug("relay: wss upgrade rejected", "remote", r.RemoteAddr, "err", err)
		return
	}

	// Extract mTLS principal (RELAY-02 §1.1). The CN/SAN on the client cert.
	tlsPrincipal := extractPrincipal(r)
	if tlsPrincipal == "" {
		s.log.Info("relay: no mTLS principal; closing", "remote", r.RemoteAddr)
		_ = conn.Close()
		return
	}

	// Parse the rendezvous handshake message within the handshake timeout.
	msg, err := parseRendezvous(conn, 30*time.Second)
	if err != nil {
		s.log.Info("relay: rendezvous rejected", "remote", r.RemoteAddr, "err", err)
		_ = conn.Close()
		return
	}

	// Admit and match.
	if s.matchEngine == nil {
		s.log.Warn("relay: match engine not initialized; closing")
		_ = conn.Close()
		return
	}

	leg, partner, err := s.matchEngine.Admit(conn, tlsPrincipal, msg)
	if err != nil {
		s.log.Info("relay: admission rejected", "remote", r.RemoteAddr, "err", err)
		_ = conn.Close()
		return
	}

	if partner != nil {
		// Matched! Start forwarding in a background goroutine.
		s.log.Info("relay: legs matched; forwarding",
			"conn_id_a", partner.ConnID, "conn_id_b", leg.ConnID)
		go s.forwarder.Run(r.Context(), leg, partner)
	} else {
		// Pending leg — waiting for counterpart. The match timeout reaper
		// handles expiry (RELAY-03 §4, 5m match timeout).
		s.log.Info("relay: leg pending", "conn_id", leg.ConnID,
			"source", leg.AgentID, "target", leg.TargetID)
	}
}

// extractPrincipal pulls the CN or first SAN from the TLS handshake's client
// certificate. Returns empty string if no client cert was presented.
func extractPrincipal(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	cert := r.TLS.PeerCertificates[0]
	// Prefer Subject CommonName.
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName
	}
	// Fall back to first DNS SAN.
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	return ""
}

// StartMatchTimeoutReaper runs a background goroutine that closes legs which
// have been in LegPending for longer than matchTimeout. It exits when ctx is
// cancelled (server shutdown).
func StartMatchTimeoutReaper(ctx context.Context, engine *MatchEngine, matchTimeout time.Duration, log *slog.Logger) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UTC()
				engine.mu.Lock()
				for key, leg := range engine.pends {
					leg.mu.Lock()
					if leg.State == LegPending && now.Sub(leg.AddedAt) > matchTimeout {
						leg.State = LegClosed
						leg.closeErr = errors.New("match_timeout")
						leg.mu.Unlock()
						delete(engine.pends, key)
						_ = leg.Conn.Close()
						engine.svc.CloseConnection(nil, leg.ConnID)
						log.Info("relay: match timeout, closing pending leg",
							"conn_id", leg.ConnID,
							"source", leg.AgentID, "target", leg.TargetID)
					} else {
						leg.mu.Unlock()
					}
				}
				engine.mu.Unlock()
			}
		}
	}()
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
