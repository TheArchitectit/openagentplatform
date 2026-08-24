package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"log/slog"
)

func testServer(t *testing.T) *AuthorizationServer {
	t.Helper()
	return NewAuthorizationServer(
		"https://oap.test",
		"https://api.oap.test",
		"https://oap.test/.well-known/oauth-protected-resource/register",
		nil,
		slog.Default(),
	)
}

func registerTestClient(t *testing.T, s *AuthorizationServer) *ClientRegistrationResponse {
	t.Helper()
	resp, err := s.RegisterClient(context.Background(), ClientMetadata{
		RedirectURIs:            []string{"https://app.test/callback"},
		TokenEndpointAuthMethod: "client_secret_basic",
		ClientName:              "test-app",
		Scope:                   "task:* secret:read",
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	return resp
}

func TestRegisterClient(t *testing.T) {
	s := testServer(t)
	resp := registerTestClient(t, s)

	if resp.ClientID == "" {
		t.Fatal("expected non-empty client_id")
	}
	if resp.ClientSecret == "" {
		t.Fatal("expected non-empty client_secret")
	}
	if resp.ClientIDIssuedAt == 0 {
		t.Fatal("expected non-zero client_id_issued_at")
	}
	if resp.RedirectURIs[0] != "https://app.test/callback" {
		t.Fatal("unexpected redirect_uri")
	}
}

func TestRegisterClient_NoRedirectURI(t *testing.T) {
	s := testServer(t)
	_, err := s.RegisterClient(context.Background(), ClientMetadata{
		TokenEndpointAuthMethod: "client_secret_basic",
	})
	if err == nil {
		t.Fatal("expected error for no redirect_uri")
	}
}

func TestRegisterClient_Duplicate(t *testing.T) {
	s := testServer(t)
	client1, _ := s.RegisterClient(context.Background(), ClientMetadata{
		RedirectURIs:            []string{"https://a.test/cb"},
		TokenEndpointAuthMethod: "client_secret_basic",
	})
	_, err := s.RegisterClient(context.Background(), ClientMetadata{
		RedirectURIs:            []string{"https://b.test/cb"},
		TokenEndpointAuthMethod: "client_secret_basic",
	})
	// Should succeed (different redirect URIs are OK for different clients).
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = client1
}

// pkceS256 computes the S256 code_challenge from a code_verifier.
func pkceS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
