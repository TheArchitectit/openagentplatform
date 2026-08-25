package mesh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testCA creates an ed25519 CA keypair, returns the ssh.Signer (private half)
// and the PEM-encoded public key for use in tests.
func testCA(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen CA key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("CA signer: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("CA pubkey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "SSH ED25519 CA PUBLIC KEY",
		Bytes: sshPub.Marshal(),
	})
	return signer, string(pemBytes)
}

// mintCert builds an SSH user certificate signed by the CA with the given
// parameters. It returns the *ssh.Certificate ready to be used in tests.
func mintCert(t *testing.T, ca ssh.Signer, agentID string, validAfter, validBefore time.Time, certType uint32) *ssh.Certificate {
	t.Helper()
	opPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("op key: %v", err)
	}
	opSSHPub, err := ssh.NewPublicKey(opPub)
	if err != nil {
		t.Fatalf("op ssh pub: %v", err)
	}
	cert := &ssh.Certificate{
		CertType:        certType,
		Key:             opSSHPub,
		KeyId:           "test-operator",
		ValidPrincipals: []string{"oap:" + agentID},
		ValidAfter:      uint64(validAfter.Unix()),
		ValidBefore:     uint64(validBefore.Unix()),
		Permissions: ssh.Permissions{
			Extensions: map[string]string{
				"permit-port-forwarding": "",
			},
		},
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatalf("sign cert: %v", err)
	}
	return cert
}

// asPublicKey returns the cert as an ssh.PublicKey (so we can pass it to
// VerifyClientCert the same way the SSH library does).
func asPublicKey(t *testing.T, cert *ssh.Certificate) ssh.PublicKey {
	t.Helper()
	pk, err := ssh.ParsePublicKey(cert.Marshal())
	if err != nil {
		t.Fatalf("parse cert pubkey: %v", err)
	}
	return pk
}

func TestVerifyClientCert_AcceptValid(t *testing.T) {
	ca, caPEM := testCA(t)
	cert := mintCert(t, ca, "test-agent-id",
		time.Now().Add(-1*time.Minute), time.Now().Add(15*time.Minute), ssh.UserCert)
	caPub, err := parseCAPublicKey(caPEM)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}

	perms, err := VerifyClientCert(asPublicKey(t, cert), caPub, "test-agent-id")
	if err != nil {
		t.Fatalf("expected accept, got error: %v", err)
	}
	if perms == nil {
		t.Fatal("expected non-nil permissions")
	}
	if perms.Extensions["agent_id"] != "test-agent-id" {
		t.Errorf("agent_id ext = %q, want test-agent-id", perms.Extensions["agent_id"])
	}
}

func TestVerifyClientCert_RejectWrongAgent(t *testing.T) {
	ca, caPEM := testCA(t)
	cert := mintCert(t, ca, "other-agent",
		time.Now().Add(-1*time.Minute), time.Now().Add(15*time.Minute), ssh.UserCert)
	caPub, _ := parseCAPublicKey(caPEM)

	_, err := VerifyClientCert(asPublicKey(t, cert), caPub, "test-agent-id")
	if err != ErrPrincipalMismatch {
		t.Fatalf("got err=%v, want ErrPrincipalMismatch", err)
	}
}

func TestVerifyClientCert_RejectExpired(t *testing.T) {
	ca, caPEM := testCA(t)
	cert := mintCert(t, ca, "test-agent-id",
		time.Now().Add(-2*time.Hour), time.Now().Add(-1*time.Hour), ssh.UserCert)
	caPub, _ := parseCAPublicKey(caPEM)

	_, err := VerifyClientCert(asPublicKey(t, cert), caPub, "test-agent-id")
	if err != ErrCertExpired {
		t.Fatalf("got err=%v, want ErrCertExpired", err)
	}
}

func TestVerifyClientCert_RejectNotYetValid(t *testing.T) {
	ca, caPEM := testCA(t)
	cert := mintCert(t, ca, "test-agent-id",
		time.Now().Add(1*time.Hour), time.Now().Add(2*time.Hour), ssh.UserCert)
	caPub, _ := parseCAPublicKey(caPEM)

	_, err := VerifyClientCert(asPublicKey(t, cert), caPub, "test-agent-id")
	if err != ErrCertExpired {
		t.Fatalf("got err=%v, want ErrCertExpired", err)
	}
}

func TestVerifyClientCert_RejectHostCert(t *testing.T) {
	ca, caPEM := testCA(t)
	cert := mintCert(t, ca, "test-agent-id",
		time.Now().Add(-1*time.Minute), time.Now().Add(15*time.Minute), ssh.HostCert)
	caPub, _ := parseCAPublicKey(caPEM)

	_, err := VerifyClientCert(asPublicKey(t, cert), caPub, "test-agent-id")
	if err != ErrWrongCertType {
		t.Fatalf("got err=%v, want ErrWrongCertType", err)
	}
}

func TestVerifyClientCert_RejectPlainKey(t *testing.T) {
	_, caPEM := testCA(t)
	caPub, _ := parseCAPublicKey(caPEM)

	// A plain (non-cert) ed25519 key presented as the public key.
	_, opPriv, _ := ed25519.GenerateKey(rand.Reader)
	opPubSSH, err := ssh.NewPublicKey(opPriv.Public())
	if err != nil {
		t.Fatalf("op pub: %v", err)
	}

	_, err = VerifyClientCert(opPubSSH, caPub, "test-agent-id")
	if err != ErrNotACertificate {
		t.Fatalf("got err=%v, want ErrNotACertificate", err)
	}
}

func TestVerifyClientCert_RejectWrongCA(t *testing.T) {
	ca1, _ := testCA(t)
	_, ca2PEM := testCA(t) // different CA
	ca2Pub, _ := parseCAPublicKey(ca2PEM)

	// Cert signed by CA1, but we verify against CA2.
	cert := mintCert(t, ca1, "test-agent-id",
		time.Now().Add(-1*time.Minute), time.Now().Add(15*time.Minute), ssh.UserCert)

	_, err := VerifyClientCert(asPublicKey(t, cert), ca2Pub, "test-agent-id")
	if err != ErrCertNotSignedByCA {
		t.Fatalf("got err=%v, want ErrCertNotSignedByCA", err)
	}
}
