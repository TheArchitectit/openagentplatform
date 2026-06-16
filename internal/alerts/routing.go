// Package alerts - routing.go implements the alert routing engine. Routing
// rules map alert conditions (agent tags, check types, severity, site) to
// notification channels. Multiple rules can match a single alert; their
// channel sets are unioned. If no rule matches, the org-level default
// channel set is used.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// RoutingConditions describes the match criteria for a routing rule.
// All set fields must match for the rule to apply. Empty/nil fields
// are wildcards.
type RoutingConditions struct {
	AgentTags  []string `json:"agent_tags,omitempty"`  // any-of match
	CheckTypes []string `json:"check_types,omitempty"` // any-of match
	Severity   string   `json:"severity,omitempty"`    // exact match (info/warning/critical/emergency)
	Severities []string `json:"severities,omitempty"`  // any-of match
	SiteID     string   `json:"site_id,omitempty"`     // exact match
	OrgID      string   `json:"org_id,omitempty"`      // exact match
	AgentID    string   `json:"agent_id,omitempty"`    // exact match
	CheckID    string   `json:"check_id,omitempty"`    // exact match
}

// Matches returns true if the conditions match the given routing
// context. Nil or empty conditions are treated as "match everything".
func (c RoutingConditions) Matches(ctx RoutingContext) bool {
	if len(c.AgentTags) > 0 {
		found := false
		for _, want := range c.AgentTags {
			for _, have := range ctx.AgentTags {
				if want == have {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(c.CheckTypes) > 0 {
		found := false
		for _, want := range c.CheckTypes {
			if want == ctx.CheckType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if c.Severity != "" && c.Severity != normalizeSeverity(ctx.Severity) {
		return false
	}
	if len(c.Severities) > 0 {
		found := false
		ns := normalizeSeverity(ctx.Severity)
		for _, s := range c.Severities {
			if s == ns {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if c.SiteID != "" && c.SiteID != ctx.SiteID {
		return false
	}
	if c.OrgID != "" && c.OrgID != ctx.OrgID {
		return false
	}
	if c.AgentID != "" && c.AgentID != ctx.AgentID {
		return false
	}
	if c.CheckID != "" && c.CheckID != ctx.CheckID {
		return false
	}
	return true
}

// RoutingContext is the input to routing evaluation. It carries the
// alert's identifying attributes that routing rules match against.
type RoutingContext struct {
	AgentID   string   `json:"agent_id,omitempty"`
	AgentTags []string `json:"agent_tags,omitempty"`
	CheckID   string   `json:"check_id,omitempty"`
	CheckType string   `json:"check_type,omitempty"`
	Severity  string   `json:"severity,omitempty"`
	SiteID    string   `json:"site_id,omitempty"`
	OrgID     string   `json:"org_id,omitempty"`
}

// RoutingRule is a single rule mapping conditions to a set of
// destination channel IDs. Rules are stored in the alert_routing_rules
// table and many-to-many associated with notification channels via
// the alert_rule_channels junction.
type RoutingRule struct {
	ID            string            `json:"id"`
	OrgID         string            `json:"org_id"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Priority      int               `json:"priority"` // lower = higher priority
	Conditions    RoutingConditions `json:"conditions"`
	ChannelIDs    []string          `json:"channel_ids"`
	Enabled       bool              `json:"enabled"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// RoutingRuleStore is the persistence seam for routing rules and
// the alert_rule_channels junction table.
type RoutingRuleStore interface {
	ListRoutingRules(ctx context.Context, orgID string) ([]RoutingRule, error)
	GetRoutingRule(ctx context.Context, id string) (*RoutingRule, error)
	CreateRoutingRule(ctx context.Context, r *RoutingRule) error
	UpdateRoutingRule(ctx context.Context, r *RoutingRule) error
	DeleteRoutingRule(ctx context.Context, id string) error

	// Junction operations.
	SetRuleChannels(ctx context.Context, ruleID string, channelIDs []string) error
	GetRuleChannels(ctx context.Context, ruleID string) ([]string, error)
	GetAlertRuleChannels(ctx context.Context, alertRuleID string) ([]string, error)
	SetAlertRuleChannels(ctx context.Context, alertRuleID string, channelIDs []string) error
}

// DefaultChannelStore is the persistence seam for org-level default
// channel fallbacks.
type DefaultChannelStore interface {
	GetDefaultChannels(ctx context.Context, orgID string) ([]string, error)
	SetDefaultChannels(ctx context.Context, orgID string, channelIDs []string) error
}

// Router evaluates routing rules for a given alert context. It
// collects the union of all matching rules' channel sets, falling
// back to the org default if no rules match. The result is
// deterministic (sorted) for stable downstream dispatch.
type Router struct {
	store   RoutingRuleStore
	defStore DefaultChannelStore
	mu      sync.RWMutex
	// Optional in-memory rule cache, refreshed on cache expiry.
	cacheTTL  time.Duration
	cacheAt   time.Time
	cacheBy   map[string][]RoutingRule // orgID -> rules
}

// NewRouter constructs a Router. cacheTTL controls how long rules are
// cached in memory; zero or negative means no caching (always read
// from the store).
func NewRouter(store RoutingRuleStore, defStore DefaultChannelStore, cacheTTL time.Duration) *Router {
	return &Router{
		store:    store,
		defStore: defStore,
		cacheTTL: cacheTTL,
		cacheBy:  make(map[string][]RoutingRule),
	}
}

// InvalidateCache clears the in-memory rule cache. Call this after
// writing rule changes through any path that bypasses the router.
func (r *Router) InvalidateCache() {
	r.mu.Lock()
	r.cacheAt = time.Time{}
	r.cacheBy = make(map[string][]RoutingRule)
	r.mu.Unlock()
}

// loadRules returns the routing rules for the given org, using the
// in-memory cache when valid.
func (r *Router) loadRules(ctx context.Context, orgID string) ([]RoutingRule, error) {
	if r.cacheTTL > 0 {
		r.mu.RLock()
		cached, ok := r.cacheBy[orgID]
		at := r.cacheAt
		r.mu.RUnlock()
		if ok && time.Since(at) < r.cacheTTL {
			return cached, nil
		}
	}
	rules, err := r.store.ListRoutingRules(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("alerts: list routing rules: %w", err)
	}
	if r.cacheTTL > 0 {
		r.mu.Lock()
		r.cacheBy[orgID] = rules
		r.cacheAt = time.Now()
		r.mu.Unlock()
	}
	return rules, nil
}

// RoutingResult is the output of Route.
type RoutingResult struct {
	ChannelIDs  []string `json:"channel_ids"`
	MatchedRule []string `json:"matched_rule_ids,omitempty"` // ids of rules that contributed
	UsedDefault bool     `json:"used_default"`               // true if no rules matched
}

// Route evaluates all enabled routing rules for the given org
// against the routing context. The returned channel IDs are the
// union of all matching rules' channels, or the org-level default
// channel set if no rules match. The result is always sorted and
// deduplicated for determinism.
func (r *Router) Route(ctx context.Context, orgID string, rc RoutingContext) (RoutingResult, error) {
	if orgID == "" {
		return RoutingResult{}, errors.New("alerts: routing requires orgID")
	}
	rules, err := r.loadRules(ctx, orgID)
	if err != nil {
		return RoutingResult{}, err
	}

	seen := make(map[string]struct{})
	matched := make([]string, 0)
	channels := make([]string, 0)

	// Sort by priority then by ID for deterministic evaluation order.
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !rule.Conditions.Matches(rc) {
			continue
		}
		matched = append(matched, rule.ID)
		for _, ch := range rule.ChannelIDs {
			if _, dup := seen[ch]; dup {
				continue
			}
			seen[ch] = struct{}{}
			channels = append(channels, ch)
		}
	}

	if len(channels) == 0 && r.defStore != nil {
		defs, err := r.defStore.GetDefaultChannels(ctx, orgID)
		if err != nil {
			return RoutingResult{}, fmt.Errorf("alerts: get default channels: %w", err)
		}
		channels = defs
		return RoutingResult{
			ChannelIDs:  sortedUnique(channels),
			UsedDefault: true,
		}, nil
	}

	return RoutingResult{
		ChannelIDs:  sortedUnique(channels),
		MatchedRule: matched,
		UsedDefault: false,
	}, nil
}

// RouteForAlert is a convenience that builds a RoutingContext from a
// models.Alert and calls Route. Agent tags must be supplied by the
// caller (the alert model does not carry them).
func (r *Router) RouteForAlert(ctx context.Context, alert interface{}, agentTags []string) (RoutingResult, error) {
	rc := RoutingContext{
		AgentTags: agentTags,
		Severity:  extractSeverity(alert),
	}
	orgID := extractOrgID(alert)
	agentID := extractAgentID(alert)
	siteID := extractSiteID(alert)
	checkID := extractCheckID(alert)
	rc.OrgID = orgID
	rc.AgentID = agentID
	rc.SiteID = siteID
	rc.CheckID = checkID
	return r.Route(ctx, orgID, rc)
}

// sortedUnique returns a sorted, deduplicated copy of ids.
func sortedUnique(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// --- alert rule channels junction -----------------------------------------

// AlertRuleChannelLinker is the subset of RoutingRuleStore used by the
// API to manage the alert_rule_channels junction table for individual
// alert rules (as opposed to general routing rules).
type AlertRuleChannelLinker interface {
	GetAlertRuleChannels(ctx context.Context, alertRuleID string) ([]string, error)
	SetAlertRuleChannels(ctx context.Context, alertRuleID string, channelIDs []string) error
}

// SetAlertRuleChannels is a small helper used by the API handler. It
// serialises the operation against the store.
func SetAlertRuleChannels(ctx context.Context, linker AlertRuleChannelLinker, ruleID string, channelIDs []string) error {
	if linker == nil {
		return errors.New("alerts: nil linker")
	}
	if ruleID == "" {
		return errors.New("alerts: ruleID required")
	}
	deduped := sortedUnique(channelIDs)
	return linker.SetAlertRuleChannels(ctx, ruleID, deduped)
}

// --- helpers --------------------------------------------------------------

// extractSeverity, extractOrgID, etc. are type-switch helpers so
// RouteForAlert accepts any value with the relevant fields (typically
// *models.Alert). They return the zero value on type mismatch.

// alertFields is the shape RouteForAlert expects.
type alertFields interface {
	GetSeverity() string
	GetOrgID() string
	GetAgentID() string
	GetSiteID() string
	GetCheckID() string
}

func extractSeverity(v interface{}) string {
	if a, ok := v.(alertFields); ok {
		return a.GetSeverity()
	}
	if m := asMap(v); m != nil {
		if s, ok := m["severity"].(string); ok {
			return s
		}
	}
	return ""
}

func extractOrgID(v interface{}) string {
	if a, ok := v.(alertFields); ok {
		return a.GetOrgID()
	}
	if m := asMap(v); m != nil {
		if s, ok := m["org_id"].(string); ok {
			return s
		}
	}
	return ""
}

func extractAgentID(v interface{}) string {
	if a, ok := v.(alertFields); ok {
		return a.GetAgentID()
	}
	if m := asMap(v); m != nil {
		if s, ok := m["agent_id"].(string); ok {
			return s
		}
	}
	return ""
}

func extractSiteID(v interface{}) string {
	if a, ok := v.(alertFields); ok {
		return a.GetSiteID()
	}
	if m := asMap(v); m != nil {
		if s, ok := m["site_id"].(string); ok {
			return s
		}
	}
	return ""
}

func extractCheckID(v interface{}) string {
	if a, ok := v.(alertFields); ok {
		return a.GetCheckID()
	}
	if m := asMap(v); m != nil {
		if s, ok := m["check_id"].(string); ok {
			return s
		}
	}
	return ""
}

func asMap(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	// Try to JSON round-trip for structs (models.Alert, etc.).
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}
