package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/bootstrap"
)

// bootstrapRequest is the POST body for /api/v1/auth/bootstrap.
type bootstrapRequest struct {
	Token   string `json:"token"`
	OrgName string `json:"org_name"`
	OrgSlug string `json:"org_slug"`
}

// handleBootstrap is the one-time first-boot org + admin claim
// (auth-rbac spec §14). The caller MUST already hold a valid session (they
// authenticated through the IdP); this endpoint binds them to a brand-new
// org. Gated three ways:
//   - cfg.BootstrapToken empty        → 404 bootstrap_disabled (§14.2)
//   - store unwired                   → 503 store_unavailable
//   - bootstrap_complete latch already set → 403 already_initialized (§14.3)
//
// It runs OUTSIDE the orgContextMiddleware group (an unbootstrapped caller
// has no org yet — that is precisely the gap it closes).
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		// Defensive: the mounted auth middleware already rejects these.
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if s.cfg.BootstrapToken == "" {
		s.recordBootstrap(r, user, "", audit.OutcomeFailure, "bootstrap_disabled")
		http.Error(w, `{"error":"bootstrap_disabled"}`, http.StatusNotFound)
		return
	}
	if s.bootstrapStore == nil {
		s.recordBootstrap(r, user, "", audit.OutcomeFailure, "store_unavailable")
		http.Error(w, `{"error":"store_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var req bootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.recordBootstrap(r, user, "", audit.OutcomeFailure, "invalid_json")
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	orgID, created, err := s.bootstrapStore.Claim(ctx, req.Token, s.cfg.BootstrapToken, req.OrgName, req.OrgSlug, user.Subject)
	switch {
	case errors.Is(err, bootstrap.ErrInvalidToken):
		s.recordBootstrap(r, user, req.OrgSlug, audit.OutcomeFailure, "invalid_token")
		http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
		return
	case errors.Is(err, bootstrap.ErrAlreadyInitialized):
		s.recordBootstrap(r, user, req.OrgSlug, audit.OutcomeFailure, "already_initialized")
		http.Error(w, `{"error":"already_initialized"}`, http.StatusForbidden)
		return
	case err != nil:
		s.log.Error("bootstrap claim failed", "err", err)
		s.recordBootstrap(r, user, req.OrgSlug, audit.OutcomeFailure, "error")
		http.Error(w, `{"error":"bootstrap_failed"}`, http.StatusInternalServerError)
		return
	}

	// Bind the caller's just-minted session to the new org so their next
	// request is org-scoped without a re-login round-trip: the store recorded
	// the binding; the client refreshes to pick up the org_id claim (§14.4b).
	if created {
		s.recordBootstrap(r, user, req.OrgSlug, audit.OutcomeSuccess, "created")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "initialized",
			"org_id":   orgID,
			"org_slug": req.OrgSlug,
		})
		return
	}
	http.Error(w, `{"error":"bootstrap_failed"}`, http.StatusInternalServerError)
}

// recordBootstrap writes an audit event for a bootstrap attempt (§14.6).
// The token is never included. Failures are logged, not surfaced.
func (s *Server) recordBootstrap(r *http.Request, user *auth.SessionClaims, orgSlug string, outcome audit.Outcome, reason string) {
	if s.audit == nil || user == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      user.Subject,
		Action:       string(audit.EventConfigChange),
		ResourceType: "bootstrap",
		ResourceID:   orgSlug,
		Details: map[string]any{
			"email":  user.Email,
			"reason": reason,
		},
		Outcome:   outcome,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		OrgID:     user.OrgID,
	})
	if err != nil {
		s.log.Error("audit: bootstrap record failed", "err", err)
	}
}

// resolveOrgForSubject implements auth-rbac spec §14.4 login org resolution,
// returning (orgID, role). It is best-effort: any store error degrades to a
// session with whatever the ID token carried (org stays empty → org-scoped
// routes keep 400), never blocking login. Order:
//
//	a) explicit org_id claim  → keep, role from groups (already set by Mint)
//	b) user_org_bindings row  → that org + bound role
//	c) exactly one live org   → auto-bind to it (beta single-org convenience)
//	d) otherwise              → empty org (multi-org, unbound subject)
func (s *Server) resolveOrgForSubject(ctx context.Context, subject string, claims *auth.Claims) (orgID, role string) {
	if claims.OrgID != "" {
		return claims.OrgID, ""
	}
	if s.bootstrapStore == nil {
		return "", ""
	}
	bctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if boundOrg, boundRole, err := s.bootstrapStore.Binding(bctx, subject); err == nil {
		return boundOrg, boundRole
	}
	if org, err := s.bootstrapStore.UniqueOrgID(bctx); err == nil && org != "" {
		// Auto-bind so the next login hits case (b) directly.
		if bindErr := s.bootstrapStore.Bind(bctx, subject, org, auth.MapGroupsToRole(claims.Groups)); bindErr != nil {
			s.log.Warn("bootstrap: auto-bind failed", "err", bindErr, "subject", subject)
		}
		return org, ""
	}
	return "", ""
}
