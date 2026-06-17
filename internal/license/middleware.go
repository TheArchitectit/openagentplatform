package license

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// licenseCtxKey is the context key under which the resolved *License
// is stored for the lifetime of a request.
type licenseCtxKey struct{}

// LicenseStore abstracts the persistence layer that LicenseMiddleware
// uses to load a license for a given org. It is satisfied by a thin
// pgxpool wrapper (see PostgresLicenseStore) and by in-memory fakes
// in tests.
type LicenseStore interface {
	GetLicenseByOrg(ctx context.Context, orgID string) (string, error) // returns signed key string
}

// PostgresLicenseStore is the production LicenseStore backed by pgxpool.
type PostgresLicenseStore struct {
	Pool *pgxpool.Pool
}

// GetLicenseByOrg fetches the active license key for an org from the
// licenses table. Returns ErrNoLicense if no row is present.
func (s *PostgresLicenseStore) GetLicenseByOrg(ctx context.Context, orgID string) (string, error) {
	const query = `SELECT license_key FROM licenses WHERE org_id = $1 AND revoked = FALSE ORDER BY issued_at DESC LIMIT 1`
	var key string
	err := s.Pool.QueryRow(ctx, query, orgID).Scan(&key)
	if err != nil {
		return "", fmt.Errorf("license: load for org %q: %w", orgID, err)
	}
	return key, nil
}

// ErrNoLicense is returned by LicenseStore when an org has no license on file.
var ErrNoLicense = errors.New("license: no license on file for org")

// orgIDExtractor pulls the org ID from the request. By default we look
// for the "X-Org-ID" header; the route may also be wrapped with
// WithOrgIDResolver to set a custom resolver (e.g. extracting from a
// JWT claim in auth middleware).
var orgIDExtractor = func(r *http.Request) string {
	return r.Header.Get("X-Org-ID")
}

// WithOrgIDResolver overrides the default org-ID extractor.
func WithOrgIDResolver(fn func(r *http.Request) string) {
	orgIDExtractor = fn
}

