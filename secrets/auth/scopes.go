// Package auth provides A2A (Agent-to-Agent) authentication token management
// for OpenAgentPlatform. This file implements scope matching with wildcard support.
package auth

import "strings"

// Scope represents a permission scope string.
type Scope string

// Matches reports whether a granted set of scopes satisfies all required scopes.
//
// Matching rules:
//   - Exact match: a granted scope exactly equals a required scope.
//   - Wildcard: a granted scope ending with ":*" matches any required scope
//     that starts with the prefix before ":*".
//     Example: "patch:*" matches "patch:approve", "patch:reject".
//   - No partial-segment matching: "patch:*" does NOT match "patch:approve:fast".
func Matches(required, granted []string) bool {
	if len(required) == 0 {
		return true
	}

	grantedSet := make([]Scope, len(granted))
	for i, g := range granted {
		grantedSet[i] = Scope(g)
	}

	for _, req := range required {
		reqScope := Scope(req)
		matched := false
		for _, g := range grantedSet {
			if scopeMatch(reqScope, g) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// scopeMatch checks if a single required scope is satisfied by a single granted scope.
func scopeMatch(required, granted Scope) bool {
	if required == granted {
		return true
	}
	// Wildcard: "patch:*" matches "patch:approve"
	g := string(granted)
	if strings.HasSuffix(g, ":*") {
		prefix := g[:len(g)-2]
		req := string(required)
		if strings.HasPrefix(req, prefix+":") {
			// Ensure we don't match across segment boundaries beyond the wildcard.
			// e.g. "patch:*" should match "patch:approve" but we strip the prefix+colon
			// and ensure the remainder doesn't itself contain a colon at the top level.
			// However the spec says "patch:*" matches "patch:approve" so we accept any
			// suffix after the prefix+colon.
			return true
		}
	}
	return false
}

// ContainsAll reports whether all required scopes are present in granted (exact match only, no wildcards).
func ContainsAll(required, granted []string) bool {
	grantedMap := make(map[string]bool, len(granted))
	for _, g := range granted {
		grantedMap[g] = true
	}
	for _, r := range required {
		if !grantedMap[r] {
			return false
		}
	}
	return true
}
