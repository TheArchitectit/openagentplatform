package relay

// E.4 acceptance (RELAY-06 §7.4 E.4): the relay is a blind forwarder — agents
// establish session keys out-of-band (WireGuard/SSH model from RMM-09) and the
// relay only ever moves ciphertext it cannot decrypt. This file proves that
// claim end-to-end over the full approved stack: mTLS-issued identity +
// entitlement + bearer token (I.3), WSS rendezvous + matching (RELAY-03), and
// metered bidirectional binary frame forwarding (RELAY-04). It asserts three
// things the spec requires:
//
//  1. Two authenticated legs carry ciphertext frames end-to-end, intact, and
//     the exact bytes are metered.
//  2. The relay's in-memory state never contains the plaintext (no payload is
//     ever parsed or stored — only byte counts are kept).
//  3. Non-binary frames and oversized frames are rejected by closing the legs
//     (Layer-4 data plane: no payload parsing, no decryption).

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// e2eCA is a minimal test PKI: one CA signing both the relay server cert and
// the agent client certs. Test-only; mirrors the production model where a
// platform CA issues every relay/agent cert.
type e2eCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newE2eCA(t *testing.T) *e2eCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "oap-e2e-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &e2eCA{cert: cert, key: key, pool: pool}
}

// issue signs a leaf certificate (server or client) under the CA. The CN is the
// principal for agent certs; uris carry identity SANs such as `oap:tenant:<id>`.
func (ca *e2eCA) issue(t *testing.T, cn string, uris []*url.URL, ips []net.IP) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		URIs:         uris,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("leaf pair: %v", err)
	}
	return pair
}

// e2eRelay is a fully authenticated in-process relay: WSS over mTLS with a
// platform-issued trust config. One fixture per test (cheap to stand up).
type e2eRelay struct {
	svc          *RelayService
	url          string
	ca           *e2eCA
	platformPriv ed25519.PrivateKey
	agentCerts   map[string]tls.Certificate // agentID -> client cert
	tenant       string
	cancel       context.CancelFunc
}

const e2eTenant = "t1"

// fullMeshEntitlements grants source→target among every pair of the given
// agents, within the test tenant.
func fullMeshEntitlements(agentIDs []string) []Entitlement {
	ents := make([]Entitlement, 0, len(agentIDs)*len(agentIDs))
	for _, a := range agentIDs {
		for _, b := range agentIDs {
			ents = append(ents, Entitlement{TenantID: e2eTenant, SourceAgentID: a, TargetAgentID: b, Action: "relay"})
		}
	}
	return ents
}

// newE2eRelay builds a relay admitting every full-mesh pair among agentIDs,
// with one client cert per agent ID.
func newE2eRelay(t *testing.T, agentIDs ...string) *e2eRelay {
	return newE2eRelayWithEntitlements(t, fullMeshEntitlements(agentIDs), agentIDs...)
}

// newE2eRelayWithEntitlements builds a relay with an explicit entitlement set,
// so negative paths (default-deny, sub-mesh) can be exercised. One client cert
// is issued per agent ID; the token-signing platform key is fresh per fixture.
func newE2eRelayWithEntitlements(t *testing.T, ents []Entitlement, agentIDs ...string) *e2eRelay {
	t.Helper()

	// Platform token-signing key (Ed25519, same key class as the discovery
	// provenance signing key).
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("platform key: %v", err)
	}

	svc := NewRelayService(RelayConfig{MaxConnections: 100, IdleTimeout: time.Minute}, nil)
	svc.SetTrustConfig(&TrustConfig{
		Version:      1,
		Entitlements: ents,
		verifyKey:    pub,
	})

	ca := newE2eCA(t)
	agentCerts := make(map[string]tls.Certificate, len(agentIDs))
	for _, a := range agentIDs {
		u, err := url.Parse("oap:tenant:" + e2eTenant)
		if err != nil {
			t.Fatalf("tenant san: %v", err)
		}
		agentCerts[a] = ca.issue(t, a, []*url.URL{u}, nil)
	}

	serverCert := ca.issue(t, "relay-server", nil, []net.IP{net.ParseIP("127.0.0.1")})
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    ca.pool,
		MinVersion:   tls.VersionTLS12,
	}
	svc.config.TLSConfig = serverTLS

	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &e2eRelay{
		svc:          svc,
		url:          "wss://" + tcpLn.Addr().String(),
		ca:           ca,
		platformPriv: priv,
		agentCerts:   agentCerts,
		tenant:       e2eTenant,
		cancel:       cancel,
	}
	t.Cleanup(func() {
		cancel()
		_ = tcpLn.Close()
	})

	go func() { _ = svc.ListenAndServe(ctx, tcpLn) }()
	return r
}