// LicenseMiddleware loads the org's license, verifies it, and stores
// the *License on the request context. If the org has no license on
// file the request continues with no license in context (i.e. community
// tier); feature gates further down the chain will then reject paid
// features with 402.
func LicenseMiddleware(store LicenseStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID := orgIDExtractor(r)
			if orgID == "" {
				// No org header — let the request through; downstream
				// auth middleware will reject it if the route requires
				// authentication.
				next.ServeHTTP(w, r)
				return
			}
			key, err := store.GetLicenseByOrg(r.Context(), orgID)
			if err != nil {
				if errors.Is(err, ErrNoLicense) {
					// No license on file — community tier.
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "license store error", http.StatusInternalServerError)
				return
			}
			lic, err := ParseKey(key)
			if err != nil {
				// Tampered, expired beyond grace, or corrupt key. Do
				// not 500 — log and fall through as community tier.
				// The license is stale; we do not want to block the
				// org entirely, only gate paid features.
				next.ServeHTTP(w, r)
				return
			}
			ctx := contextWithLicense(r.Context(), lic)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// contextWithLicense attaches a *License to the given context.
func contextWithLicense(parent context.Context, lic *License) context.Context {
	return context.WithValue(parent, licenseCtxKey{}, lic)
}

// LicenseFromContext returns the license attached to the request
// context by LicenseMiddleware. Returns nil if none is present.
func LicenseFromContext(ctx context.Context) (*License, bool) {
	lic, ok := ctx.Value(licenseCtxKey{}).(*License)
	return lic, ok
}

// GetLicense is the package-level accessor used by application code
// outside the HTTP request path (e.g. background jobs). It returns
// the license if one is in context, or nil/false.
func GetLicense(ctx context.Context) (*License, bool) {
	return LicenseFromContext(ctx)
}

// Resource identifies a quota-tracked resource. Used by EnforceQuota.
type Resource string

const (
	ResourceAgents Resource = "agents"
	ResourceUsers  Resource = "users"
	ResourceSites  Resource = "sites"
)

// maxForResource returns the license's max for the given resource.
// Zero is treated as "unlimited" (a convention used by community
// tier for non-commercial resources).
func (l *License) maxForResource(r Resource) int {
	if l == nil {
		return 0
	}
	switch r {
	case ResourceAgents:
		return l.MaxAgents
	case ResourceUsers:
		return l.MaxUsers
	case ResourceSites:
		return l.MaxSites
	default:
		return 0
	}
}

// EnforceQuota checks whether `current` for `resource` is within the
// license's limit. If `max` is zero it is treated as unlimited and the
// check always passes. Returns nil when within quota, or an
// QuotaExceededError otherwise. This function does not write HTTP
// responses — it returns an error that the caller (typically a handler)
// can translate into a 403.
func EnforceQuota(ctx context.Context, resource Resource, current int) error {
	lic, ok := GetLicense(ctx)
	if !ok || lic == nil {
		// Community tier: hard cap of 1 site, 3 users, 5 agents.
		limit := communityCap(resource)
		if limit > 0 && current >= limit {
			return QuotaExceededError{Resource: resource, Current: current, Max: limit}
		}
		return nil
	}
	if lic.IsExpired() && !lic.IsInGracePeriod() {
		// Treat expired-beyond-grace as community.
		limit := communityCap(resource)
		if limit > 0 && current >= limit {
			return QuotaExceededError{Resource: resource, Current: current, Max: limit}
		}
		return nil
	}
	max := lic.maxForResource(resource)
	if max > 0 && current >= max {
		return QuotaExceededError{Resource: resource, Current: current, Max: max}
	}
	return nil
}

// QuotaExceededError is returned by EnforceQuota when a resource would
// exceed the license limit. The HTTP layer can errors.As it to produce
// a structured 403 response.
type QuotaExceededError struct {
	Resource Resource
	Current  int
	Max      int
}

func (e QuotaExceededError) Error() string {
	return fmt.Sprintf("license: %s quota exceeded (%d/%d)", e.Resource, e.Current, e.Max)
}

// communityCap returns the hard-coded cap for community tier per
// resource. 0 means "not capped / not applicable".
func communityCap(r Resource) int {
	switch r {
	case ResourceAgents:
		return 5
	case ResourceUsers:
		return 3
	case ResourceSites:
		return 1
	default:
		return 0
	}
}

// featureKey is the context key used by the WithFeature / FeatureGate
// pairing. Defined here so the two files share a single key type.
type featureKey struct{}

// withFeatureValue stores the required feature name on the context.
func withFeatureValue(parent context.Context, name string) context.Context {
	return context.WithValue(parent, featureKey{}, name)
}

// FeatureFromContext returns the feature name previously stored by
// WithFeature, and whether one was found.
func FeatureFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(featureKey{}).(string)
	return name, ok
}

// JSONLicense serialises a license to JSON for storage or display.
// The Signature field is included; the public key is not.
func JSONLicense(l *License) ([]byte, error) {
	if l == nil {
		return []byte("null"), nil
	}
	type wire struct {
		ID        string    `json:"license_id"`
		OrgID     string    `json:"org_id"`
		Tier      Tier      `json:"tier"`
		MaxAgents int       `json:"max_agents"`
		MaxUsers  int       `json:"max_users"`
		MaxSites  int       `json:"max_sites"`
		Features  []string  `json:"features"`
		IssuedAt  time.Time `json:"issued_at"`
		ExpiresAt time.Time `json:"expires_at"`
		Signature string    `json:"signature,omitempty"`
	}
	sig := ""
	if len(l.Signature) > 0 {
		sig = base64RawURLEncode(l.Signature)
	}
	return json.Marshal(wire{
		ID:        l.ID,
		OrgID:     l.OrgID,
		Tier:      l.Tier,
		MaxAgents: l.MaxAgents,
		MaxUsers:  l.MaxUsers,
		MaxSites:  l.MaxSites,
		Features:  l.Features,
		IssuedAt:  l.IssuedAt,
		ExpiresAt: l.ExpiresAt,
		Signature: sig,
	})
}
