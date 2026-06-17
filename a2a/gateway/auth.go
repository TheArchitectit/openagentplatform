// Package gateway - auth.go implements the Authenticator, which
// extracts and validates credentials from HTTP requests. It supports
// Bearer tokens, mTLS peer info, and OAuth2 access tokens.
package gateway

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

// ============================================================
// Authenticator errors
// ============================================================

var (
	// ErrMissingCredentials is returned when no auth credentials are present.
	ErrMissingCredentials = errors.New("a2a gateway: missing credentials")

	// ErrInvalidCredentials is returned when credentials are malformed.
	ErrInvalidCredentials = errors.New("a2a gateway: invalid credentials")

	// ErrUnsupportedAuthMethod is returned when the auth method is not allowed.
	ErrUnsupportedAuthMethod = errors.New("a2a gateway: unsupported auth method")
)

// ============================================================
// Authenticator
// ============================================================

// Authenticator validates incoming requests and extracts identities.
type Authenticator struct {
	config     Config
	// StaticTokens maps bearer tokens to identities (for development/testing).
	// In production, token validation would use JWT verification or an
	// OAuth2 introspection endpoint.
	staticTokens map[string]*Identity

	// mTLSValidator validates peer certificate info. nil = mTLS not configured.
	mtlsValidator func(peerID string) (*Identity, error)

	// oauth2Validator validates OAuth2 access tokens. nil = OAuth2 not configured.
	oauth2Validator func(token string) (*Identity, error)
}

// NewAuthenticator creates a new authenticator from the gateway config.
func NewAuthenticator(cfg Config) *Authenticator {
	return &Authenticator{
		config:      cfg,
		staticTokens: make(map[string]*Identity),
	}
}

// SetTokenValidator registers a function that validates bearer tokens.
// This replaces the default static token lookup.
func (a *Authenticator) SetTokenValidator(fn func(token string) (*Identity, error)) {
	a.oauth2Validator = fn
}

// SetMTLSValidator registers a function that validates mTLS peer identities.
func (a *Authenticator) SetMTLSValidator(fn func(peerID string) (*Identity, error)) {
	a.mtlsValidator = fn
}

// AddStaticToken adds a static token -> identity mapping.
// Intended for development and testing only.
func (a *Authenticator) AddStaticToken(token string, id *Identity) {
	a.staticTokens[token] = id
}

// ============================================================
// Authenticate
// ============================================================

// Authenticate extracts an identity from the request. It returns:
//   - (identity, nil) on success
//   - (nil, ErrMissingCredentials) if no credentials present
//   - (nil, ErrInvalidCredentials) if credentials are malformed
//   - (nil, ErrUnsupportedAuthMethod) if the method is not allowed
func (a *Authenticator) Authenticate(r *http.Request) (*Identity, error) {
	// 1. Check Bearer token
	if id, err := a.authenticateBearer(r); err == nil && id != nil {
		return id, a.checkAllowed(AuthBearer)
	}

	// 2. Check mTLS (via custom header set by reverse proxy or TLS terminator)
	if id, err := a.authenticateMTLS(r); err == nil && id != nil {
		return id, a.checkAllowed(AuthMTLS)
	}

	// 3. Check OAuth2
	if id, err := a.authenticateOAuth2(r); err == nil && id != nil {
		return id, a.checkAllowed(AuthOAuth2)
	}

	return nil, ErrMissingCredentials
}

// authenticateBearer extracts and validates a Bearer token.
func (a *Authenticator) authenticateBearer(r *http.Request) (*Identity, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, ErrMissingCredentials
	}
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, ErrMissingCredentials
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		return nil, ErrInvalidCredentials
	}

	// Try OAuth2 validator first
	if a.oauth2Validator != nil {
		return a.oauth2Validator(token)
	}

	// Fall back to static token lookup
	id, ok := a.staticTokens[token]
	if !ok {
		return nil, ErrInvalidCredentials
	}
	return id, nil
}

// authenticateMTLS extracts peer identity from mTLS headers.
func (a *Authenticator) authenticateMTLS(r *http.Request) (*Identity, error) {
	if a.mtlsValidator == nil {
		return nil, nil
	}
	peerID := r.Header.Get("X-SSL-Client-S-DN")
	if peerID == "" {
		peerID = r.Header.Get("X-Client-DN")
	}
	if peerID == "" {
		return nil, nil
	}
	return a.mtlsValidator(peerID)
}

// authenticateOAuth2 validates an OAuth2 access token.
func (a *Authenticator) authenticateOAuth2(r *http.Request) (*Identity, error) {
	if a.oauth2Validator == nil {
		return nil, nil
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, nil
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		return nil, nil
	}
	return a.oauth2Validator(token)
}

// checkAllowed verifies the auth method is in the allowed list.
// If no methods are configured, all are allowed.
func (a *Authenticator) checkAllowed(method AuthMethod) error {
	if len(a.config.AllowedAuthMethods) == 0 {
		return nil
	}
	for _, m := range a.config.AllowedAuthMethods {
		if m == method {
			return nil
		}
	}
	return ErrUnsupportedAuthMethod
}

// ============================================================
// Token comparison (constant-time)
// ============================================================

// compareTokens performs a constant-time comparison of two tokens.
func compareTokens(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// decodeBasicAuth decodes a Basic auth header value.
func decodeBasicAuth(header string) (username, password string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