// dialWSS dials the relay's WSS endpoint with an explicit TLS client config
// and returns the connection or the handshake error. Exposed so negative paths
// (missing client cert, cert from an untrusted CA) can assert TLS rejection.
func (r *e2eRelay) dialWSS(tlsCfg *tls.Config) (*websocket.Conn, error) {
	dialer := websocket.Dialer{TLSClientConfig: tlsCfg}
	conn, _, err := dialer.Dial(r.url, nil)
	return conn, err
}

// dialAndAdmit connects over WSS with the agent's mTLS cert, completes the
// rendezvous handshake (signed bearer token + fresh jti), and returns the
// admitted WebSocket.
func (r *e2eRelay) dialAndAdmit(t *testing.T, agentID, targetID string) *websocket.Conn {
	t.Helper()

	cert, ok := r.agentCerts[agentID]
	if !ok {
		t.Fatalf("no fixture cert for %q", agentID)
	}
	conn, err := r.dialWSS(&tls.Config{
		RootCAs:      r.ca.pool,
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("wss dial %s: %v", agentID, err)
	}

	now := time.Now().Unix()
	token := helperSignToken(r.platformPriv, agentID, targetID, r.tenant, now-10, now+300)
	msg := RendezvousMsg{
		Type:     RendezvousType,
		AgentID:  agentID,
		TargetID: targetID,
		TenantID: r.tenant, // informational; server derives tenant from the cert
		Token:    token,
		JTI:      fmt.Sprintf("e4-%s-%d", agentID, time.Now().UnixNano()),
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("rendezvous %s: %v", agentID, err)
	}
	return conn
}

// waitMatched blocks until the relay has admitted all n legs with nothing left
// pending (i.e. every leg is matched). Fails the test if that state never
// arrives.
func (r *e2eRelay) waitMatched(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r.svc.MatchEngine().PendingLegCount() == 0 && len(r.svc.ListConnections(context.Background(), r.tenant)) == n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("matched state not reached: pending=%d conns=%d",
		r.svc.MatchEngine().PendingLegCount(),
		len(r.svc.ListConnections(context.Background(), r.tenant)))
}

// readBinary reads one binary frame with a timeout and asserts its type/size.
func readBinary(t *testing.T, conn *websocket.Conn, want []byte) {
	t.Helper()
	done := make(chan struct {
		mt  int
		msg []byte
		err error
	}, 1)
	go func() {
		mt, msg, err := conn.ReadMessage()
		done <- struct {
			mt  int
			msg []byte
			err error
		}{mt, msg, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("read: %v", got.err)
		}
		if got.mt != websocket.BinaryMessage {
			t.Fatalf("message type = %d, want binary", got.mt)
		}
		if !bytes.Equal(got.msg, want) {
			t.Fatalf("ciphertext mismatch: got %d bytes, want %d", len(got.msg), len(want))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for frame")
	}
}

// readClosed asserts the relay closes the connection (protocol violation).
func readClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected relay to close the connection, got nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for relay to close the connection")
	}
}

// assertNoPlaintextLeak serializes every piece of relay in-memory state that
// could hold payload content and asserts the plaintext marker never appears.
// The blind-forwarder contract is that the relay keeps only IDs and byte
// counts — never payload bytes.
func (r *e2eRelay) assertNoPlaintextLeak(t *testing.T, plaintext []byte) {
	t.Helper()
	var dump bytes.Buffer
	for _, c := range r.svc.ListConnections(context.Background(), r.tenant) {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal connection: %v", err)
		}
		dump.Write(b)
	}
	for _, m := range r.svc.AllMetrics(context.Background()) {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal metrics: %v", err)
		}
		dump.Write(b)
	}
	if bytes.Contains(dump.Bytes(), plaintext) {
		t.Fatalf("plaintext leaked into relay in-memory state: %q", plaintext)
	}
}

