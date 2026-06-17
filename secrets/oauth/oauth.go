// Package oauth implements MCP OAuth 2.1 with Dynamic Client Registration
// (RFC 7591), Authorization Code grant with PKCE (RFC 7636), DPoP token
// binding (RFC 9449), and Protected Resource Metadata (RFC 9728).
//
// This package is the authorization server for MCP clients connecting to
// OpenAgentPlatform protected resources.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/audit"
)

// --- Errors ---

var (
	ErrClientNotFound       = errors.New("oauth: client not found")
	ErrClientAlreadyExists  = errors.New("oauth: client already exists")
	ErrInvalidRedirectURI   = errors.New("oauth: invalid redirect URI")
	ErrInvalidGrantType     = errors.New("oauth: invalid grant type")
	ErrInvalidCodeChallenge = errors.New("oauth: invalid PKCE code challenge")
	ErrCodeNotFound         = errors.New("oauth: authorization code not found")
	ErrCodeExpired          = errors.New("oauth: authorization code expired")
	ErrCodeAlreadyUsed      = errors.New("oauth: authorization code already used")
	ErrInvalidClientSecret  = errors.New("oauth: invalid client secret")
	ErrTokenNotFound        = errors.New("oauth: token not found")
	ErrTokenExpired         = errors.New("oauth: token expired")
	ErrDPoPProofInvalid     = errors.New("oauth: DPoP proof invalid")
	ErrUnsupportedMethod    = errors.New("oauth: unsupported PKCE method")
)

// --- Constants ---

// AuthorizationCodeTTL is the lifetime of an authorization code.
const AuthorizationCodeTTL = 60 * time.Second

// AccessTokenTTL is the lifetime of an access token.
const AccessTokenTTL = 1 * time.Hour

// RefreshTokenTTL is the lifetime of a refresh token.
const RefreshTokenTTL = 24 * time.Hour

// NonceTTL is the lifetime of a server-issued DPoP nonce.
const NonceTTL = 120 * time.Second

// RegistrationAccessTokenTTL is the lifetime of a registration access token.
const RegistrationAccessTokenTTL = 24 * time.Hour

// --- Client Registration (RFC 7591) ---

// ClientMetadata is the metadata submitted during Dynamic Client Registration.
type ClientMetadata struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
	TOSUri                  string   `json:"tos_uri,omitempty"`
	PolicyURI               string   `json:"policy_uri,omitempty"`
	JWKSURI                 string   `json:"jwks_uri,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
}

// ClientRegistrationResponse is the response to a Dynamic Client Registration request.
type ClientRegistrationResponse struct {
	ClientID                string         `json:"client_id"`
	ClientSecret            string         `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64          `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64          `json:"client_secret_expires_at,omitempty"`
	RegistrationAccessToken string         `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string         `json:"registration_client_uri,omitempty"`
	RedirectURIs            []string       `json:"redirect_uris"`
	TokenEndpointAuthMethod string         `json:"token_endpoint_auth_method"`
	GrantTypes              []string       `json:"grant_types,omitempty"`
	ResponseTypes           []string       `json:"response_types,omitempty"`
	ClientName              string         `json:"client_name,omitempty"`
	ClientURI               string         `json:"client_uri,omitempty"`
	Scope                   string         `json:"scope,omitempty"`
}

// --- Authorization Code with PKCE (RFC 7636) ---

// PKCEMethod identifies the code challenge transformation method.
type PKCEMethod string

const (
	// PKCES256 uses SHA-256 for the code challenge.
	PKCES256 PKCEMethod = "S256"
	// PKCEPlain uses the plain code_verifier as the challenge (discouraged).
	PKCEPlain PKCEMethod = "plain"
)

// AuthCode represents an issued authorization code.
type AuthCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod PKCEMethod
	IssuedAt            time.Time
	ExpiresAt           time.Time
	Used                bool
	// DPoPJKT binds the code to a specific DPoP key (thumbprint of JWK).
	DPoPJKT string
}

// --- Access Token / Refresh Token ---

// AccessToken represents an issued access token.
type AccessToken struct {
	Token     string
	ClientID  string
	Scope     string
	IssuedAt  time.Time
	ExpiresAt time.Time
	// DPoPJKT binds the token to a specific DPoP public key.
	DPoPJKT string
	// Revoked indicates the token has been revoked.
	Revoked bool
}

// RefreshToken represents an issued refresh token.
type RefreshToken struct {
	Token     string
	ClientID  string
	Scope     string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// --- Protected Resource Metadata (RFC 9728) ---

// ProtectedResourceMetadata describes the protected resource.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	DPoPSupported          bool     `json:"dpop_supported"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
}

// --- Introspection ---

