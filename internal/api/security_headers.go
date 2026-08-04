package api

import "net/http"

// securityHeadersMiddleware sets browser security response headers that are
// missing from the default stack: MIME-sniffing protection, clickjacking
// protection, referrer policy, and HSTS when the connection is TLS.
//
// Previously the API set none of these (verified by grep for the header
// names across internal/ and cmd/), leaving the admin console frameable
// (clickjacking) and JSON responses MIME-sniffable (content-type confusion).
//
// HSTS is emitted only when r.TLS != nil (the server terminates TLS
// directly). Behind a TLS-terminating proxy, set HSTS at the proxy layer to
// avoid sending it over a plaintext hop.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// A minimal CSP that denies framing and blocks inline where the SPA
		// doesn't need it. React/Vite apps generally rely on inline styles
		// injected by styled tooling, so we do not set a restrictive
		// default-src here; the framing/MIME protections above are the
		// high-value ones. Operators with a known CSP policy should set it
		// at the proxy.
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
