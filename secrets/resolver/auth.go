package resolver

import (
	"strings"
)

// AuthContext represents the authenticated entity requesting a secret.
// The hierarchy is: Client > Site > Agent.
type AuthContext struct {
	ClientID string
	SiteID   string
	AgentID  string
}

// Authorizer implements hierarchy-based secret access control.
type Authorizer struct{}

// NewAuthorizer creates a new Authorizer.
func NewAuthorizer() *Authorizer {
	return &Authorizer{}
}

// CanAccess checks whether the given AuthContext can access a secret at the
// given path. Paths follow the template:
//
//	openagentplatform/<client>/<site>/<agent>/credentials
//
// Access rules:
//   - If AgentID matches the path's agent segment -> allowed
//   - If SiteID matches and the path has no agent segment -> allowed
//   - If ClientID matches and the path has no site/agent segment -> allowed
//   - If the path is client-level (no site/agent) and ClientID matches -> allowed
func (a *Authorizer) CanAccess(path string, ctx *AuthContext) bool {
	if ctx == nil {
		return false
	}

	segments := splitPath(path)

	clientSeg := segmentAfter(segments, "clients")
	siteSeg := segmentAfter(segments, "sites")
	agentSeg := segmentAfter(segments, "agents")

	// ClientID must always match.
	if clientSeg == "" || clientSeg != ctx.ClientID {
		return false
	}

	// Agent-level access: path has an agent segment.
	if agentSeg != "" {
		return agentSeg == ctx.AgentID
	}

	// Site-level access: path has a site segment but no agent.
	if siteSeg != "" {
		return siteSeg == ctx.SiteID
	}

	// Client-level access: no site or agent in path.
	return true
}

// splitPath splits a path into segments, stripping empty entries.
func splitPath(path string) []string {
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// segmentAfter finds the value following a given key segment.
// e.g., segmentAfter(["clients", "acme", "sites", "branch-01"], "clients") -> "acme"
func segmentAfter(segments []string, key string) string {
	for i, s := range segments {
		if s == key && i+1 < len(segments) {
			return segments[i+1]
		}
	}
	return ""
}
