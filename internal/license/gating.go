package license

import (
	"net/http"
)

// FeatureGate is a chi middleware that rejects a request with HTTP 402
// Payment Required if the active license does not include the
// specified feature. The license is read from the request context; it
// must have been placed there by LicenseMiddleware (or a test helper).
//
// Use the WithFeature wrapper on the route group to declare the
// required feature, or call FeatureGate("feature_name") directly if the
// feature is fixed at registration time.
func FeatureGate(requiredFeature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lic, _ := LicenseFromContext(r.Context())
			if lic == nil {
				// No license loaded — treat as community; gate the feature.
				writeFeatureRequired(w, requiredFeature, "community")
				return
			}
			if lic.IsExpired() && !lic.IsInGracePeriod() {
				writeLicenseExpired(w)
				return
			}
			if !lic.HasFeature(requiredFeature) {
				writeFeatureRequired(w, requiredFeature, string(lic.Tier))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithFeature returns a chi middleware that sets the required-feature
// context value for downstream handlers and middleware. Use it on a
// route group to attach a feature gate:
//
//	r.With(license.WithFeature("enterprise_sso")).Get("/sso", handler)
//
// Pair it with GateFromContextFeature to resolve the feature inside a
// single FeatureGate("dynamic") registration.
func WithFeature(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := withFeatureValue(r.Context(), name)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GateFromContextFeature is a FeatureGate that reads the required
// feature name from the context (set by WithFeature) instead of a
// compile-time constant. Useful for shared route groups where the
// feature depends on the specific route.
func GateFromContextFeature(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feature, ok := FeatureFromContext(r.Context())
		if !ok || feature == "" {
			// No feature declared — pass through; the route is not
			// gated.
			next.ServeHTTP(w, r)
			return
		}
		FeatureGate(feature)(next).ServeHTTP(w, r)
	})
}

// writeFeatureRequired writes a 402 response with a structured JSON
// body so the UI can render an actionable upgrade prompt.
func writeFeatureRequired(w http.ResponseWriter, feature, currentTier string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	body := `{"error":"feature_gated","feature":"` + feature + `","tier":"` + currentTier + `","message":"This feature requires a commercial license. Contact sales@openagentplatform.io to upgrade."}`
	_, _ = w.Write([]byte(body))
}

// writeLicenseExpired writes a 402 response indicating the license has
// passed the grace period and must be renewed.
func writeLicenseExpired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = w.Write([]byte(`{"error":"license_expired","message":"License has expired and the grace period has elapsed. Renew at openagentplatform.io/billing."}`))
}
