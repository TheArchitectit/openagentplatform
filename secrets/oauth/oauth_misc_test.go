package oauth

import (
	"testing"
)

func TestNonce_IssueConsume(t *testing.T) {
	s := testServer(t)

	nonce, err := s.IssueNonce()
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	if nonce == "" {
		t.Fatal("expected non-empty nonce")
	}

	// First consume succeeds.
	if !s.ConsumeNonce(nonce) {
		t.Fatal("expected valid nonce")
	}

	// Second consume fails (one-time use).
	if s.ConsumeNonce(nonce) {
		t.Fatal("expected consumed nonce to be invalid")
	}
}

func TestNonce_Invalid(t *testing.T) {
	s := testServer(t)
	if s.ConsumeNonce("nonexistent") {
		t.Fatal("expected invalid nonce")
	}
}

func TestVerifyClientSecret(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	// Correct secret.
	err := s.VerifyClientSecret(client.ClientID, client.ClientSecret)
	if err != nil {
		t.Fatalf("VerifyClientSecret: %v", err)
	}

	// Wrong secret.
	err = s.VerifyClientSecret(client.ClientID, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}

	// Unknown client.
	err = s.VerifyClientSecret("nonexistent", "x")
	if err == nil {
		t.Fatal("expected error for unknown client")
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	m := &ProtectedResourceMetadata{
		Resource:             "https://api.oap.test",
		AuthorizationServers: []string{"https://oap.test"},
		DPoPSupported:        true,
	}
	data, err := MarshalProtectedResourceMetadata(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}
}

func TestJWKThumbprint(t *testing.T) {
	jwk := []byte(`{"kty":"OKP","crv":"Ed25519"}`)
	tp, err := JWKThumbprint(jwk)
	if err != nil {
		t.Fatalf("JWKThumbprint: %v", err)
	}
	if tp == "" {
		t.Fatal("expected non-empty thumbprint")
	}
	// Deterministic.
	tp2, _ := JWKThumbprint(jwk)
	if tp != tp2 {
		t.Fatal("expected deterministic thumbprint")
	}
}

func TestRedirectURIMatches(t *testing.T) {
	if !redirectURIMatches([]string{"https://a.test/cb", "https://b.test/cb"}, "https://a.test/cb") {
		t.Fatal("expected match")
	}
	if redirectURIMatches([]string{"https://a.test/cb"}, "https://evil.test/cb") {
		t.Fatal("expected no match")
	}
}
