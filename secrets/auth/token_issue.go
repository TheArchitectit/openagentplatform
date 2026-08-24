package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Issue creates and signs a new JWT with the given claims.
// The issuer's configured "iss" value is automatically set; iss in the provided
// claims is overridden. iat and nbf are set to now. exp must be set in claims.
// A JTI is auto-generated if not provided.
func (t *TokenIssuer) Issue(claims TokenClaims) (string, error) {
	now := time.Now()

	// Auto-generate JTI if not provided.
	if claims.JTI == "" {
		jti, err := generateJTI()
		if err != nil {
			return "", fmt.Errorf("auth: generate JTI: %w", err)
		}
		claims.JTI = jti
	}

	// Enforce issuer.
	claims.Issuer = t.issuer

	// Set timestamps.
	claims.IssuedAt = now.Unix()
	if claims.NotBefore == 0 {
		claims.NotBefore = now.Unix()
	}
	if claims.ExpiresAt == 0 {
		return "", errors.New("auth: ExpiresAt must be set in claims")
	}
	if claims.ExpiresAt <= now.Unix() {
		return "", errors.New("auth: ExpiresAt must be in the future")
	}

	// Ensure scopes is never nil for JSON consistency.
	if claims.Scopes == nil {
		claims.Scopes = []string{}
	}
	if claims.DelegationChain == nil {
		claims.DelegationChain = []DelegationEntry{}
	}

	// Build header.
	header := jwtHeader{
		Alg: "EdDSA",
		Typ: "JWT",
		Kid: t.keyID,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("auth: marshal header: %w", err)
	}

	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := headerB64 + "." + payloadB64

	sig := ed25519.Sign(t.privateKey, []byte(signingInput))
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return signingInput + "." + sigB64, nil
}

// Verify parses, validates signature, expiry, nbf, and revocation status of a JWT.
// It does NOT check scope requirements; use Matches() for that after verification.
func (t *TokenIssuer) Verify(tokenStr string) (*TokenClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidTokenFormat
	}

	headerB64, payloadB64, sigB64 := parts[0], parts[1], parts[2]

	// Decode and validate header.
	headerJSON, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		return nil, fmt.Errorf("auth: decode header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("auth: parse header: %w", err)
	}
	if header.Alg != "EdDSA" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlgorithm, header.Alg)
	}

	// Verify signature.
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("auth: decode signature: %w", err)
	}
	signingInput := headerB64 + "." + payloadB64
	if !ed25519.Verify(t.publicKey, []byte(signingInput), sig) {
		return nil, ErrInvalidSignature
	}

	// Decode claims.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("auth: decode payload: %w", err)
	}
	var claims TokenClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("auth: parse claims: %w", err)
	}

	now := time.Now().Unix()

	// Check expiration.
	if claims.ExpiresAt <= now {
		return nil, ErrTokenExpired
	}

	// Check not-before.
	if claims.NotBefore > now {
		return nil, ErrTokenNotYetValid
	}

	// Check issuer matches (prevents tokens from other issuers being accepted).
	if claims.Issuer != t.issuer {
		return nil, fmt.Errorf("auth: unexpected issuer: %s", claims.Issuer)
	}

	// Check revocation.
	if t.revocation.Contains(claims.JTI) {
		return nil, ErrTokenRevoked
	}

	// Compute remaining TTL.
	claims.RemainingTTL = time.Duration(claims.ExpiresAt-now) * time.Second

	return &claims, nil
}
