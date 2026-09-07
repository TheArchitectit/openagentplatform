package edr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// verifyHMAC checks whether a webhook signature is valid for the given
// payload. EDR vendors vary in how they encode the signature: CrowdStrike
// uses "timestamp=...,signature=<hex>" while Defender uses a raw hex
// digest. We try the common shapes.
func verifyHMAC(payload []byte, signature, secret string) bool {
	candidates := []string{signature}
	if idx := strings.Index(signature, "="); idx >= 0 {
		candidates = append(candidates, signature[idx+1:])
	}
	for _, sig := range candidates {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		expected := hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(expected), []byte(sig)) {
			return true
		}
	}
	return false
}
