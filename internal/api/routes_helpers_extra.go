package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/license"
	"github.com/openagentplatform/openagentplatform/internal/licensing"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
)

// clientIP is duplicated from the audit middleware so the auth handlers
// (which run before middleware-injected request IDs) can still attribute
// the event to a client. chi's RealIP middleware sets X-Forwarded-For /
// X-Real-IP, so we honour those here too.
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if comma := strings.Index(h, ","); comma >= 0 {
			return strings.TrimSpace(h[:comma])
		}
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return strings.TrimSpace(h)
	}
	return r.RemoteAddr
}

// licenseContextMiddleware injects a *licensing.License into the request
// context so the licensing.Gater's RequireFeature/RequireTier middleware can
// evaluate entitlements. The license is derived from the server's tier
// resolver (the same source the tenancy middleware uses for quota limits),
// so tier-gated routes see the resolved commercial tier. It must run AFTER
// orgContextMiddleware so the org ID is available.
func (s *Server) licenseContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.gater == nil {
			http.Error(w, `{"error":"feature_gating_not_configured"}`, http.StatusServiceUnavailable)
			return
		}
		tier := license.TierCommunity
		if s.tierResolver != nil {
			if t := s.tierResolver(orgIDFromContext(r)); t != "" {
				tier = t
			}
		}
		lic := &licensing.License{
			Entity:        "openagentplatform",
			Tier:          mapLicenseTier(tier),
			Features:      licensingFeaturesForTier(tier),
			EndpointLimit: 0,
			IssueDate:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			ExpiryDate:    time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC),
		}
		ctx := context.WithValue(r.Context(), licensing.LicenseContextKey, lic)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// mapLicenseTier maps the internal license.Tier vocabulary onto the
// licensing package's tier vocabulary.
func mapLicenseTier(t license.Tier) licensing.Tier {
	switch t {
	case license.TierProfessional:
		return licensing.TierPro
	case license.TierEnterprise:
		return licensing.TierEnterprise
	default:
		return licensing.TierCommunity
	}
}

// licensingFeaturesForTier returns the canonical feature set for a tier.
// It mirrors tenancy.featureFlagsForTier: higher tiers gain additional
// capabilities.
func licensingFeaturesForTier(t license.Tier) []licensing.Feature {
	switch t {
	case license.TierProfessional:
		return []licensing.Feature{
			licensing.FeatureMultiTenancy,
			licensing.FeatureBilling,
			licensing.FeatureAuditExport,
			licensing.FeatureAlertSuppressionWindows,
		}
	case license.TierEnterprise:
		return []licensing.Feature{
			licensing.FeatureMultiTenancy,
			licensing.FeatureBilling,
			licensing.FeatureAuditExport,
			licensing.FeatureAlertSuppressionWindows,
			licensing.FeatureManagedRelay,
			licensing.FeatureEnterpriseReporting,
			licensing.FeatureSSO,
		}
	default:
		return []licensing.Feature{}
	}
}

// orgContextMiddleware ensures every authenticated request carries an OrgID
// in its session claims. If no org context is present, the request is
// rejected with 400. This enforces multi-tenant isolation: every API call
// must be scoped to the caller's organization.
func orgContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.UserFromContext(r.Context())
		if !ok || claims == nil {
			// No claims means the request is unauthenticated; the auth
			// middleware should have already rejected it.
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if claims.OrgID == "" {
			http.Error(w, `{"error":"org context required"}`, http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps http.ResponseWriter so we can capture the status
// code for metrics emission.  The default http.ResponseWriter does not
// expose the status once it has been written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// routeLabel returns the chi route pattern for the current request, or
// "unmatched" when the request did not match a registered route.  This is
// what we expose as the "path" label on api_requests_total so we avoid
// high-cardinality URL explosions.
func routeLabel(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

// metricsMiddleware records request count and duration for every request
// handled by the API.  It should be installed near the top of the
// middleware stack so it captures all responses, including 401s and
// 500s.  The /metrics endpoint itself is excluded to keep the scrape
// from polluting the request rate.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't count scrapes of the metrics endpoint itself.
		if r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/api/v1/metrics") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		path := routeLabel(r)
		status := strconv.Itoa(rec.status)
		telemetry.RecordAPIRequest(r.Method, path, status)
		// Feed the JSON-summary snapshot so /api/v1/metrics/summary shows
		// live request roll-ups instead of an empty document.
		telemetry.RecordCounterRollup("api_requests_total", 1)
		telemetry.ObserveHTTPRequestDuration(r.Method, path, time.Since(start).Seconds())
	})
}

// recordLogin writes a "login" audit event for a successful OIDC callback.
// Failures are logged but do not block the response.
func (s *Server) recordLogin(r *http.Request, claims *auth.Claims) {
	if s.audit == nil || claims == nil {
		return
	}
	// Use a detached context so the audit write survives the request
	// being cancelled by the browser navigating away.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      claims.Subject,
		Action:       string(audit.EventLogin),
		ResourceType: "session",
		ResourceID:   claims.Subject,
		Details: map[string]any{
			"email": claims.Email,
			"role":  auth.MapGroupsToRole(claims.Groups),
		},
		Outcome:   audit.OutcomeSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		OrgID:     claims.OrgID,
		SiteID:    claims.SiteID,
	})
	if err != nil {
		s.log.Error("audit: login record failed", "err", err)
	}
}

// recordLogout writes a "logout" audit event. We try to attribute the event
// to the authenticated user, but fall back to "unknown" if the session has
// already been invalidated.
func (s *Server) recordLogout(r *http.Request) {
	if s.audit == nil {
		return
	}
	actorID := ""
	orgID := ""
	siteID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		actorID = claims.Subject
		orgID = claims.OrgID
		siteID = claims.SiteID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actorID,
		Action:       string(audit.EventLogout),
		ResourceType: "session",
		ResourceID:   actorID,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
		SiteID:       siteID,
	})
	if err != nil {
		s.log.Error("audit: logout record failed", "err", err)
	}
}
