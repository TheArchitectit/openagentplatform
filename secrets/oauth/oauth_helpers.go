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
	"time"
	"github.com/openagentplatform/openagentplatform/internal/audit"
)

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
