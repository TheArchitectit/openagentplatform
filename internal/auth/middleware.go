package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey int

const (
	ctxUserKey ctxKey = iota
)

// VerifierMiddleware returns a chi-compatible middleware that authenticates
// requests using an internal session JWT (Bearer or session cookie) or, as a
// fallback, an OIDC ID token in the Authorization header.
func VerifierMiddleware(sm *SessionMinter, v *Verifier, cookieName string) func(http.Handler) http.Handler {
	if cookieName == "" {
		cookieName = "oap_session"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractToken(r, cookieName)
			if tok == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Try internal session JWT first.
			if sm != nil {
				claims := &SessionClaims{}
				parsed, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (interface{}, error) {
					if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
						return nil, jwt.ErrTokenSignatureInvalid
					}
					return sm.signKey.Public(), nil
				})
				if err == nil && parsed.Valid {
					ctx := context.WithValue(r.Context(), ctxUserKey, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Fall back to OIDC ID token verification.
			if v != nil {
				oidcClaims, err := v.Verify(r.Context(), tok)
				if err == nil {
					session := &SessionClaims{
						Email:  oidcClaims.Email,
						Name:   oidcClaims.Name,
						Groups: oidcClaims.Groups,
						OrgID:  oidcClaims.OrgID,
						SiteID: oidcClaims.SiteID,
						Role:   MapGroupsToRole(oidcClaims.Groups),
						RegisteredClaims: jwt.RegisteredClaims{
							Subject: oidcClaims.Subject,
						},
					}
					ctx := context.WithValue(r.Context(), ctxUserKey, session)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
		})
	}
}

// extractToken pulls a bearer token from the Authorization header or cookie.
func extractToken(r *http.Request, cookieName string) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(h, "Bearer ") {
			return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// UserFromContext returns the session claims attached by VerifierMiddleware.
func UserFromContext(ctx context.Context) (*SessionClaims, bool) {
	c, ok := ctx.Value(ctxUserKey).(*SessionClaims)
	return c, ok
}

// RequireRole returns a middleware that allows the request only when the
// authenticated user has one of the listed roles.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if _, allow := allowed[user.Role]; !allow {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
