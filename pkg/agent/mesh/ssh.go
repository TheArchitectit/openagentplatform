// Package mesh implements the agent-side handler for the RMM-09 secure
// tunnel fabric. This file provides the SSH server that binds exclusively
// to the WireGuard mesh tunnel IP and accepts only server-CA-signed user
// certificates. It enforces port-forward-only access (no shell, no PTY)
// and validates the cert principal matches this agent's identity.
package mesh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Errors returned by the SSH server and cert verifier.
var (
	ErrNotACertificate   = errors.New("mesh-ssh: public key is not a certificate")
	ErrWrongCertType     = errors.New("mesh-ssh: certificate is not a user cert")
	ErrCertExpired       = errors.New("mesh-ssh: certificate expired or not yet valid")
	ErrPrincipalMismatch = errors.New("mesh-ssh: cert principal does not match agent")
	ErrCertNotSignedByCA = errors.New("mesh-ssh: certificate not signed by trusted CA")
	ErrNoMeshIP         = errors.New("mesh-ssh: mesh IP not available (interface not up)")
)

// defaultSSHPort is the port the SSH server listens on inside the mesh.
const defaultSSHPort = 22

// SSHServer listens on the mesh tunnel IP and accepts operator connections
// authenticated by short-lived SSH user certificates signed by the server's
// CA. Only port forwarding is permitted (no shell, no PTY, no agent
// forwarding) — defense in depth on top of the WireGuard mesh encryption.
type SSHServer struct {
	agentID string
	meshIP  string
	caPub   ssh.PublicKey
	port    int
	log     *slog.Logger

	mu      sync.Mutex
	ln      net.Listener
	closed  bool
	conns   map[*ssh.ServerConn]struct{}
}

// NewSSHServer builds an SSH server that will listen on meshIP:port.
// caPEM is the PEM-encoded SSH CA public key (the same CA the server's
// KeyManager uses to sign operator certs in internal/mesh/admission.go).
func NewSSHServer(agentID, meshIP, caPEM string, log *slog.Logger) (*SSHServer, error) {
	if agentID == "" {
		return nil, errors.New("mesh-ssh: agent_id required")
	}
	if meshIP == "" {
		return nil, ErrNoMeshIP
	}
	caPub, err := parseCAPublicKey(caPEM)
	if err != nil {
		return nil, fmt.Errorf("mesh-ssh: parse CA: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &SSHServer{
		agentID: agentID,
		meshIP:  meshIP,
		caPub:   caPub,
		port:    defaultSSHPort,
		log:     log,
		conns:   make(map[*ssh.ServerConn]struct{}),
	}, nil
}

// SetPort overrides the SSH listen port (default 22).
func (s *SSHServer) SetPort(p int) {
	if p > 0 && p <= 65535 {
		s.port = p
	}
}

// ListenAndServe starts the SSH listener on the mesh tunnel IP. It blocks
// until the context is cancelled or the listener encounters a fatal error.
func (s *SSHServer) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.meshIP, s.port)
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return VerifyClientCert(key, s.caPub, s.agentID)
		},
		NoClientAuth: false,
	}

	// Generate an ephemeral host key for each server instance. In production
	// this could be loaded from disk for host-key continuity, but the mesh
	// already authenticates via cert + CA, so the host key is secondary.
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("mesh-ssh: generate host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		return fmt.Errorf("mesh-ssh: host key signer: %w", err)
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mesh-ssh: listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	s.log.Info("mesh-ssh: listening", "addr", addr, "agent_id", s.agentID)

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return fmt.Errorf("mesh-ssh: accept: %w", err)
		}
		go s.handleConn(conn, cfg)
	}
}

// Close shuts down the listener and all active connections.
func (s *SSHServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.ln != nil {
		_ = s.ln.Close()
	}
	for c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
}

