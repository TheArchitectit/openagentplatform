package auth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"
)

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

	now := time.Now()

	// Extend delegation chain.
	newChain := make([]DelegationEntry, len(parent.DelegationChain)+1)
	copy(newChain, parent.DelegationChain)
	newChain[currentDepth] = DelegationEntry{
		Issuer:      parent.Subject,
		DelegatedTo: parent.Audience,
		Scopes:      requestedScopes,
		Exp:         now.Add(newTTL).Unix(),
	}

	// Issue child token.
	childClaims := TokenClaims{
		Subject:         parent.Subject,
		Audience:        parent.Audience,
		ExpiresAt:       now.Add(newTTL).Unix(),
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
