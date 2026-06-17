// Package oauth — DPoP proof validation (RFC 9449).
//
// DPoP (Demonstrating Proof-of-Possession) binds an access token to a
// specific public key. The client proves possession of the corresponding
// private key by signing a JWT-like proof that includes the HTTP method,
// URL, timestamp, and (for token requests) the access token hash.
//
// This file implements the server-side DPoP proof validator.
package oauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/big"
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
	hash := sha256.Sum256([]byte(accessToken))
	expectedATH := base64.RawURLEncoding.EncodeToString(hash[:])
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

// --- DPoP Signature Verification ---

// verifyDPoPSignature verifies the signature in a DPoP proof using the
// JWK in the header.
func verifyDPoPSignature(proof *DPoPProof) error {
	signingInput := proof.RawHeader + "." + proof.RawPayload

	switch proof.Header.JWK.Kty {
	case "EC":
		return verifyECDSASignature(proof, signingInput)
	case "RSA":
		return verifyRSASignature(proof, signingInput)
	default:
		return fmt.Errorf("unsupported key type: %s", proof.Header.JWK.Kty)
	}
}

// verifyECDSASignature verifies an ECDSA-signed DPoP proof.
func verifyECDSASignature(proof *DPoPProof, signingInput string) error {
	var curve elliptic.Curve
	switch proof.Header.JWK.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return fmt.Errorf("unsupported curve: %s", proof.Header.JWK.Crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(proof.Header.JWK.X)
	if err != nil {
		return fmt.Errorf("decode x: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(proof.Header.JWK.Y)
	if err != nil {
		return fmt.Errorf("decode y: %w", err)
	}

	pubKey := &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}

	hashID, err := hashAlgo(proof.Header.Alg)
	if err != nil {
		return err
	}
	digest := hashID.New()
	digest.Write([]byte(signingInput))
	digestSum := digest.Sum(nil)

	// For ECDSA, the signature may be in ASN.1 DER or raw r||s format.
	// We try ASN.1 first, then fall back to raw concatenation.
	if ecdsa.VerifyASN1(pubKey, digestSum, proof.Signature) {
		return nil
	}

	// Try raw r||s format (ES256: 32 bytes each for P-256).
	coordSize := (curve.Params().BitSize + 7) / 8
	if len(proof.Signature) == 2*coordSize {
		r := new(big.Int).SetBytes(proof.Signature[:coordSize])
		s := new(big.Int).SetBytes(proof.Signature[coordSize:])
		if ecdsa.Verify(pubKey, digestSum, r, s) {
			return nil
		}
	}

	return errors.New("ECDSA signature verification failed")
}

// verifyRSASignature verifies an RSA-signed DPoP proof.
func verifyRSASignature(proof *DPoPProof, signingInput string) error {
	nBytes, err := base64.RawURLEncoding.DecodeString(proof.Header.JWK.N)
	if err != nil {
		return fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(proof.Header.JWK.E)
	if err != nil {
		return fmt.Errorf("decode e: %w", err)
	}

	pubKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}

	hashID, err := hashAlgo(proof.Header.Alg)
	if err != nil {
		return err
	}
	digest := hashID.New()
	digest.Write([]byte(signingInput))
	digestSum := digest.Sum(nil)

	switch proof.Header.Alg {
	case "PS256", "PS384", "PS512":
		opts := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}
		return rsa.VerifyPSS(pubKey, hashID, digestSum, proof.Signature, opts)
	case "RS256", "RS384", "RS512":
		return rsa.VerifyPKCS1v15(pubKey, hashID, digestSum, proof.Signature)
	default:
		return fmt.Errorf("unsupported RSA alg: %s", proof.Header.Alg)
	}
}

// hashAlgo returns the crypto.Hash for the given algorithm name.
func hashAlgo(alg string) (crypto.Hash, error) {
	switch alg {
	case "ES256", "PS256", "RS256":
		return crypto.SHA256, nil
	case "ES384", "PS384", "RS384":
		return crypto.SHA384, nil
	case "ES512", "PS512", "RS512":
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("unsupported alg: %s", alg)
	}
}

// computeDPoPJKT computes the JWK thumbprint per RFC 9449 / RFC 7638.
// For EC keys: kty, crv, x, y (lexicographic order).
// For RSA keys: kty, e, n (lexicographic order).
func computeDPoPJKT(jwk DPoPJWK) (string, error) {
	var members map[string]string
	switch jwk.Kty {
	case "EC":
		members = map[string]string{
			"crv": jwk.Crv,
			"kty": jwk.Kty,
			"x":   jwk.X,
			"y":   jwk.Y,
		}
	case "RSA":
		members = map[string]string{
			"e":   jwk.E,
			"kty": jwk.Kty,
			"n":   jwk.N,
		}
	default:
		return "", fmt.Errorf("unsupported key type: %s", jwk.Kty)
	}
	// Encode as a canonical JSON object with sorted keys.
	canonical, err := canonicalJSONEncode(members)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(h[:]), nil
}

// canonicalJSONEncode encodes a map[string]string as a JSON object with
// keys in lexicographic order (required for JWK thumbprint).
func canonicalJSONEncode(m map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sort lexicographically using bubble sort (no import needed).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}

	// Build the JSON manually to guarantee key ordering.
	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		valJSON, err := json.Marshal(m[k])
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return []byte(buf.String()), nil
}

// AccessTokenHash computes the SHA-256 hash of an access token for the
// ath claim in DPoP proofs.
func AccessTokenHash(accessToken string) string {
	h := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// DPoPJKTFromJWK is a convenience function that computes the JWK thumbprint.
func DPoPJKTFromJWK(jwk DPoPJWK) (string, error) {
	return computeDPoPJKT(jwk)
}

// hexJKT returns the hex-encoded JWK thumbprint (for logging).
func hexJKT(jkt string) string {
	b, err := base64.RawURLEncoding.DecodeString(jkt)
	if err != nil {
		return jkt
	}
	return hex.EncodeToString(b)
}

// Ensure hash is referenced (for future use of additional hash algorithms).
var _ = hash.Hash(nil)