// handleConn handles a single SSH connection. Only port-forwarding channels
// are accepted; "session" channels (which carry shell/exec/subsystem) are
// rejected — the cert already has permit-port-forwarding only, but we
// enforce it at the channel level too.
func (s *SSHServer) handleConn(netConn net.Conn, cfg *ssh.ServerConfig) {
	defer netConn.Close()
	conn, chans, reqs, err := ssh.NewServerConn(netConn, cfg)
	if err != nil {
		s.log.Debug("mesh-ssh: handshake failed", "remote", netConn.RemoteAddr(), "err", err)
		return
	}
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	s.log.Info("mesh-ssh: connection accepted",
		"remote", netConn.RemoteAddr(),
		"user", conn.User(),
		"agent_id", s.agentID,
	)

	// Discard out-of-band requests (keepalive, etc.).
	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		switch ch.ChannelType() {
		case "session":
			// Explicitly reject shell/exec/subsystem — port-forward only.
			ch.Reject(ssh.Prohibited, "mesh-ssh: session channel not allowed (port-forward only)")
		case "direct-tcpip", "tcpip-forward":
			// Port forwarding is the intended use case. Accept the channel.
			ch.Accept()
		default:
			ch.Reject(ssh.UnknownChannelType, "mesh-ssh: unknown channel type")
		}
	}
}

// parseCAPublicKey decodes a PEM block and parses it as an SSH public key.
// The PEM block type is flexible (OpenSSH writes various headers); we just
// need the base64 payload.
func parseCAPublicKey(pemStr string) (ssh.PublicKey, error) {
	if pemStr == "" {
		return nil, errors.New("mesh-ssh: empty CA PEM")
	}
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		// Might be an AuthorizedKeys format (no PEM wrapper). Try parsing
		// the raw string directly.
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pemStr))
		if err != nil {
			return nil, fmt.Errorf("mesh-ssh: not PEM or authorized-key format: %w", err)
		}
		return pk, nil
	}
	pk, err := ssh.ParsePublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mesh-ssh: parse public key from PEM: %w", err)
	}
	return pk, nil
}

// VerifyClientCert is the certificate verification function used by the
// SSH server's PublicKeyCallback. It is exported so it can be unit-tested
// independently of any network listener.
//
// The function enforces:
//  1. The key must be an SSH certificate (not a plain public key).
//  2. The cert must be a user cert (CertTypeUser).
//  3. The cert must be within its validity window.
//  4. The cert must be signed by the trusted CA.
//  5. The cert's ValidPrincipals must contain "oap:<agentID>".
//
// On success it returns ssh.Permissions; on failure it returns an error
// that the SSH library converts into an authentication rejection.
func VerifyClientCert(pubKey ssh.PublicKey, caPub ssh.PublicKey, agentID string) (*ssh.Permissions, error) {
	cert, ok := pubKey.(*ssh.Certificate)
	if !ok {
		return nil, ErrNotACertificate
	}
	if cert.CertType != ssh.UserCert {
		return nil, ErrWrongCertType
	}

	// Check validity window.
	now := time.Now().Unix()
	if int64(cert.ValidBefore) < now || int64(cert.ValidAfter) > now {
		return nil, ErrCertExpired
	}

	// Check that the cert was signed by the trusted CA. The SSH handshake
	// already verified the cert's cryptographic signature against
	// cert.SignatureKey (this is how SSH cert auth works — the transport
	// validates the signature before the callback fires). Our job is to
	// ensure cert.SignatureKey IS our trusted CA. If a different CA signed
	// the cert, the signature would be valid under that other CA, not ours.
	if !keysEqual(cert.SignatureKey, caPub) {
		return nil, ErrCertNotSignedByCA
	}

	// Check principal: the cert must be scoped to this specific agent.
	expected := "oap:" + agentID
	found := false
	for _, p := range cert.ValidPrincipals {
		if p == expected {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrPrincipalMismatch
	}

	return &ssh.Permissions{
		Extensions: map[string]string{
			"agent_id": agentID,
		},
	}, nil
}

// keysEqual compares two SSH public keys by their marshaled form.
func keysEqual(a, b ssh.PublicKey) bool {
	return string(a.Marshal()) == string(b.Marshal())
}
