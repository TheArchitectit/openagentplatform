package mesh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ssh"
)

func TestGenerateWireGuardKeys(t *testing.T) {
	km, err := NewKeyManager(nil)
	if err != nil {
		t.Fatalf("NewKeyManager: %v", err)
	}
	privB64, pubB64, err := km.GenerateWireGuardKeys()
	if err != nil {
		t.Fatalf("GenerateWireGuardKeys: %v", err)
	}
	priv, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		t.Fatalf("decode private: %v", err)
	}
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		t.Fatalf("decode public: %v", err)
	}
	if len(priv) != curve25519.ScalarSize {
		t.Errorf("private key length = %d, want %d", len(priv), curve25519.ScalarSize)
	}
	if len(pub) != curve25519.PointSize {
		t.Errorf("public key length = %d, want %d", len(pub), curve25519.PointSize)
	}
	// Verify the derived public matches the private via X25519.
	wantPub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		t.Fatalf("X25519 derive: %v", err)
	}
	if string(wantPub) != string(pub) {
		t.Errorf("public key mismatch: derived != returned")
	}
}

func TestAllocateMeshIP(t *testing.T) {
	km, _ := NewKeyManager(nil)
	ip1, err := km.AllocateMeshIP()
	if err != nil {
		t.Fatalf("AllocateMeshIP: %v", err)
	}
	if ip1 == "" {
		t.Fatal("expected non-empty IP")
	}
	ip2, err := km.AllocateMeshIP()
	if err != nil {
		t.Fatalf("AllocateMeshIP 2: %v", err)
	}
	if ip1 == ip2 {
		t.Errorf("allocated same IP twice: %s", ip1)
	}
	km.ReleaseMeshIP(ip1)
	ip3, err := km.AllocateMeshIP()
	if err != nil {
		t.Fatalf("AllocateMeshIP 3: %v", err)
	}
	if ip3 != ip1 {
		t.Logf("released IP %s reused as %s (acceptable; allocator picks next free)", ip1, ip3)
	}
}

func TestSignSSHCert(t *testing.T) {
	km, _ := NewKeyManager(nil)
	opPub, opSigner, err := generateTestSSHKeypair()
	if err != nil {
		t.Fatalf("generate test ssh key: %v", err)
	}
	certBytes, err := km.SignSSHCert(opPub, "op-123", "agent-456", 15*time.Minute)
	if err != nil {
		t.Fatalf("SignSSHCert: %v", err)
	}
	// Parse the authorized-key line back into a cert.
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(certBytes)
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}
	cert, ok := pubKey.(*ssh.Certificate)
	if !ok {
		t.Fatal("expected *ssh.Certificate")
	}
	if cert.CertType != ssh.UserCert {
		t.Errorf("CertType = %d, want UserCert", cert.CertType)
	}
	if cert.KeyId != "op-123" {
		t.Errorf("KeyId = %q, want %q", cert.KeyId, "op-123")
	}
	found := false
	for _, p := range cert.ValidPrincipals {
		if p == "oap:agent-456" {
			found = true
		}
	}
	if !found {
		t.Errorf("ValidPrincipals = %v, want oap:agent-456", cert.ValidPrincipals)
	}
	// Verify the cert is usable: create a cert signer from the operator key.
	_, err = ssh.NewCertSigner(cert, opSigner)
	if err != nil {
		t.Fatalf("NewCertSigner: %v", err)
	}
}

func TestCAPublicKeyPEM(t *testing.T) {
	km, _ := NewKeyManager(nil)
	pem := km.CAPublicKeyPEM()
	if pem == "" {
		t.Fatal("expected non-empty CA PEM")
	}
}

// generateTestSSHKeypair returns an ed25519 SSH public key + signer for tests.
func generateTestSSHKeypair() (ssh.PublicKey, ssh.Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return sshPub, signer, nil
}
