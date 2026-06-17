package tenancy

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/openagentplatform/openagentplatform/internal/license"
)

// QuotaResource identifies a quota-able resource type.
type QuotaResource string

const (
	QuotaAgents  QuotaResource = "agents"
	QuotaUsers   QuotaResource = "users"
	QuotaSites   QuotaResource = "sites"
	QuotaAPICall QuotaResource = "api_call"
	QuotaA2A     QuotaResource = "a2a_call"
)

// QuotaDefinition describes the hard limits for a single tier.
// Zero values are treated as "unlimited" for collection-style limits
// (agents, users, sites).  Rate-style limits (API per hour, A2A per
// day) use -1 to mean "unlimited" because 0 is a valid (if useless)
// rate.
type QuotaDefinition struct {
	Tier              license.Tier
	MaxAgents         int
	MaxUsers          int
	MaxSites          int
	MaxAPIReqPerHour  int // -1 = unlimited
	MaxA2ACallsPerDay int // -1 = unlimited
	RetentionDays     int // audit/check-result retention in days
}

// TierQuotas maps tiers to their quota definitions.  These are the
// canonical limits and must agree with internal/license and
// internal/billing.
var TierQuotas = map[license.Tier]QuotaDefinition{
	license.TierCommunity: {
		Tier:              license.TierCommunity,
		MaxAgents:         10,
		MaxUsers:          2,
		MaxSites:          1,
		MaxAPIReqPerHour:  1000,
		MaxA2ACallsPerDay: 100,
		RetentionDays:     30,
	},
	license.TierProfessional: {
		Tier:              license.TierProfessional,
		MaxAgents:         100,
		MaxUsers:          10,
		MaxSites:          5,
		MaxAPIReqPerHour:  10000,
		MaxA2ACallsPerDay: 1000,
		RetentionDays:     90,
	},
	license.TierEnterprise: {
		Tier:              license.TierEnterprise,
		MaxAgents:         0,  // unlimited
		MaxUsers:          0,  // unlimited
		MaxSites:          0,  // unlimited
		MaxAPIReqPerHour:  100000,
		MaxA2ACallsPerDay: -1, // unlimited
		RetentionDays:     365,
	},
}

// QuotasForTier returns a QuotaSnapshot suitable for embedding in a
// TenantContext.  Tier strings that are not recognised fall back to
// Community limits.
func QuotasForTier(tier license.Tier) QuotaSnapshot {
	qd, ok := TierQuotas[tier]
	if !ok {
		qd = TierQuotas[license.TierCommunity]
	}
	return QuotaSnapshot{
		MaxAgents:         qd.MaxAgents,
		MaxUsers:          qd.MaxUsers,
		MaxSites:          qd.MaxSites,
		MaxAPIReqPerHour:  qd.MaxAPIReqPerHour,
		MaxA2ACallsPerDay: qd.MaxA2ACallsPerDay,
	}
}

// QuotaError is returned by EnforceQuota when a tenant exceeds a limit.
// It carries enough metadata for the caller to produce a structured
// 429 response with a Retry-After header and quota details.
type QuotaError struct {
	Resource    QuotaResource
	Limit       int64
	Current     int64
	RetryAfter  int // seconds
	Tier        license.Tier
}

// Error implements the error interface.
func (e *QuotaError) Error() string {
	return fmt.Sprintf("quota exceeded for %s on tier %s: %d/%d",
		e.Resource, e.Tier, e.Current, e.Limit)
}

// EnforceQuota checks whether a tenant has exceeded the limit for the
// given resource.  current is the actual usage (e.g. agent count, API
// call count in the current window).  When the limit is zero (collection
// limit) or -1 (rate limit) the resource is unlimited and the function
// always returns nil.  When the limit is exceeded, a *QuotaError is
// returned; the caller can inspect it to build a 429 response.
//
// The retryAfterSeconds hint is used to set the Retry-After header on
// the resulting HTTP response.  For collection-style limits (agents,
// users, sites) there is no automatic recovery window, so the caller
// should pass 0 and the header is omitted.  For rate limits the caller
// should pass the number of seconds until the window resets.
func EnforceQuota(tc *TenantContext, resource QuotaResource, current int64, retryAfterSeconds int) error {
	if tc == nil {
		return nil // no tenant context means no enforcement
	}
	qd, ok := TierQuotas[tc.Tier]
	if !ok {
		qd = TierQuotas[license.TierCommunity]
	}

	var limit int64
	switch resource {
	case QuotaAgents:
		if qd.MaxAgents == 0 {
			return nil
		}
		limit = int64(qd.MaxAgents)
	case QuotaUsers:
		if qd.MaxUsers == 0 {
			return nil
		}
		limit = int64(qd.MaxUsers)
	case QuotaSites:
		if qd.MaxSites == 0 {
			return nil
		}
		limit = int64(qd.MaxSites)
	case QuotaAPICall:
		if qd.MaxAPIReqPerHour == -1 {
			return nil
		}
		limit = int64(qd.MaxAPIReqPerHour)
	case QuotaA2A:
		if qd.MaxA2ACallsPerDay == -1 {
			return nil
		}
		limit = int64(qd.MaxA2ACallsPerDay)
	default:
		return nil
	}

	if current >= limit {
		return &QuotaError{
			Resource:   resource,
			Limit:      limit,
			Current:    current,
			RetryAfter: retryAfterSeconds,
			Tier:       tc.Tier,
		}
	}
	return nil
}

// WriteQuotaResponse writes a structured 429 response for the given
// *QuotaError.  It sets the Retry-After header when retryAfter > 0 and
// includes the resource, limit, and current usage in the JSON body so
// clients can present actionable error messages to the operator.
func WriteQuotaResponse(w http.ResponseWriter, err *QuotaError) {
	if err == nil {
		return
	}
	if err.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(err.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	body := fmt.Sprintf(
		`{"error":"quota_exceeded","resource":"%s","limit":%d,"current":%d,"tier":"%s","retry_after":%d}`,
		err.Resource, err.Limit, err.Current, err.Tier, err.RetryAfter,
	)
	_, _ = w.Write([]byte(body))
}

// QuotaMiddleware is a convenience HTTP middleware that reads the
// TenantContext and rejects requests with 429 when the per-tenant
// rate-limit (API requests per hour) is exceeded.  It is intended to
// be mounted after TenantMiddleware and before the rate limiter so
// that tenant-level caps take precedence over the global rate limit.
func QuotaMiddleware(currentUsage func(orgID string) int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := GetTenant(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			current := int64(0)
			if currentUsage != nil {
				current = currentUsage(tc.OrgID)
			}
			if qerr := EnforceQuota(tc, QuotaAPICall, current, 3600); qerr != nil {
				if qe, ok := qerr.(*QuotaError); ok {
					WriteQuotaResponse(w, qe)
				} else {
					http.Error(w, `{"error":"quota check failed"}`, http.StatusInternalServerError)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
