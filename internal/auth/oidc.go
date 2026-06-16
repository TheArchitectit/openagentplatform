package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// Role constants for RBAC mapping from OIDC groups.
const (
	RoleAdmin      = "admin"
	RoleTechnician = "technician"
	RoleViewer     = "viewer"
	RoleOperator   = "operator"
)

// groupRoleMap maps OIDC group names to internal RBAC roles.
var groupRoleMap = map[string]string{
	"oap-admins":      RoleAdmin,
	"oap-technicians": RoleTechnician,
	"oap-operators":   RoleOperator,
	"oap-viewers":     RoleViewer,
}

// Claims represents the claims extracted from a verified OIDC ID token.
type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Groups  []string `json:"groups"`
	OrgID   string   `json:"org_id"`
	SiteID  string   `json:"site_id"`
	Role    string   `json:"role"`
}

// Verifier wraps an OIDC provider and ID-token verifier.
type Verifier struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// SessionMinter mints internal session JWTs signed with EdDSA.
type SessionMinter struct {
	signKey   ed25519.PrivateKey
	issuer    string
	audience  string
	expiryDur time.Duration
}

// NewVerifier discovers the OIDC provider at issuerURL and returns a Verifier
// that will accept ID tokens issued for clientID.
func NewVerifier(ctx context.Context, issuerURL, clientID string) (*Verifier, error) {
	if issuerURL == "" {
		return nil, errors.New("oidc: issuer URL required")
	}
	if clientID == "" {
		return nil, errors.New("oidc: client ID required")
	}

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: provider discovery: %w", err)
	}

	v := provider.Verifier(&oidc.Config{ClientID: clientID})
	return &Verifier{provider: provider, verifier: v}, nil
}

// Verify validates rawIDToken and returns the extracted claims.
func (v *Verifier) Verify(ctx context.Context, rawIDToken string) (*Claims, error) {
	tok, err := v.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: verify token: %w", err)
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}
	return &c, nil
}

// MapGroupsToRole returns the highest-privilege RBAC role for the given groups.
// Returns "viewer" if no recognised group is present.
func MapGroupsToRole(groups []string) string {
	// Precedence: admin > technician > operator > viewer.
	precedence := map[string]int{
		RoleAdmin:      4,
		RoleTechnician: 3,
		RoleOperator:   2,
		RoleViewer:     1,
	}
	best := ""
	bestRank := 0
	for _, g := range groups {
		if role, ok := groupRoleMap[g]; ok {
			if r, ok := precedence[role]; ok && r > bestRank {
				bestRank = r
				best = role
			}
		}
	}
	if best == "" {
		return RoleViewer
	}
	return best
}

// NewSessionMinter creates a SessionMinter. If privKeyPEM is non-empty it is
// parsed as a PKCS#8 PEM-encoded Ed25519 private key; otherwise a fresh
// ephemeral key is generated (suitable for tests only).
func NewSessionMinter(issuer, audience string, expiry time.Duration, privKeyPEM string) (*SessionMinter, error) {
	if issuer == "" {
		return nil, errors.New("auth: session issuer required")
	}
	if expiry <= 0 {
		expiry = time.Hour
	}

	var key ed25519.PrivateKey
	if privKeyPEM != "" {
		block, _ := pem.Decode([]byte(privKeyPEM))
		if block == nil {
			return nil, errors.New("auth: failed to decode session key PEM")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("auth: parse session key: %w", err)
		}
		k, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("auth: session key is not Ed25519")
		}
		key = k
	} else {
		_, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("auth: generate session key: %w", err)
		}
		key = k
	}

	return &SessionMinter{
		signKey:   key,
		issuer:    issuer,
		audience:  audience,
		expiryDur: expiry,
	}, nil
}

// NewSessionMinterFromFile loads the Ed25519 private key from a PEM file.
func NewSessionMinterFromFile(issuer, audience string, expiry time.Duration, path string) (*SessionMinter, error) {
	if path == "" {
		return NewSessionMinter(issuer, audience, expiry, "")
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: read session key: %w", err)
	}
	return NewSessionMinter(issuer, audience, expiry, string(pemBytes))
}

// SessionClaims is the payload carried by the internal session JWT.
type SessionClaims struct {
	Email  string   `json:"email"`
	Name   string   `json:"name,omitempty"`
	Groups []string `json:"groups,omitempty"`
	OrgID  string   `json:"org_id,omitempty"`
	SiteID string   `json:"site_id,omitempty"`
	Role   string   `json:"role"`
	jwt.RegisteredClaims
}

// Mint produces a signed EdDSA session JWT for the given user.
func (m *SessionMinter) Mint(c *Claims) (string, error) {
	if c == nil {
		return "", errors.New("auth: claims required")
	}
	now := time.Now()
	sc := SessionClaims{
		Email:  c.Email,
		Name:   c.Name,
		Groups: c.Groups,
		OrgID:  c.OrgID,
		SiteID: c.SiteID,
		Role:   MapGroupsToRole(c.Groups),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.Subject,
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{m.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiryDur)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, sc)
	signed, err := tok.SignedString(m.signKey)
	if err != nil {
		return "", fmt.Errorf("auth: sign session: %w", err)
	}
	return signed, nil
}

// MintPublicKey returns the base64-encoded raw public key, useful for JWKS.
func (m *SessionMinter) MintPublicKey() string {
	pub := m.signKey.Public().(ed25519.PublicKey)
	return base64.RawURLEncoding.EncodeToString(pub)
}

// Parse validates a session JWT and returns its claims.
func (m *SessionMinter) Parse(raw string) (*SessionClaims, error) {
	if m == nil {
		return nil, errors.New("auth: session minter not configured")
	}
	claims := &SessionClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return m.signKey.Public(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: parse session: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("auth: invalid session token")
	}
	return claims, nil
}
