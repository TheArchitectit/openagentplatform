package relay

// E.2 acceptance (RELAY-06 §7.4 E.2): the relay is a private relay — not a
// general open forwarder. Only issued, entitled clients are admitted. These
// tests prove three failure modes over the full WSS admission path:
//
//  1. A client without an mTLS certificate is rejected at the TLS layer.
//  2. A client presenting a certificate from an untrusted CA is rejected at
//     the TLS layer.
//  3. A client with a valid certificate AND token, but no entitlement grant,
//     is rejected at admission with no connection registered.
//
// The third test also closes the admission question the spec still lists as a
// known limitation ("entitlement-gated admission NOT yet wired"): it proves
// CheckEntitlement is enforced in handleWSS before any connection exists.

import (
	"context"
	"crypto/tls"
	"net/url"
	"testing"
)

// TestE2_PrivateRelay_RejectsMissingClientCert proves mTLS is mandatory: a
// client dialing without a certificate is turned away at the TLS handshake
// (ClientAuth=RequireAndVerifyClientCert) before the relay registers anything.
func TestE2_PrivateRelay_RejectsMissingClientCert(t *testing.T) {
	r := newE2eRelay(t, "agentA", "agentB")

	conn, err := r.dialWSS(&tls.Config{RootCAs: r.ca.pool}) // no client certificate
	if err == nil {
		conn.Close()
		t.Fatal("expected TLS rejection for missing client cert, got connection")
	}

	if got := r.svc.ListConnections(context.Background(), r.tenant); len(got) != 0 {
		t.Fatalf("registered %d connections, want 0", len(got))
	}
}

// TestE2_PrivateRelay_RejectsUnknownCA proves the trust anchor is the platform
// CA: a certificate signed by a different CA cannot pass ClientAuth, even with
// the correct agent principal and tenant SAN.
func TestE2_PrivateRelay_RejectsUnknownCA(t *testing.T) {
	r := newE2eRelay(t, "agentA", "agentB")

	// A rogue CA issues a visually-identical agentA cert.
	rogue := newE2eCA(t)
	u, err := url.Parse("oap:tenant:" + r.tenant)
	if err != nil {
		t.Fatal(err)
	}
	rogueCert := rogue.issue(t, "agentA", []*url.URL{u}, nil)

	conn, err := r.dialWSS(&tls.Config{
		RootCAs:      r.ca.pool,                    // trust the real server CA...
		Certificates: []tls.Certificate{rogueCert}, // ...but present a rogue client cert
	})
	if err == nil {
		conn.Close()
		t.Fatal("expected TLS rejection for certificate from an untrusted CA, got connection")
	}

	if got := r.svc.ListConnections(context.Background(), r.tenant); len(got) != 0 {
		t.Fatalf("registered %d connections, want 0", len(got))
	}
}

// TestE2_PrivateRelay_RejectsNonEntitled proves entitlement-gated admission:
// an issued, authenticated, valid-token client with NO source→target grant is
// closed before any connection is established. Default-deny, not default-allow.
func TestE2_PrivateRelay_RejectsNonEntitled(t *testing.T) {
	// No entitlements at all: every pair is denied (default-deny).
	r := newE2eRelayWithEntitlements(t, nil, "agentA", "agentB")

	conn := r.dialAndAdmit(t, "agentA", "agentB")
	defer conn.Close()

	// The relay closes the leg at the entitlement gate.
	readClosed(t, conn)

	// No connection was ever registered — the rejection happens before Admit.
	if got := r.svc.ListConnections(context.Background(), r.tenant); len(got) != 0 {
		t.Fatalf("registered %d connections, want 0 (non-entitled client must not register)", len(got))
	}
}
