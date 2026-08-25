package mesh

import (
	"crypto/ed25519"
	"testing"
)

func TestSignAndVerifyRelease(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	reg := NewReleaseRegistry(nil, nil)

	binary := []byte("fake-agent-binary-v1.2.3")
	sha, sig, err := reg.SignRelease(binary, priv)
	if err != nil {
		t.Fatalf("SignRelease: %v", err)
	}
	if sha == "" || sig == "" {
		t.Fatal("SignRelease returned empty sha or sig")
	}
	if !reg.VerifyRelease(binary, sha, sig, pub) {
		t.Fatal("VerifyRelease should accept valid signature")
	}
}

func TestVerifyRelease_ShaMismatch(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	reg := NewReleaseRegistry(nil, nil)

	binary := []byte("agent-binary")
	_, sig, _ := reg.SignRelease(binary, priv)

	// Wrong sha256
	if reg.VerifyRelease(binary, "deadbeef", sig, pub) {
		t.Fatal("VerifyRelease should reject sha256 mismatch")
	}
}

func TestVerifyRelease_BadSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	reg := NewReleaseRegistry(nil, nil)

	binary := []byte("agent-binary")
	sha, _, _ := reg.SignRelease(binary, priv)

	// Wrong signature
	if reg.VerifyRelease(binary, sha, "not-valid-base64!!!", pub) {
		t.Fatal("VerifyRelease should reject bad base64 signature")
	}
}

func TestVerifyRelease_WrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	reg := NewReleaseRegistry(nil, nil)

	binary := []byte("agent-binary")
	sha, sig, _ := reg.SignRelease(binary, priv)

	// Different public key
	if reg.VerifyRelease(binary, sha, sig, wrongPub) {
		t.Fatal("VerifyRelease should reject signature from wrong key")
	}
}

func TestVerifyRelease_TamperedBinary(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	reg := NewReleaseRegistry(nil, nil)

	binary := []byte("agent-binary")
	sha, sig, _ := reg.SignRelease(binary, priv)

	// Tamper with binary after signing
	tampered := []byte("agent-binary-TAMPERED")
	if reg.VerifyRelease(tampered, sha, sig, pub) {
		t.Fatal("VerifyRelease should reject tampered binary")
	}
}
