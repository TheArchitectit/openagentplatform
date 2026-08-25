package mesh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ssh"
)

// defaultSessionTTL is the lifetime of a minted SSH user certificate.
const defaultSessionTTL = 15 * time.Minute

// meshSubnet is the IPv4 range operators + agents are allocated from.
// Allocation is in-memory only (this is the coordination plane); the data
// plane never needs the server to hold these, but the server must hand out
// unique addresses per session.
const meshSubnet = "10.0.0.0/24"

// KeyManager owns the WireGuard keypair generation for agents and the
// Ed25519 CA used to sign short-lived SSH user certificates. Production must
// load the CA from a secret/KMS rather than generate it in-memory; see the
// NewKeyManager comment.
type KeyManager struct {
	log *slog.Logger

	// caSigner is the Ed25519 private key that signs operator SSH certs.
	caSigner ssh.Signer
	caPubPEM string

	mu        sync.Mutex
	allocated map[string]struct{} // assigned mesh IPs (host bits only, e.g. "10.0.0.5")
	nextHost  int                 // last .N handed out; 1-2 are gateways/reserved
}

// NewKeyManager builds a KeyManager with a fresh in-memory Ed25519 CA. For
// production, the CA private key MUST be loaded from a secret store, not
// generated per process — a rotated CA invalidates every outstanding cert and
// re-keying mid-fleet is disruptive. The caller is responsible for wiring a
// persistent key before enabling the mesh in production.
func NewKeyManager(log *slog.Logger) (*KeyManager, error) {
	if log == nil {
		log = slog.Default()
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mesh: generate CA: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("mesh: ssh CA signer: %w", err)
	}
	caPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("mesh: ssh CA public: %w", err)
	}
	caPEM := string(pem.EncodeToMemory(
		&pem.Block{Type: "SSH ED25519 CA PUBLIC KEY", Bytes: caPub.Marshal()},
	))
	return &KeyManager{
		log:       log,
		caSigner: signer,
		caPubPEM:  caPEM,
		allocated: make(map[string]struct{}),
		nextHost:  2, // .1 reserved for the coordination gateway
	}, nil
}

// CAPublicKeyPEM returns the PEM-encoded SSH CA public key. Operator clients
// use this to verify the agent's acceptance and to trust the signed certs.
func (k *KeyManager) CAPublicKeyPEM() string { return k.caPubPEM }

// GenerateWireGuardKeys produces a WireGuard keypair encoded like `wg genkey`
// / `wg pubkey` (base64 of the 32-byte raw key). The private key is returned
// to the caller (agent) only at registration time; the server stores only the
// public key (via the Store).
func (k *KeyManager) GenerateWireGuardKeys() (privateB64, publicB64 string, err error) {
	priv := make([]byte, curve25519.ScalarSize)
	if _, err := rand.Read(priv); err != nil {
		return "", "", fmt.Errorf("mesh: wg rand: %w", err)
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("mesh: wg pubkey: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv),
		base64.StdEncoding.EncodeToString(pub), nil
}

// AllocateMeshIP returns the next free address from meshSubnet and reserves
// it. Callers must call ReleaseMeshIP when the session ends. The address is
// returned as a host CIDR (/32) suitable for allowed-ips.
func (k *KeyManager) AllocateMeshIP() (string, error) {
	_, subnet, err := net.ParseCIDR(meshSubnet)
	if err != nil {
		return "", fmt.Errorf("mesh: bad subnet config: %w", err)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for i := k.nextHost + 1; i < 255; i++ {
		ip := append(net.IP{}, subnet.IP...)
		ip[3] = byte(i)
		host := ip.String()
		if _, taken := k.allocated[host]; taken {
			continue
		}
		k.allocated[host] = struct{}{}
		k.nextHost = i
		return host + "/32", nil
	}
	return "", errors.New("mesh: address pool exhausted")
}

// ReleaseMeshIP frees a previously allocated address.
func (k *KeyManager) ReleaseMeshIP(hostCIDR string) {
	host := net.ParseIP(hostCIDR)
	if host == nil {
		// Maybe it carried a /32 suffix.
		if _, ipNet, err := net.ParseCIDR(hostCIDR); err == nil {
			host = ipNet.IP
		}
	}
	if host == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.allocated, host.String())
}

// SignSSHCert mints a short-lived SSH user certificate the agent's SSH server
// will accept. The certificate is bound to a single agent principal
// ("oap:<agentID>") and carries the operator's identity in KeyId. The
// Permissions allow only port forwarding (no shell, no PTY, no agent
// forwarding) — defense in depth on top of WireGuard. The operator generates
// and keeps its own SSH keypair; only opPub (the public half) is certified,
// so the signed cert is useless without the operator's private key.
func (k *KeyManager) SignSSHCert(opPub ssh.PublicKey, operatorID, agentID string, ttl time.Duration) ([]byte, error) {
	if opPub == nil {
		return nil, errors.New("mesh: operator public key required")
	}
	if operatorID == "" || agentID == "" {
		return nil, errors.New("mesh: operatorID and agentID required")
	}
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	now := time.Now()
	cert := &ssh.Certificate{
		CertType:        ssh.UserCert,
		Key:             opPub,
		KeyId:           operatorID,
		ValidPrincipals: []string{"oap:" + agentID},
		ValidAfter:      uint64(now.Add(-1 * time.Minute).Unix()),
		ValidBefore:     uint64(now.Add(ttl).Unix()),
		Permissions: ssh.Permissions{
			// Port forwarding allowed; no shell, no PTY, no agent.
			// The explicit absence of permit-shell / permit-pty in
			// Extensions restricts the cert to forwarding only.
			Extensions: map[string]string{
				"permit-port-forwarding": "",
			},
		},
	}
	if err := cert.SignCert(rand.Reader, k.caSigner); err != nil {
		return nil, fmt.Errorf("mesh: sign cert: %w", err)
	}
	return ssh.MarshalAuthorizedKey(cert), nil
}
