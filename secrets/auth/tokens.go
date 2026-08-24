// Package auth provides A2A authentication token management.
// This file defines the token types, errors, Ed25519 key helpers, and the
// TokenIssuer constructor. JWT signing/verification lives in token_issue.go
// and delegation/revocation logic in token_exchange.go.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
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
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
	issuer     string
	revocation *RevocationList
	defaultTTL time.Duration
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

// generateJTI creates a random unique token identifier.
// Uses 128 bits of randomness encoded as a URL-safe base64 string.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
