package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/openagentplatform/openagentplatform/internal/audit"
)

type AuthorizeCodeRequest struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod PKCEMethod
	DPoPJKT             string // optional: bind code to a DPoP key
}

// AuthorizeCode issues a new authorization code for the given request.
// The code is short-lived and single-use.
func (a *AuthorizationServer) AuthorizeCode(ctx context.Context, req AuthorizeCodeRequest) (*AuthCode, error) {
	if req.ClientID == "" {
		return nil, errors.New("oauth: client_id is required")
	}
	if req.RedirectURI == "" {
		return nil, errors.New("oauth: redirect_uri is required")
	}
	if req.CodeChallenge == "" {
		return nil, ErrInvalidCodeChallenge
	}
	method := req.CodeChallengeMethod
	if method == "" {
		method = PKCES256
	}
	if method != PKCES256 && method != PKCEPlain {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedMethod, method)
	}

	// Verify the client and redirect URI.
	a.mu.RLock()
	client, ok := a.clients[req.ClientID]
	a.mu.RUnlock()
	if !ok {
		return nil, ErrClientNotFound
	}
	if !redirectURIMatches(client.Metadata.RedirectURIs, req.RedirectURI) {
		return nil, ErrInvalidRedirectURI
	}

	code, err := generateToken("code_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate code: %w", err)
	}

	now := time.Now().UTC()
	authCode := &AuthCode{
		Code:                code,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		Scope:               req.Scope,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: method,
		IssuedAt:            now,
		ExpiresAt:           now.Add(AuthorizationCodeTTL),
		DPoPJKT:             req.DPoPJKT,
	}

	a.mu.Lock()
	a.codes[code] = authCode
	a.mu.Unlock()

	a.emitAudit(ctx, "oauth.code.issue", req.ClientID, audit.OutcomeSuccess,
		fmt.Sprintf("scope=%s dpop_bound=%t", req.Scope, req.DPoPJKT != ""))

	return authCode, nil
}

// --- Token Endpoint ---

// TokenRequest is the parsed token endpoint request.
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	ClientSecret string
	CodeVerifier string
	RefreshToken string
	// DPoPProof is the DPoP proof JWT for token-bound requests.
	DPoPProof string
	// DPoPKeyThumbprint is the JKT thumbprint from a pre-validated DPoP
	// request (already computed by the DPoP validator middleware).
	DPoPKeyThumbprint string
}

// TokenResponse is the response from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	// Cnf carries the confirmation of the DPoP key binding.
	Cnf *CnfClaim `json:"cnf,omitempty"`
}

// CnfClaim carries the DPoP key confirmation (RFC 9449).
type CnfClaim struct {
	JKT string `json:"jkt"`
}

// ExchangeCode exchanges an authorization code + PKCE verifier for an
// access token and refresh token.
func (a *AuthorizationServer) ExchangeCode(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	if req.GrantType != "authorization_code" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidGrantType, req.GrantType)
	}
	if req.Code == "" {
		return nil, errors.New("oauth: code is required")
	}
	if req.CodeVerifier == "" {
		return nil, errors.New("oauth: code_verifier is required")
	}

	// Authenticate the client.
	if err := a.VerifyClientSecret(req.ClientID, req.ClientSecret); err != nil {
		a.emitAudit(ctx, "oauth.token.exchange", req.ClientID, audit.OutcomeDenied,
			"client authentication failed")
		return nil, err
	}

	a.mu.Lock()
	code, ok := a.codes[req.Code]
	if !ok {
		a.mu.Unlock()
		return nil, ErrCodeNotFound
	}
	if code.Used {
		a.mu.Unlock()
		return nil, ErrCodeAlreadyUsed
	}
	if time.Now().After(code.ExpiresAt) {
		delete(a.codes, req.Code)
		a.mu.Unlock()
		return nil, ErrCodeExpired
	}
	if code.ClientID != req.ClientID {
		a.mu.Unlock()
		return nil, errors.New("oauth: code was issued to a different client")
	}
	if code.RedirectURI != req.RedirectURI {
		a.mu.Unlock()
		return nil, errors.New("oauth: redirect_uri mismatch")
	}

	// Verify PKCE.
	if !verifyPKCE(code.CodeChallenge, code.CodeChallengeMethod, req.CodeVerifier) {
		a.mu.Unlock()
		return nil, ErrInvalidCodeChallenge
	}

	// If the code was DPoP-bound, the token request must use the same key.
	if code.DPoPJKT != "" && code.DPoPJKT != req.DPoPKeyThumbprint {
		a.mu.Unlock()
		return nil, errors.New("oauth: DPoP key does not match code binding")
	}

	// Mark code as used and delete it (single-use).
	code.Used = true
	delete(a.codes, req.Code)

	// Issue tokens.
	accessToken, err := generateToken("at_")
	if err != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("oauth: generate access token: %w", err)
	}
	refreshToken, err := generateToken("rt_")
	if err != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("oauth: generate refresh token: %w", err)
	}

	now := time.Now().UTC()
	accessTok := &AccessToken{
		Token:     accessToken,
		ClientID:  req.ClientID,
		Scope:     code.Scope,
		IssuedAt:  now,
		ExpiresAt: now.Add(AccessTokenTTL),
		DPoPJKT:   req.DPoPKeyThumbprint,
	}
	refreshTok := &RefreshToken{
		Token:     refreshToken,
		ClientID:  req.ClientID,
		Scope:     code.Scope,
		IssuedAt:  now,
		ExpiresAt: now.Add(RefreshTokenTTL),
	}

	a.tokens[accessToken] = accessTok
	a.refresh[refreshToken] = refreshTok
	a.mu.Unlock()

	a.emitAudit(ctx, "oauth.token.issue", req.ClientID, audit.OutcomeSuccess,
		fmt.Sprintf("grant=authorization_code scope=%s dpop_bound=%t",
			code.Scope, req.DPoPKeyThumbprint != ""))

	resp := &TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "DPoP",
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        code.Scope,
	}
	if req.DPoPKeyThumbprint != "" {
		resp.Cnf = &CnfClaim{JKT: req.DPoPKeyThumbprint}
	}
	return resp, nil
}

// RefreshAccessToken exchanges a refresh token for a new access token.
