package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeUpgrader is a wsUpgrader stub used to assert that ServeWS calls Upgrade
// and that handleWSS closes the connection without registering it. It records
// the number of upgrade attempts and hands back a canned conn.
type fakeUpgrader struct {
	calls   int
	lastReq *http.Request
	conn    *websocket.Conn // nil means Upgrade returns an error
	err     error
}

func (f *fakeUpgrader) Upgrade(w http.ResponseWriter, r *http.Request, _ http.Header) (*websocket.Conn, error) {
	f.calls++
	f.lastReq = r
	if f.err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return nil, f.err
	}
	if f.conn != nil {
		return f.conn, nil
	}
	http.Error(w, "no conn", http.StatusInternalServerError)
	return nil, f.err
}

// testTLSConfig builds a self-signed server cert for 127.0.0.1 used by the
// WSS handshake tests. Test-only; never committed.
func testTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		DNSNames:     []string{"127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("x509 key pair: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}

// TestRelayWS_ServeWS_UpgradesThenRejectsUnauthenticated verifies the WSS
// upgrade may complete, but the session is closed and no connection is
// registered (ListConnections stays empty). This is the fail-closed boundary
// until RELAY-02 implements identity admission.
func TestRelayWS_ServeWS_UpgradesThenRejectsUnauthenticated(t *testing.T) {
	svc := NewRelayService(RelayConfig{ListenAddr: "127.0.0.1:0"}, nil)

	// Bind a real TLS listener so a client can actually upgrade.
	tlsCfg := testTLSConfig(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = svc.ServeWS(ctx, ln, nil) }()

	// Give the server a moment to start, then connect over WSS.
	time.Sleep(50 * time.Millisecond)
	wsURL := "wss://" + ln.Addr().String()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	wsConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("wss dial: %v", err)
	}
	defer wsConn.Close()

	// The server must close the socket (no frame relay, no registration).
	done := make(chan error, 1)
	go func() {
		_, _, err := wsConn.ReadMessage()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected server to close the connection, got nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not close the unauthenticated session within 3s")
	}

	// No connection may have been registered.
	if conns := svc.ListConnections(ctx, "any-tenant"); len(conns) != 0 {
		t.Fatalf("registered connections = %d, want 0 (fail-closed)", len(conns))
	}
}

// TestRelayWS_ServeWS_HandshakeNoUpgrade_Rejected verifies a non-WSS HTTP
// request is rejected (returns non-101).
func TestRelayWS_ServeWS_HandshakeNoUpgrade_Rejected(t *testing.T) {
	svc := NewRelayService(RelayConfig{ListenAddr: "127.0.0.1:0"}, nil)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = svc.ServeWS(ctx, ln, nil) }()

	time.Sleep(50 * time.Millisecond)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatal("expected non-upgrade rejection, got 101 Switching Protocols")
	}
}
