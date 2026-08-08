package oauth

import (
	"errors"
	"log/slog"
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
