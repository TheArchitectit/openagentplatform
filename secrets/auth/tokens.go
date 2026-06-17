// Package auth provides A2A authentication token management.
// This file implements the TokenIssuer with EdDSA (Ed25519) JWT signing/verification.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxDelegationDepth is the maximum allowed delegation chain depth.
const MaxDelegationDepth = 3

// TTL reduction factor per delegation hop (50% reduction).
const ttlReductionFactor = 0.5

// Errors returned by the token issuer.
var (
	ErrTokenExpired       = errors.New("auth: token expired")
	ErrTokenNotYetValid   = errors.New("auth: token not yet valid")
	ErrTokenRevoked       = errors.New("auth: token revoked")
	ErrInvalidSignature   = errors.New("auth: invalid token signature")
	ErrInvalidAlgorithm   = errors.New("auth: unexpected signing algorithm")
	ErrInvalidTokenFormat = errors.New("auth: invalid token format")
	ErrMaxDelegationDepth = errors.New("auth: maximum delegation depth exceeded")
	ErrScopeNotAllowed    = errors.New("auth: requested scope not in parent token")
)

// TokenClaims represents the verified claims of an A2A JWT.
type TokenClaims struct {
	Issuer          string            `json:"iss"`
	Subject         string            `json:"sub"`
	Audience        string            `json:"aud"`
	JTI             string            `json:"jti"`
	IssuedAt        int64             `json:"iat"`
	NotBefore       int64             `json:"nbf"`
	ExpiresAt       int64             `json:"exp"`
	Scopes          []string          `json:"scopes"`
	DelegationChain []DelegationEntry `json:"delegation_chain"`
	// RemainingTTL is computed at verification time.
	RemainingTTL time.Duration `json:"-"`
}

// DelegationEntry records a single hop in the delegation chain.
type DelegationEntry struct {
	Issuer      string   `json:"issuer"`
	DelegatedTo string   `json:"delegated_to"`
	Scopes      []string `json:"scopes"`
	Exp         int64    `json:"exp"`
}

// jwtHeader is the JWT header for EdDSA tokens.
type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

// TokenIssuer issues and verifies EdDSA (Ed25519) signed JWTs for A2A communication.
type TokenIssuer struct {
	privateKey   ed25519.PrivateKey
	publicKey    ed25519.PublicKey
	keyID        string
	issuer       string
	revocation   *RevocationList
	defaultTTL   time.Duration
}

// NewTokenIssuer creates a new TokenIssuer with the given Ed25519 key pair.
// keyID is an opaque identifier published in the JWT header for key rotation.
// issuer is the value set in the "iss" claim of issued tokens.
// revocation is the revocation list used during verification.
func NewTokenIssuer(privKey ed25519.PrivateKey, keyID, issuer string, revocation *RevocationList) (*TokenIssuer, error) {
	if privKey == nil {
		return nil, errors.New("auth: private key is nil")
	}
	if revocation == nil {
		return nil, errors.New("auth: revocation list is nil")
	}
	pubKey, ok := privKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("auth: failed to derive public key")
	}
	return &TokenIssuer{
		privateKey: privKey,
		publicKey:  pubKey,
		keyID:      keyID,
		issuer:     issuer,
		revocation: revocation,
		defaultTTL: 1 * time.Hour,
	}, nil
}

// GenerateKeyPair generates a new Ed25519 key pair suitable for use with TokenIssuer.
func GenerateKeyPair() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: generate key: %w", err)
	}
	return priv, pub, nil
}

// EncodePrivateKeyPEM encodes an Ed25519 private key to PEM format.
func EncodePrivateKeyPEM(priv ed25519.PrivateKey) ([]byte, error) {
	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("auth: marshal private key: %w", err)
	}
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}
	return pem.EncodeToMemory(pemBlock), nil
}

// DecodePrivateKeyPEM decodes an Ed25519 private key from PEM format.
func DecodePrivateKeyPEM(pemData []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("auth: no PEM block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse PKCS8 private key: %w", err)
	}
	privKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("auth: key is not Ed25519")
	}
	return privKey, nil
}

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

// Exchange creates a new down-scoped token from a parent token. The child token
// has a subset of the parent's scopes, an extended delegation chain, and a
// reduced TTL (50% per hop). Maximum delegation depth is 3.
func (t *TokenIssuer) Exchange(parentToken string, requestedScopes []string) (string, error) {
	parent, err := t.Verify(parentToken)
	if err != nil {
		return "", fmt.Errorf("auth: verify parent: %w", err)
	}

	// Check delegation depth.
	currentDepth := len(parent.DelegationChain)
	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("%w: current depth %d", ErrMaxDelegationDepth, currentDepth)
	}

	// Down-scope: requested scopes must be a subset of parent scopes.
	if !Matches(requestedScopes, parent.Scopes) {
		return "", fmt.Errorf("%w: requested scopes not all granted by parent", ErrScopeNotAllowed)
	}

	// Reduce TTL by 50% per hop.
	parentTTL := parent.RemainingTTL
	newTTL := time.Duration(float64(parentTTL) * ttlReductionFactor)
	if newTTL <= 0 {
		return "", errors.New("auth: parent TTL too short for further delegation")
	}

	// Extend delegation chain.
	newChain := make([]DelegationEntry, len(parent.DelegationChain)+1)
	copy(newChain, parent.DelegationChain)
	newChain[currentDepth] = DelegationEntry{
		Issuer:      parent.Subject,
		DelegatedTo: parent.Audience,
		Scopes:      requestedScopes,
		Exp:         time.Now().Add(newTTL).Unix(),
	}

	// Issue child token.
	childClaims := TokenClaims{
		Subject:         parent.Subject,
		Audience:        parent.Audience,
		ExpiresAt:       time.Now().Add(newTTL).Unix(),
		Scopes:          requestedScopes,
		DelegationChain: newChain,
	}

	return t.Issue(childClaims)
}

// Revoke adds a JTI to the revocation list with the given TTL.
// The TTL should typically be the remaining lifetime of the token.
func (t *TokenIssuer) Revoke(jti string, ttl time.Duration) {
	expiresAt := time.Now().Add(ttl)
	t.revocation.Add(jti, expiresAt)
}

// IsRevoked reports whether the given JTI is in the revocation list.
func (t *TokenIssuer) IsRevoked(jti string) bool {
	return t.revocation.Contains(jti)
}

// PublicKey returns the Ed25519 public key for external verification.
func (t *TokenIssuer) PublicKey() ed25519.PublicKey {
	return t.publicKey
}

// KeyID returns the configured key identifier.
func (t *TokenIssuer) KeyID() string {
	return t.keyID
}

// generateJTI creates a random unique token identifier.
// Uses 128 bits of randomness encoded as a URL-safe base64 string.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
