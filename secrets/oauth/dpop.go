// Package oauth — DPoP proof validation (RFC 9449).
//
// DPoP (Demonstrating Proof-of-Possession) binds an access token to a
// specific public key. The client proves possession of the corresponding
// private key by signing a JWT-like proof that includes the HTTP method,
// URL, timestamp, and (for token requests) the access token hash.
//
// This file implements the DPoP proof structures and the server-side
// proof validator. Signature verification and JWK thumbprint helpers live
// in dpop_signature.go.
package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DPoP header typ value per RFC 9449.
const dpopType = "dpop+jwt"

// DPoPTimeWindow is the maximum allowed clock skew for iat validation.
const DPoPTimeWindow = 60 * time.Second

// --- DPoP Proof Structure ---

// DPoPJWK is the JWK contained in the proof header.
type DPoPJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Kid string `json:"kid,omitempty"`
}

// DPoPHeader is the JOSE header of a DPoP proof JWT.
type DPoPHeader struct {
	Type string  `json:"typ"`
	Alg  string  `json:"alg"`
	JWK  DPoPJWK `json:"jwk"`
	JKT  string  `json:"jkt,omitempty"`
	// Nonce is included when the server has issued a nonce.
	Nonce string `json:"nonce,omitempty"`
}

// DPoPPayload is the JWT payload of a DPoP proof.
type DPoPPayload struct {
	// JTI is a unique identifier for this proof.
	JTI string `json:"jti"`
	// HTM is the HTTP method of the request.
	HTM string `json:"htm"`
	// HTU is the HTTP URL of the request.
	HTU string `json:"htu"`
	// IAT is the time the proof was created (seconds since epoch).
	IAT int64 `json:"iat"`
	// ATH is the SHA-256 hash of the access token (for token-bound requests).
	ATH string `json:"ath,omitempty"`
	// Nonce is included when the server has issued a nonce.
	Nonce string `json:"nonce,omitempty"`
}

// DPoPProof is the parsed DPoP proof structure.
type DPoPProof struct {
	Header     DPoPHeader
	Payload    DPoPPayload
	Signature  []byte
	RawHeader  string
	RawPayload string
	RawToken   string
}

// --- DPoP Validator ---

// DPoPValidator validates DPoP proofs per RFC 9449.
type DPoPValidator struct {
	mu sync.RWMutex

	// usedJTIs tracks recently seen JTI values to prevent replay.
	usedJTIs map[string]time.Time

	// server is the parent authorization server (for nonce management).
	server *AuthorizationServer
}

// NewDPoPValidator creates a new DPoP validator.
func NewDPoPValidator(server *AuthorizationServer) *DPoPValidator {
	return &DPoPValidator{
		usedJTIs: make(map[string]time.Time),
		server:   server,
	}
}

// ParseDPoPProof parses a DPoP proof JWT from the HTTP DPoP header value.
// The proof is a JWT with three parts: header.payload.signature, all
// URL-safe base64 encoded.
func ParseDPoPProof(header string) (*DPoPProof, error) {
	parts := strings.SplitN(header, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 JWT parts, got %d", ErrDPoPProofInvalid, len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: decode header: %v", ErrDPoPProofInvalid, err)
	}
	var hdr DPoPHeader
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		return nil, fmt.Errorf("%w: parse header JSON: %v", ErrDPoPProofInvalid, err)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %v", ErrDPoPProofInvalid, err)
	}
	var pay DPoPPayload
	if err := json.Unmarshal(payloadJSON, &pay); err != nil {
		return nil, fmt.Errorf("%w: parse payload JSON: %v", ErrDPoPProofInvalid, err)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: decode signature: %v", ErrDPoPProofInvalid, err)
	}

	return &DPoPProof{
		Header:     hdr,
		Payload:    pay,
		Signature:  sig,
		RawHeader:  parts[0],
		RawPayload: parts[1],
		RawToken:   header,
	}, nil
}

// ValidateRequest validates a DPoP proof for a regular (non-token) request.
// It checks:
//   - htm matches the expected HTTP method
//   - htu matches the expected URL
//   - iat is within the allowed time window
//   - jti is unique (not seen before)
//
// Returns the proof's JWK thumbprint on success.
func (v *DPoPValidator) ValidateRequest(
	proof *DPoPProof,
	expectedMethod string,
	expectedURL string,
) (string, error) {
	if err := v.checkHeader(proof); err != nil {
		return "", err
	}
	if proof.Header.Type != dpopType {
		return "", fmt.Errorf("%w: expected typ %q, got %q", ErrDPoPProofInvalid, dpopType, proof.Header.Type)
	}
	if proof.Payload.HTM != expectedMethod {
		return "", fmt.Errorf("%w: htm mismatch: expected %q, got %q", ErrDPoPProofInvalid, expectedMethod, proof.Payload.HTM)
	}
	if proof.Payload.HTU != expectedURL {
		return "", fmt.Errorf("%w: htu mismatch: expected %q, got %q", ErrDPoPProofInvalid, expectedURL, proof.Payload.HTU)
	}
	return v.checkJTIAndFingerprint(proof)
}

