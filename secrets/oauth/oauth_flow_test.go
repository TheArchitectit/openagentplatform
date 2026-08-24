package oauth

import (
	"context"
	"testing"
)

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