// IntrospectionResponse follows RFC 7662.
type IntrospectionResponse struct {
	Active    bool   `json:"active"`
	Scope     string `json:"scope,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Exp       int64  `json:"exp,omitempty"`
	Iat       int64  `json:"iat,omitempty"`
	Nbf       int64  `json:"nbf,omitempty"`
	Sub       string `json:"sub,omitempty"`
	Aud       string `json:"aud,omitempty"`
	Iss       string `json:"iss,omitempty"`
	JTI       string `json:"jti,omitempty"`
}

// --- Client Management ---

// RegisteredClient is a client registered via Dynamic Client Registration.
type RegisteredClient struct {
	ClientID                string
	ClientSecretHash        []byte // SHA-256 of the client secret
	ClientSecretPrefix      string // first 4 chars for identification
	Metadata                ClientMetadata
	CreatedAt               time.Time
	RegistrationAccessToken string
}

// --- AuthorisationServer ---

// AuthorizationServer is the main MCP OAuth 2.1 authorization server.
type AuthorizationServer struct {
	mu sync.RWMutex

	// issuer is the authorization server's issuer URL.
	issuer string
	// resource is the resource server URL this authz server protects.
	resource string
	// registrationEndpoint is the full URL for client registration.
	registrationEndpoint string

	clients   map[string]*RegisteredClient // clientID -> client
	codes     map[string]*AuthCode         // code -> auth code
	tokens    map[string]*AccessToken      // access token -> info
	refresh   map[string]*RefreshToken     // refresh token -> info
	nonces    map[string]time.Time         // nonce -> expiry

	audit  *audit.AuditService
	logger *slog.Logger
}

// NewAuthorizationServer creates a new OAuth 2.1 authorization server.
// issuer is the authz server URL (e.g. "https://oap.example.com").
// resource is the resource server URL (e.g. "https://api.oap.example.com").
// registrationEndpoint is the full URL for /.well-known/oauth-protected-resource/register.
func NewAuthorizationServer(
	issuer string,
	resource string,
	registrationEndpoint string,
	auditSvc *audit.AuditService,
	logger *slog.Logger,
) *AuthorizationServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthorizationServer{
		issuer:              issuer,
		resource:            resource,
		registrationEndpoint: registrationEndpoint,
		clients:             make(map[string]*RegisteredClient),
		codes:               make(map[string]*AuthCode),
		tokens:              make(map[string]*AccessToken),
		refresh:             make(map[string]*RefreshToken),
		nonces:              make(map[string]time.Time),
		audit:               auditSvc,
		logger:              logger,
	}
}

// --- Dynamic Client Registration (RFC 7591) ---

// RegisterClient performs Dynamic Client Registration per RFC 7591.
// It generates a client_id and client_secret, stores the client, and
// returns the full registration response.
func (a *AuthorizationServer) RegisterClient(ctx context.Context, metadata ClientMetadata) (*ClientRegistrationResponse, error) {
	if len(metadata.RedirectURIs) == 0 {
		return nil, fmt.Errorf("%w: at least one redirect_uri is required", ErrInvalidRedirectURI)
	}
	for _, uri := range metadata.RedirectURIs {
		if _, err := url.Parse(uri); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidRedirectURI, uri)
		}
	}

	clientID, err := generateToken("client_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate client ID: %w", err)
	}
	clientSecret, err := generateToken("secret_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate client secret: %w", err)
	}
	regAccessToken, err := generateToken("reg_")
	if err != nil {
		return nil, fmt.Errorf("oauth: generate registration access token: %w", err)
	}

	secretHash := sha256.Sum256([]byte(clientSecret))
	prefix := ""
	if len(clientSecret) >= 4 {
		prefix = clientSecret[:4]
	}

	now := time.Now().UTC()
	client := &RegisteredClient{
		ClientID:                clientID,
		ClientSecretHash:        secretHash[:],
		ClientSecretPrefix:      prefix,
		Metadata:                metadata,
		CreatedAt:               now,
		RegistrationAccessToken: regAccessToken,
	}

	a.mu.Lock()
	a.clients[clientID] = client
	a.mu.Unlock()

	a.emitAudit(ctx, "oauth.client.register", clientID, audit.OutcomeSuccess,
		fmt.Sprintf("redirect_uris=%d name=%s", len(metadata.RedirectURIs), metadata.ClientName))

	authMethod := metadata.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "client_secret_basic"
	}

	return &ClientRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        now.Unix(),
		ClientSecretExpiresAt:   now.Add(RegistrationAccessTokenTTL).Unix(),
		RegistrationAccessToken: regAccessToken,
		RegistrationClientURI:   a.registrationEndpoint + "/" + clientID,
		RedirectURIs:            metadata.RedirectURIs,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              metadata.GrantTypes,
		ResponseTypes:           metadata.ResponseTypes,
		ClientName:              metadata.ClientName,
		ClientURI:               metadata.ClientURI,
		Scope:                   metadata.Scope,
	}, nil
}

// RotateClientSecret generates a new client secret for the given client ID.
// The old secret is invalidated immediately.
func (a *AuthorizationServer) RotateClientSecret(ctx context.Context, clientID string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[clientID]
	if !ok {
		return "", ErrClientNotFound
	}

	newSecret, err := generateToken("secret_")
	if err != nil {
		return "", fmt.Errorf("oauth: generate secret: %w", err)
	}

	hash := sha256.Sum256([]byte(newSecret))
	client.ClientSecretHash = hash[:]
	if len(newSecret) >= 4 {
		client.ClientSecretPrefix = newSecret[:4]
	}

	a.emitAudit(ctx, "oauth.client.rotate_secret", clientID, audit.OutcomeSuccess, "")
	return newSecret, nil
}

// DeleteClient removes a client and revokes all its tokens and codes.
func (a *AuthorizationServer) DeleteClient(ctx context.Context, clientID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[clientID]
	if !ok {
		return ErrClientNotFound
	}

	// Revoke all tokens and codes belonging to this client.
	for _, tok := range a.tokens {
		if tok.ClientID == clientID {
			tok.Revoked = true
		}
	}
	for _, rt := range a.refresh {
		if rt.ClientID == clientID {
			rt.Revoked = true
		}
	}
	for _, code := range a.codes {
		if code.ClientID == clientID {
			code.Used = true
		}
	}

	delete(a.clients, clientID)
	a.emitAudit(ctx, "oauth.client.delete", clientID, audit.OutcomeSuccess, "")
	_ = client
	return nil
}

// GetClient retrieves a registered client by ID.
func (a *AuthorizationServer) GetClient(clientID string) (*RegisteredClient, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	client, ok := a.clients[clientID]
	if !ok {
		return nil, ErrClientNotFound
	}
	return client, nil
}

// VerifyClientSecret checks the provided client secret against the stored hash.
func (a *AuthorizationServer) VerifyClientSecret(clientID, clientSecret string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	client, ok := a.clients[clientID]
	if !ok {
		return ErrClientNotFound
	}
	hash := sha256.Sum256([]byte(clientSecret))
	if subtle.ConstantTimeCompare(hash[:], client.ClientSecretHash) != 1 {
		return ErrInvalidClientSecret
	}
	return nil
}

// --- Protected Resource Metadata (RFC 9728) ---

// ProtectedResourceMetadata returns the RFC 9728 metadata document.
func (a *AuthorizationServer) ProtectedResourceMetadata() *ProtectedResourceMetadata {
	return &ProtectedResourceMetadata{
		Resource:               a.resource,
		AuthorizationServers:   []string{a.issuer},
		BearerMethodsSupported: []string{"header", "body"},
		DPoPSupported:          true,
		ScopesSupported:        []string{"read", "write", "admin", "agent:execute"},
	}
}

// --- Authorization Code Grant with PKCE (RFC 7636) ---

// AuthorizeCodeRequest is the parsed authorization request.
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
func (a *AuthorizationServer) RefreshAccessToken(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	if req.GrantType != "refresh_token" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidGrantType, req.GrantType)
	}
	if req.RefreshToken == "" {
		return nil, errors.New("oauth: refresh_token is required")
	}

	// Authenticate the client.
	if err := a.VerifyClientSecret(req.ClientID, req.ClientSecret); err != nil {
		a.emitAudit(ctx, "oauth.token.refresh", req.ClientID, audit.OutcomeDenied,
			"client authentication failed")
		return nil, err
	}

	a.mu.Lock()
	rt, ok := a.refresh[req.RefreshToken]
	if !ok {
		a.mu.Unlock()
		return nil, ErrTokenNotFound
	}
	if rt.Revoked {
		a.mu.Unlock()
		return nil, ErrTokenExpired
	}
	if time.Now().After(rt.ExpiresAt) {
		delete(a.refresh, req.RefreshToken)
		a.mu.Unlock()
		return nil, ErrTokenExpired
	}
	if rt.ClientID != req.ClientID {
		a.mu.Unlock()
		return nil, errors.New("oauth: refresh token was issued to a different client")
	}

	// Revoke old access tokens for this client (optional: could keep them).
	// Issue a new access token.
	accessToken, err := generateToken("at_")
	if err != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("oauth: generate access token: %w", err)
	}

	now := time.Now().UTC()
	accessTok := &AccessToken{
		Token:     accessToken,
		ClientID:  req.ClientID,
		Scope:     rt.Scope,
		IssuedAt:  now,
		ExpiresAt: now.Add(AccessTokenTTL),
		DPoPJKT:   req.DPoPKeyThumbprint,
	}
	a.tokens[accessToken] = accessTok
	a.mu.Unlock()

	a.emitAudit(ctx, "oauth.token.refresh", req.ClientID, audit.OutcomeSuccess,
		fmt.Sprintf("scope=%s", rt.Scope))

	resp := &TokenResponse{
		AccessToken: accessToken,
		TokenType:   "DPoP",
		ExpiresIn:   int(AccessTokenTTL.Seconds()),
		Scope:       rt.Scope,
	}
	if req.DPoPKeyThumbprint != "" {
		resp.Cnf = &CnfClaim{JKT: req.DPoPKeyThumbprint}
	}
	return resp, nil
}

// --- Token Introspection (RFC 7662) ---

// IntrospectToken returns the active state of a token.
func (a *AuthorizationServer) IntrospectToken(token string) *IntrospectionResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if access, ok := a.tokens[token]; ok {
		if access.Revoked || time.Now().After(access.ExpiresAt) {
			return &IntrospectionResponse{Active: false}
		}
		return &IntrospectionResponse{
			Active:    true,
			Scope:     access.Scope,
			ClientID:  access.ClientID,
			TokenType: "DPoP",
			Exp:       access.ExpiresAt.Unix(),
			Iat:       access.IssuedAt.Unix(),
		}
	}

	if refresh, ok := a.refresh[token]; ok {
		if refresh.Revoked || time.Now().After(refresh.ExpiresAt) {
			return &IntrospectionResponse{Active: false}
		}
		return &IntrospectionResponse{
			Active:    true,
			Scope:     refresh.Scope,
			ClientID:  refresh.ClientID,
			TokenType: "refresh_token",
			Exp:       refresh.ExpiresAt.Unix(),
			Iat:       refresh.IssuedAt.Unix(),
		}
	}

	return &IntrospectionResponse{Active: false}
}

// --- Nonce Management ---

// IssueNonce generates and stores a new DPoP nonce.
func (a *AuthorizationServer) IssueNonce() (string, error) {
	nonce, err := generateToken("nonce_")
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.nonces[nonce] = time.Now().Add(NonceTTL)
	a.mu.Unlock()
	return nonce, nil
}

// ConsumeNonce validates and removes a nonce. Returns true if valid.
func (a *AuthorizationServer) ConsumeNonce(nonce string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.nonces[nonce]
	if !ok {
		return false
	}
	delete(a.nonces, nonce)
	return time.Now().Before(expiry)
}

// --- DPoP Key Thumbprint (RFC 9449) ---

// JWKThumbprint computes the JWK thumbprint (SHA-256) of a JWK's required
// members (kty, crv for OKP/EC; kty, e, n for RSA). The input must be the
// JSON-encoded JWK object containing only the required members in
// lexicographic order.
func JWKThumbprint(jwkJSON []byte) (string, error) {
	hash := sha256.Sum256(jwkJSON)
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// --- PKCE Verification (RFC 7636) ---

// verifyPKCE checks the code_verifier against the stored code_challenge.
func verifyPKCE(challenge string, method PKCEMethod, verifier string) bool {
	switch method {
	case PKCES256:
		hash := sha256.Sum256([]byte(verifier))
		computed := base64.RawURLEncoding.EncodeToString(hash[:])
		return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
	case PKCEPlain:
		return subtle.ConstantTimeCompare([]byte(verifier), []byte(challenge)) == 1
	default:
		return false
	}
}

// --- Internal helpers ---

// generateToken creates a cryptographically random token string with a
// type prefix. The token is URL-safe base64 (no padding) of 32 random bytes.
func generateToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// redirectURIMatches reports whether the candidate matches one of the
// registered redirect URIs (exact string match per RFC 7591/6749).
func redirectURIMatches(registered []string, candidate string) bool {
	for _, r := range registered {
		if r == candidate {
			return true
		}
	}
	return false
}

// emitAudit is a thin wrapper over the audit service.
func (a *AuthorizationServer) emitAudit(ctx context.Context, action, resourceID string, outcome audit.Outcome, detail string) {
	if a.audit == nil {
		return
	}
	details := map[string]any{}
	if detail != "" {
		details["detail"] = detail
	}
	_, _ = a.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorSystem,
		ActorID:      "oauth-server",
		Action:       action,
		ResourceType: "oauth",
		ResourceID:   resourceID,
		Details:      details,
		Outcome:      outcome,
	})
}

// --- JSON helpers (for documentation/testing) ---

// MarshalProtectedResourceMetadata serialises the metadata to JSON.
func MarshalProtectedResourceMetadata(m *ProtectedResourceMetadata) ([]byte, error) {
	return json.Marshal(m)
}

// hexEncode is a convenience function for hex encoding.
func hexEncode(b []byte) string {
	return hex.EncodeToString(b)
}