// ValidateTokenRequest validates a DPoP proof for a token request.
// It also verifies that the proof was signed by the key bound to the
// authorization code (if applicable).
func (v *DPoPValidator) ValidateTokenRequest(
	proof *DPoPProof,
	expectedMethod string,
	expectedURL string,
	expectedCodeDPoPJKT string,
) (string, error) {
	jkt, err := v.ValidateRequest(proof, expectedMethod, expectedURL)
	if err != nil {
		return "", err
	}
	// If the authorization code was DPoP-bound, the JKT must match.
	if expectedCodeDPoPJKT != "" && jkt != expectedCodeDPoPJKT {
		return "", fmt.Errorf("%w: JKT does not match code binding", ErrDPoPProofInvalid)
	}
	return jkt, nil
}

// ValidateTokenBoundRequest validates a DPoP proof for a resource request
// (i.e. a request that carries an access token). It additionally checks
// the ath (access token hash) claim.
func (v *DPoPValidator) ValidateTokenBoundRequest(
	proof *DPoPProof,
	expectedMethod string,
	expectedURL string,
	accessToken string,
	expectedJKT string,
) error {
	jkt, err := v.ValidateRequest(proof, expectedMethod, expectedURL)
	if err != nil {
		return err
	}
	if expectedJKT != "" && jkt != expectedJKT {
		return fmt.Errorf("%w: JKT does not match access token binding", ErrDPoPProofInvalid)
	}
	// Verify the access token hash.
	expectedATH := AccessTokenHash(accessToken)
	if proof.Payload.ATH != expectedATH {
		return fmt.Errorf("%w: ath mismatch", ErrDPoPProofInvalid)
	}
	return nil
}

// checkHeader performs the common header checks (alg, jwk, signature).
func (v *DPoPValidator) checkHeader(proof *DPoPProof) error {
	if proof.Header.Alg == "" {
		return fmt.Errorf("%w: missing alg in header", ErrDPoPProofInvalid)
	}
	if proof.Header.JWK.Kty == "" {
		return fmt.Errorf("%w: missing jwk in header", ErrDPoPProofInvalid)
	}
	// Verify the signature against the JWK.
	if err := verifyDPoPSignature(proof); err != nil {
		return fmt.Errorf("%w: signature verification: %v", ErrDPoPProofInvalid, err)
	}
	return nil
}

// checkJTIAndFingerprint validates the JTI uniqueness, IAT window, and
// returns the JWK thumbprint.
func (v *DPoPValidator) checkJTIAndFingerprint(proof *DPoPProof) (string, error) {
	// Validate IAT is within the time window.
	if proof.Payload.IAT == 0 {
		return "", fmt.Errorf("%w: missing iat", ErrDPoPProofInvalid)
	}
	iatTime := time.Unix(proof.Payload.IAT, 0)
	if d := time.Since(iatTime); d > DPoPTimeWindow {
		return "", fmt.Errorf("%w: iat is %s in the past", ErrDPoPProofInvalid, d)
	}
	if d := iatTime.Sub(time.Now()); d > DPoPTimeWindow {
		return "", fmt.Errorf("%w: iat is %s in the future", ErrDPoPProofInvalid, d)
	}

	// Check JTI uniqueness.
	if proof.Payload.JTI == "" {
		return "", fmt.Errorf("%w: missing jti", ErrDPoPProofInvalid)
	}
	v.mu.Lock()
	if _, seen := v.usedJTIs[proof.Payload.JTI]; seen {
		v.mu.Unlock()
		return "", fmt.Errorf("%w: duplicate jti %s", ErrDPoPProofInvalid, proof.Payload.JTI)
	}
	v.usedJTIs[proof.Payload.JTI] = time.Now()
	v.mu.Unlock()

	// Compute JWK thumbprint.
	jkt, err := computeDPoPJKT(proof.Header.JWK)
	if err != nil {
		return "", fmt.Errorf("%w: compute JKT: %v", ErrDPoPProofInvalid, err)
	}
	return jkt, nil
}

// CleanupExpiredJTIs removes JTI entries older than the time window.
// Call this periodically (e.g. every 5 minutes) to prevent unbounded growth.
func (v *DPoPValidator) CleanupExpiredJTIs() {
	cutoff := time.Now().Add(-DPoPTimeWindow * 2)
	v.mu.Lock()
	defer v.mu.Unlock()
	for jti, ts := range v.usedJTIs {
		if ts.Before(cutoff) {
			delete(v.usedJTIs, jti)
		}
	}
}
