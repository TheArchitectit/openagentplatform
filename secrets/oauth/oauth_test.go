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

func TestAuthorizeCode(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	code, err := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.test/callback",
		Scope:               "task:create",
		State:               "abc123",
		CodeChallenge:       "test-challenge",
		CodeChallengeMethod: PKCES256,
	})
	if err != nil {
		t.Fatalf("AuthorizeCode: %v", err)
	}
	if code.Code == "" {
		t.Fatal("expected non-empty code")
	}
	if code.ClientID != client.ClientID {
		t.Fatal("client_id mismatch")
	}
}

func TestAuthorizeCode_NoClient(t *testing.T) {
	s := testServer(t)
	_, err := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:      "nonexistent",
		RedirectURI:   "https://app.test/callback",
		CodeChallenge: "x",
	})
	if err != ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound, got %v", err)
	}
}

func TestAuthorizeCode_NoChallenge(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)
	_, err := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:    client.ClientID,
		RedirectURI: "https://app.test/callback",
	})
	if err != ErrInvalidCodeChallenge {
		t.Fatalf("expected ErrInvalidCodeChallenge, got %v", err)
	}
}

func TestAuthorizeCode_WrongRedirect(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)
	_, err := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:      client.ClientID,
		RedirectURI:   "https://evil.test/callback",
		CodeChallenge: "x",
	})
	if err != ErrInvalidRedirectURI {
		t.Fatalf("expected ErrInvalidRedirectURI, got %v", err)
	}
}

// pkceS256 computes the S256 code_challenge from a code_verifier.
func pkceS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func TestExchangeCode_Success(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := pkceS256(verifier)

	code, _ := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.test/callback",
		Scope:               "task:create",
		CodeChallenge:       challenge,
		CodeChallengeMethod: PKCES256,
	})

	resp, err := s.ExchangeCode(context.Background(), TokenRequest{
		GrantType:    "authorization_code",
		Code:         code.Code,
		RedirectURI:  "https://app.test/callback",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		CodeVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected non-empty refresh_token")
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("expected Bearer, got %s", resp.TokenType)
	}
	if resp.Scope != "task:create" {
		t.Fatalf("expected task:create, got %s", resp.Scope)
	}
}

func TestExchangeCode_WrongVerifier(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	verifier := "correct-verifier"
	challenge := pkceS256(verifier)

	code, _ := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.test/callback",
		CodeChallenge:       challenge,
		CodeChallengeMethod: PKCES256,
	})

	_, err := s.ExchangeCode(context.Background(), TokenRequest{
		GrantType:    "authorization_code",
		Code:         code.Code,
		RedirectURI:  "https://app.test/callback",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		CodeVerifier: "wrong-verifier",
	})
	if err != ErrInvalidCodeChallenge {
		t.Fatalf("expected ErrInvalidCodeChallenge, got %v", err)
	}
}

func TestExchangeCode_InvalidGrantType(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)
	_, err := s.ExchangeCode(context.Background(), TokenRequest{
		GrantType:    "implicit",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
	})
	if err == nil {
		t.Fatal("expected error for invalid grant type")
	}
}

func TestExchangeCode_UsedCode(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	verifier := "verifier"
	challenge := pkceS256(verifier)

	code, _ := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.test/callback",
		CodeChallenge:       challenge,
		CodeChallengeMethod: PKCES256,
	})

	req := TokenRequest{
		GrantType:    "authorization_code",
		Code:         code.Code,
		RedirectURI:  "https://app.test/callback",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		CodeVerifier: verifier,
	}

	_, err := s.ExchangeCode(context.Background(), req)
	if err != nil {
		t.Fatalf("first exchange: %v", err)
	}

	// Second use should fail — code is deleted after first use.
	_, err = s.ExchangeCode(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on second use of same code")
	}
}

func TestRefreshAccessToken(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	verifier := "v"
	code, _ := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:      client.ClientID,
		RedirectURI:   "https://app.test/callback",
		CodeChallenge: pkceS256(verifier),
	})

	tokResp, _ := s.ExchangeCode(context.Background(), TokenRequest{
		GrantType:    "authorization_code",
		Code:         code.Code,
		RedirectURI:  "https://app.test/callback",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		CodeVerifier: verifier,
	})

	// Use refresh token to get new access token.
	refreshResp, err := s.RefreshAccessToken(context.Background(), TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokResp.RefreshToken,
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
	})
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if refreshResp.AccessToken == "" {
		t.Fatal("expected new access_token")
	}
	if refreshResp.AccessToken == tokResp.AccessToken {
		t.Fatal("expected different access_token")
	}
}

func TestRefreshAccessToken_InvalidRefreshToken(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)
	_, err := s.RefreshAccessToken(context.Background(), TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "nonexistent",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
	})
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestIntrospectToken(t *testing.T) {
	s := testServer(t)
	client := registerTestClient(t, s)

	verifier := "v"
	code, _ := s.AuthorizeCode(context.Background(), AuthorizeCodeRequest{
		ClientID:      client.ClientID,
		RedirectURI:   "https://app.test/callback",
		CodeChallenge: pkceS256(verifier),
	})

	tokResp, _ := s.ExchangeCode(context.Background(), TokenRequest{
		GrantType:    "authorization_code",
		Code:         code.Code,
		RedirectURI:  "https://app.test/callback",
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		CodeVerifier: verifier,
	})

	resp := s.IntrospectToken(tokResp.AccessToken)
	if !resp.Active {
		t.Fatal("expected active token")
	}
	if resp.ClientID != client.ClientID {
		t.Fatal("client_id mismatch")
	}

	// Unknown token.
	resp = s.IntrospectToken("nonexistent")
	if resp.Active {
		t.Fatal("expected inactive for unknown token")
	}
}

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
