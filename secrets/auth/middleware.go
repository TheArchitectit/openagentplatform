// Package auth provides A2A authentication token management.
// This file implements HTTP middleware for token verification and scope enforcement.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// ContextKey is a type used for context value keys to avoid collisions.
type ContextKey string

// ClaimsContextKey is the context key under which verified *TokenClaims are stored.
const ClaimsContextKey ContextKey = "a2a_claims"

// claimsFromContext retrieves the verified token claims from a request context.
// Returns nil if no claims are present.
func claimsFromContext(ctx context.Context) *TokenClaims {
	v, ok := ctx.Value(ClaimsContextKey).(*TokenClaims)
	if !ok {
		return nil
	}
	return v
}

// A2AAuthMiddleware returns an HTTP middleware that extracts a Bearer token from
// the Authorization header, verifies it via the TokenIssuer, and injects the
// resulting *TokenClaims into the request context under ClaimsContextKey.
//
// On failure, responds with 401 Unauthorized and a JSON error body.
// On success, the next handler in the chain is called.
func A2AAuthMiddleware(issuer *TokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, http.StatusUnauthorized, "missing Authorization header")
				return
			}

			// Extract Bearer token.
			const prefix = "Bearer "
			if !strings.HasPrefix(authHeader, prefix) {
				writeAuthError(w, http.StatusUnauthorized, "Authorization header must use Bearer scheme")
				return
			}
			tokenStr := strings.TrimSpace(authHeader[len(prefix):])
			if tokenStr == "" {
				writeAuthError(w, http.StatusUnauthorized, "empty Bearer token")
				return
			}

			// Verify token.
			claims, err := issuer.Verify(tokenStr)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "token verification failed: "+err.Error())
				return
			}

			// Inject claims into context.
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope returns a middleware that ensures the verified token in the request
// context has all of the specified scopes. If the token lacks required scopes,
// responds with 403 Forbidden.
//
// This middleware MUST be chained after A2AAuthMiddleware so that claims are
// already present in the context.
func RequireScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := claimsFromContext(r.Context())
			if claims == nil {
				writeAuthError(w, http.StatusUnauthorized, "no verified token claims in context")
				return
			}

			if !Matches(scopes, claims.Scopes) {
				writeAuthError(w, http.StatusForbidden, "insufficient scope")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireScopeFunc returns an http.HandlerFunc wrapper that checks scope requirements.
// This is a convenience for cases where a middleware-style wrapper is not ideal.
func RequireScopeFunc(handler http.HandlerFunc, scopes ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r.Context())
		if claims == nil {
			writeAuthError(w, http.StatusUnauthorized, "no verified token claims in context")
			return
		}

		if !Matches(scopes, claims.Scopes) {
			writeAuthError(w, http.StatusForbidden, "insufficient scope")
			return
		}

		handler(w, r)
	}
}

// writeAuthError writes a JSON error response.
func writeAuthError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
