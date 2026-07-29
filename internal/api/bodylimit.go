package api

import (
	"net/http"
)

// maxRequestBodyBytes caps the size of inbound request bodies to protect
// against memory-exhaustion DoS (e.g. arbitrarily large script bodies or
// JSON payloads). 2 MiB is well above any legitimate single-request payload
// in this API while bounding per-request allocation.
const maxRequestBodyBytes = 2 << 20 // 2 MiB

// bodyLimitMiddleware wraps r.Body in an http.MaxBytesReader so a handler
// that calls json.Decode (or io.ReadAll) on an oversized body gets an error
// and the connection is closed, rather than buffering an unbounded payload.
//
// WebSocket upgrade requests (/ws and the shell WS bridge) and multipart
// uploads are excluded if needed in the future; today every mutating handler
// decodes JSON, so a single global cap is appropriate.
func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MaxBytesReader also sets a flag that triggers http.CloseConnection
		// once the limit is exceeded, defending against slow-send attackers.
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}