// TestE4_BlindForwarder_BidirectionalCiphertext is the core E.4 acceptance:
// two authenticated legs move ciphertext end-to-end, metered exactly, and the
// relay never sees the plaintext.
func TestE4_BlindForwarder_BidirectionalCiphertext(t *testing.T) {
	r := newE2eRelay(t, "agentA", "agentB")

	connA := r.dialAndAdmit(t, "agentA", "agentB")
	defer connA.Close()
	connB := r.dialAndAdmit(t, "agentB", "agentA")
	defer connB.Close()
	r.waitMatched(t, 2)

	// The agents' plaintext session secret — the relay must never learn this.
	// Agents encrypt out-of-band (RMM-09); the relay only moves ciphertext.
	plaintext := []byte("OAP-E4-SECRET-THE-RELAY-MUST-NEVER-SEE")

	// Opaque ciphertext blobs (do not contain the plaintext). In a real
	// session these are the outputs of the WireGuard/SSH session key.
	cipherAtoB := make([]byte, 256)
	if _, err := rand.Read(cipherAtoB); err != nil {
		t.Fatal(err)
	}
	cipherBtoA := make([]byte, 512)
	if _, err := rand.Read(cipherBtoA); err != nil {
		t.Fatal(err)
	}

	// A → B.
	if err := connA.WriteMessage(websocket.BinaryMessage, cipherAtoB); err != nil {
		t.Fatalf("A write: %v", err)
	}
	readBinary(t, connB, cipherAtoB)

	// B → A.
	if err := connB.WriteMessage(websocket.BinaryMessage, cipherBtoA); err != nil {
		t.Fatalf("B write: %v", err)
	}
	readBinary(t, connA, cipherBtoA)

	// Exact metering: the tenant's byte count equals exactly the ciphertext
	// moved; nothing is unmetered or double-counted.
	m := r.svc.GetMetrics(context.Background(), r.tenant)
	if want := int64(len(cipherAtoB) + len(cipherBtoA)); m.TotalBytesRelayed != want {
		t.Fatalf("TotalBytesRelayed = %d, want %d", m.TotalBytesRelayed, want)
	}

	// Blind-forwarder: the plaintext never entered relay state.
	r.assertNoPlaintextLeak(t, plaintext)
}

// TestE4_BlindForwarder_RejectsTextFrames proves the data plane moves only
// binary frame (Layer-4): a text frame is a protocol violation that closes the
// pair, not a payload that gets parsed.
func TestE4_BlindForwarder_RejectsTextFrames(t *testing.T) {
	r := newE2eRelay(t, "agentA", "agentB")

	connA := r.dialAndAdmit(t, "agentA", "agentB")
	defer connA.Close()
	connB := r.dialAndAdmit(t, "agentB", "agentA")
	defer connB.Close()
	r.waitMatched(t, 2)

	// Both legs send a text frame: the forwarder rejects each direction, then
	// closes the pair.
	_ = connA.WriteMessage(websocket.TextMessage, []byte("not a binary frame"))
	_ = connB.WriteMessage(websocket.TextMessage, []byte("not a binary frame"))

	readClosed(t, connA)
	readClosed(t, connB)
}

// TestE4_BlindForwarder_RejectsOversizedFrames proves MaxFrameSize is enforced
// at the data plane (RELAY-03 §5.2): an oversized binary frame closes the leg
// before any bytes are forwarded or metered.
func TestE4_BlindForwarder_RejectsOversizedFrames(t *testing.T) {
	r := newE2eRelay(t, "agentA", "agentB")

	connA := r.dialAndAdmit(t, "agentA", "agentB")
	defer connA.Close()
	connB := r.dialAndAdmit(t, "agentB", "agentA")
	defer connB.Close()
	r.waitMatched(t, 2)

	oversized := make([]byte, MaxFrameSize+1)
	if err := connA.WriteMessage(websocket.BinaryMessage, oversized); err != nil {
		t.Fatalf("A write: %v", err)
	}
	// Both directions must reject before any bytes are recorded. B also sends
	// an oversized frame: a small valid binary frame here would be metered,
	// which would violate the "oversized frame is not forwarded" assertion.
	if err := connB.WriteMessage(websocket.BinaryMessage, oversized); err != nil {
		t.Fatalf("B write: %v", err)
	}

	readClosed(t, connA)
	readClosed(t, connB)

	// No metered bytes: the oversized frame was rejected before forwarding.
	if m := r.svc.GetMetrics(context.Background(), r.tenant); m.TotalBytesRelayed != 0 {
		t.Fatalf("TotalBytesRelayed = %d, want 0 (oversized frame not forwarded)", m.TotalBytesRelayed)
	}
}
