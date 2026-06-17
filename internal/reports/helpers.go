// Package reports - helpers.go provides shared utility functions
// for the reports package (HMAC signing, base64 encoding).
package reports

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// hmacSum computes HMAC-SHA256(secret, message) and returns it
// as a hex-encoded string.
func hmacSum(secret []byte, message string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// base64URLEncode returns the URL-safe base64 encoding of s.
func base64URLEncode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
