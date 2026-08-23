package auth

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func testIssuer(t *testing.T) *TokenIssuer {
	t.Helper()
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	rev := NewRevocationList()
	issuer, err := NewTokenIssuer(priv, "key-1", "https://oap.test", rev)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	return issuer
}

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("private key size: got %d", len(priv))
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("public key size: got %d", len(pub))
	}
}

func TestPEMRoundTrip(t *testing.T) {
	priv, _, _ := GenerateKeyPair()
	pemData, err := EncodePrivateKeyPEM(priv)
	if err != nil {
		t.Fatalf("EncodePrivateKeyPEM: %v", err)
	}
	decoded, err := DecodePrivateKeyPEM(pemData)
	if err != nil {
		t.Fatalf("DecodePrivateKeyPEM: %v", err)
	}
	if len(decoded) != len(priv) {
		t.Fatalf("decoded key size mismatch")
	}
}

func TestDecodePrivateKeyPEM_Invalid(t *testing.T) {
	_, err := DecodePrivateKeyPEM([]byte("not-a-pem"))
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestNewTokenIssuer_NilKey(t *testing.T) {
	rev := NewRevocationList()
	_, err := NewTokenIssuer(nil, "k", "iss", rev)
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestNewTokenIssuer_NilRevocation(t *testing.T) {
	priv, _, _ := GenerateKeyPair()
	_, err := NewTokenIssuer(priv, "k", "iss", nil)
	if err == nil {
		t.Fatal("expected error for nil revocation list")
	}
}

func TestIssueAndVerify(t *testing.T) {
	iss := testIssuer(t)

	claims := TokenClaims{
		Subject:   "agent-1",
		Audience:  "gateway",
		Scopes:    []string{"task:create", "task:read"},
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := iss.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Verify.
	verified, err := iss.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.Issuer != "https://oap.test" {
		t.Fatalf("expected issuer https://oap.test, got %s", verified.Issuer)
	}
	if verified.Subject != "agent-1" {
		t.Fatalf("expected subject agent-1, got %s", verified.Subject)
	}
	if verified.ExpiresAt <= time.Now().Unix() {
		t.Fatal("expected future expiry")
	}
}

func TestVerify_Expired(t *testing.T) {
	// Issue a token with a very short TTL and let it expire.
	iss := testIssuer(t)

	claims := TokenClaims{
		Subject:   "agent-1",
		ExpiresAt: time.Now().Add(1 * time.Second).Unix(),
	}

	token, err := iss.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Wait for token to expire.
	time.Sleep(1100 * time.Millisecond)

	_, err = iss.Verify(token)
	if err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestVerify_InvalidSignature(t *testing.T) {
	iss1 := testIssuer(t)
	iss2 := testIssuer(t) // different key pair

	claims := TokenClaims{
		Subject:   "agent-1",
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	token, _ := iss1.Issue(claims)
	_, err := iss2.Verify(token)
	if err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerify_InvalidFormat(t *testing.T) {
	iss := testIssuer(t)
	_, err := iss.Verify("not-a-jwt")
	if err != ErrInvalidTokenFormat {
		t.Fatalf("expected ErrInvalidTokenFormat, got %v", err)
	}
}

func TestVerify_Revoked(t *testing.T) {
	iss := testIssuer(t)

	claims := TokenClaims{
		Subject:   "agent-1",
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	token, _ := iss.Issue(claims)
	verified, _ := iss.Verify(token)

	iss.Revoke(verified.JTI, 1*time.Hour)
	_, err := iss.Verify(token)
	if err != ErrTokenRevoked {
		t.Fatalf("expected ErrTokenRevoked, got %v", err)
	}
}

func TestExchange_Downscope(t *testing.T) {
	iss := testIssuer(t)

	parent := TokenClaims{
		Subject:   "parent-agent",
		Audience:  "child-agent",
		Scopes:    []string{"task:create", "task:read", "task:delete"},
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	parentToken, _ := iss.Issue(parent)

	childToken, err := iss.Exchange(parentToken, []string{"task:read"})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	child, err := iss.Verify(childToken)
	if err != nil {
		t.Fatalf("Verify child: %v", err)
	}
	if len(child.Scopes) != 1 || child.Scopes[0] != "task:read" {
		t.Fatalf("expected [task:read], got %v", child.Scopes)
	}
	if len(child.DelegationChain) != 1 {
		t.Fatalf("expected 1 delegation entry, got %d", len(child.DelegationChain))
	}
}

func TestExchange_ExceedsParentScope(t *testing.T) {
	iss := testIssuer(t)

	parent := TokenClaims{
		Subject:   "parent",
		Audience:  "child",
		Scopes:    []string{"task:read"},
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	parentToken, _ := iss.Issue(parent)
	_, err := iss.Exchange(parentToken, []string{"task:delete"})
	if err == nil {
		t.Fatal("expected error for scope escalation")
	}
}

func TestExchange_MaxDepth(t *testing.T) {
	iss := testIssuer(t)

	// Build a chain of MaxDelegationDepth.
	token := iss.mustIssue(t, TokenClaims{
		Subject:   "root",
		Audience:  "a1",
		Scopes:    []string{"*"},
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	})

	for i := 0; i < MaxDelegationDepth; i++ {
		var err error
		token, err = iss.Exchange(token, []string{"*"})
		if err != nil {
			t.Fatalf("Exchange hop %d: %v", i, err)
		}
	}

	// Next exchange should fail.
	_, err := iss.Exchange(token, []string{"*"})
	if err == nil {
		t.Fatal("expected error for max delegation depth")
	}
}

func TestIssue_RequiresExpiry(t *testing.T) {
	iss := testIssuer(t)
	_, err := iss.Issue(TokenClaims{Subject: "x"})
	if err == nil {
		t.Fatal("expected error for missing ExpiresAt")
	}
}

func TestIssue_PastExpiry(t *testing.T) {
	iss := testIssuer(t)
	_, err := iss.Issue(TokenClaims{
		Subject:   "x",
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})
	if err == nil {
		t.Fatal("expected error for past ExpiresAt")
	}
}

func TestPublicKey(t *testing.T) {
	iss := testIssuer(t)
	pub := iss.PublicKey()
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("unexpected public key size: %d", len(pub))
	}
}

func TestKeyID(t *testing.T) {
	iss := testIssuer(t)
	if iss.KeyID() != "key-1" {
		t.Fatalf("expected key-1, got %s", iss.KeyID())
	}
}

func (iss *TokenIssuer) mustIssue(t *testing.T, claims TokenClaims) string {
	t.Helper()
	token, err := iss.Issue(claims)
	if err != nil {
		t.Fatalf("mustIssue: %v", err)
	}
	return token
}
