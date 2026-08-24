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
	"sort"
	"strings"
)

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
	// Sort lexicographically.
	sort.Strings(keys)

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
