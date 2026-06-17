package api

import (
	"context"
	"net/http"

	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// RequireOrgAccess checks that the authenticated session belongs to the
// requested org. Returns true when access is allowed; false otherwise (caller
// should respond 403).
func RequireOrgAccess(ctx context.Context, orgID string) bool {
	claims, ok := auth.UserFromContext(ctx)
	if !ok || claims == nil {
		return false
	}
	if orgID == "" {
		return false
	}
	return claims.OrgID == orgID
}

// RequireSiteAccess checks that the authenticated session may access the
// given site. An admin (role "admin") in the org can access all sites in that
// org; otherwise the session's SiteID must match exactly.
func RequireSiteAccess(ctx context.Context, siteID string) bool {
	claims, ok := auth.UserFromContext(ctx)
	if !ok || claims == nil {
		return false
	}
	if siteID == "" {
		return false
	}
	if claims.SiteID == siteID {
		return true
	}
	// Org admins can access any site within their own org.
	if claims.Role == auth.RoleAdmin && claims.OrgID != "" {
		return true
	}
	return false
}

// RequireRole checks that the authenticated session has one of the allowed
// roles. Returns true when the role matches.
func RequireRole(ctx context.Context, roles ...string) bool {
	claims, ok := auth.UserFromContext(ctx)
	if !ok || claims == nil {
		return false
	}
	if len(roles) == 0 {
		return true // no role restriction
	}
	for _, r := range roles {
		if claims.Role == r {
			return true
		}
	}
	return false
}

// CanAccessAgent verifies that the authenticated session's org owns the given
// agent. Uses GetAgent which returns the agent's OrgID for the check.
func CanAccessAgent(ctx context.Context, agentStore agentStore, agentID string) bool {
	claims, ok := auth.UserFromContext(ctx)
	if !ok || claims == nil || claims.OrgID == "" {
		return false
	}
	if agentID == "" || agentStore == nil {
		return false
	}
	a, err := agentStore.GetAgent(ctx, claims.OrgID, agentID)
	if err != nil || a == nil {
		return false
	}
	return a.OrgID == claims.OrgID
}

// RequireOrgAccessHTTP writes 403 if the session does not belong to orgID.
// Convenience wrapper for HTTP handlers.
func RequireOrgAccessHTTP(w http.ResponseWriter, r *http.Request, orgID string) bool {
	if RequireOrgAccess(r.Context(), orgID) {
		return true
	}
	http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	return false
}