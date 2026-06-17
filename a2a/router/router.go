// Package router - router.go implements skill-based agent routing.
// Given a task, the router scores all registered agent cards by tag
// overlap and bonuses, then returns the highest-scoring agent.
package router

import (
	"errors"
	"fmt"
	"sort"

	"github.com/openagentplatform/openagentplatform/a2a/models"
	"github.com/openagentplatform/openagentplatform/a2a/registry"
)

// ============================================================
// Router errors
// ============================================================

var (
	// ErrNoMatchingAgent is returned when no agent card in the registry
	// matches the task requirements.
	ErrNoMatchingAgent = errors.New("a2a: no matching agent found")

	// ErrNilRegistry is returned when a nil registry is passed to the
	// router constructor.
	ErrNilRegistry = errors.New("a2a: nil registry")
)

// ============================================================
// Router
// ============================================================

// Router selects the best agent card for a given task based on
// skill-tag overlap and scoring bonuses.
type Router struct {
	registry *registry.Registry
}

// NewRouter constructs a Router backed by the given registry.
func NewRouter(reg *registry.Registry) (*Router, error) {
	if reg == nil {
		return nil, ErrNilRegistry
	}
	return &Router{registry: reg}, nil
}

// ============================================================
// Routing
// ============================================================

// Route selects the best agent card for a task. The selection algorithm:
//
//  1. Extract required skills from task metadata (key: "required_skills").
//  2. For each agent card in the registry, compute a score:
//     - +10 per matching skill tag (overlap between task's required tags
//       and the agent's skill tags).
//     - +5 per exact skill ID match.
//     - +2 bonus if the agent supports streaming (when the task requests it).
//     - +2 bonus if the agent supports push notifications (when the task
//       requests it).
//     - +1 per generic capability match.
//     - Stale agents (past heartbeat TTL) are excluded.
//  3. If preferredAgent is non-empty and is registered, it wins ties
//     (equal scores go to the preferred agent).
//  4. Return the highest-scoring agent. If no agent scores above zero,
//     return ErrNoMatchingAgent.
func (rt *Router) Route(task *models.Task, preferredAgent string) (*models.AgentCard, error) {
	if task == nil {
		return nil, errors.New("a2a: nil task")
	}

	requiredTags := extractRequiredTags(task)

	// Check if the task requires streaming or push notifications.
	wantStreaming := false
	wantPush := false
	if task.Metadata != nil {
		wantStreaming = task.Metadata["streaming"] == "true" || task.Metadata["streaming"] == "1"
		wantPush = task.Metadata["push_notifications"] == "true" || task.Metadata["push_notifications"] == "1"
	}

	cards := rt.registry.ListCards(true) // skip stale
	if len(cards) == 0 {
		return nil, ErrNoMatchingAgent
	}

	type scored struct {
		card  models.AgentCard
		score int
	}

	results := make([]scored, 0, len(cards))
	for i := range cards {
		s := scoreCard(&cards[i], requiredTags, wantStreaming, wantPush)
		if s > 0 {
			results = append(results, scored{card: cards[i], score: s})
		}
	}

	if len(results) == 0 {
		return nil, ErrNoMatchingAgent
	}

	// Sort: highest score first; preferred agent breaks ties.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		// Tie-break: preferred agent wins.
		if preferredAgent != "" {
			if results[i].card.URL == preferredAgent {
				return true
			}
			if results[j].card.URL == preferredAgent {
				return false
			}
		}
		return results[i].card.URL < results[j].card.URL
	})

	best := results[0].card
	return &best, nil
}

// ============================================================
// Scoring helpers
// ============================================================

// scoreCard computes a routing score for a single agent card.
// Returns 0 if the card has no relevance to the required tags and
// capability demands.
func scoreCard(card *models.AgentCard, requiredTags []string, wantStreaming, wantPush bool) int {
	if card == nil {
		return 0
	}

	score := 0
	requiredSet := make(map[string]struct{}, len(requiredTags))
	for _, t := range requiredTags {
		requiredSet[t] = struct{}{}
	}

	// Build a flat set of all tags across all skills for this agent.
	agentTagSet := make(map[string]struct{})
	agentSkillIDs := make(map[string]struct{}, len(card.Skills))
	for _, skill := range card.Skills {
		agentSkillIDs[skill.ID] = struct{}{}
		for _, t := range skill.Tags {
			agentTagSet[t] = struct{}{}
		}
	}
	// Also include top-level card tags.
	for _, t := range card.Tags {
		agentTagSet[t] = struct{}{}
	}

	// +10 per matching tag overlap
	for tag := range requiredSet {
		if _, ok := agentTagSet[tag]; ok {
			score += 10
		}
	}

	// If required skills are specified as exact IDs, +5 per ID match.
	for _, tag := range requiredTags {
		// Treat a required tag that starts with "skill:" as an exact skill ID.
		if len(tag) > 6 && tag[:6] == "skill:" {
			id := tag[6:]
			if _, ok := agentSkillIDs[id]; ok {
				score += 5
			}
		}
	}

	// +2 bonus for streaming capability if requested
	if wantStreaming && card.Streaming {
		score += 2
	}

	// +2 bonus for push notifications if requested
	if wantPush && card.PushNotifications {
		score += 2
	}

	return score
}

// extractRequiredTags pulls required skill tags from task metadata.
// The metadata key "required_skills" may contain a comma-separated
// list of tags or skill IDs.
func extractRequiredTags(task *models.Task) []string {
	if task.Metadata == nil {
		return nil
	}

	raw, ok := task.Metadata["required_skills"]
	if !ok || raw == "" {
		return nil
	}

	return splitAndTrim(raw, ",")
}

// splitAndTrim splits a string by sep and trims whitespace from each
// element, discarding empty results.
func splitAndTrim(s, sep string) []string {
	parts := splitString(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = trimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// containsCap checks if a capability slice contains a value.
func containsCap(caps []string, val string) bool {
	for _, c := range caps {
		if c == val {
			return true
		}
	}
	return false
}

// ============================================================
// String helpers (avoid importing strings to keep this package lean)
// ============================================================

func splitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	out := []string{}
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			out = append(out, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// ============================================================
// Diagnostic helper (for logging / debugging)
// ============================================================

// ExplainRoute returns a human-readable description of how a task would
// be routed. It does not modify any state. Useful for debugging routing
// decisions.
func (rt *Router) ExplainRoute(task *models.Task, preferredAgent string) (string, error) {
	if task == nil {
		return "", errors.New("a2a: nil task")
	}

	requiredTags := extractRequiredTags(task)
	wantStreaming := false
	wantPush := false
	if task.Metadata != nil {
		wantStreaming = task.Metadata["streaming"] == "true" || task.Metadata["streaming"] == "1"
		wantPush = task.Metadata["push_notifications"] == "true" || task.Metadata["push_notifications"] == "1"
	}

	cards := rt.registry.ListCards(true)
	if len(cards) == 0 {
		return "no agents registered", nil
	}

	out := fmt.Sprintf("routing task %q with required_tags=%v streaming=%v push=%v preferred=%q\n",
		task.ID, requiredTags, wantStreaming, wantPush, preferredAgent)
	for i := range cards {
		s := scoreCard(&cards[i], requiredTags, wantStreaming, wantPush)
		out += fmt.Sprintf("  agent=%q score=%d skills=%d streaming=%v push=%v\n",
			cards[i].URL, s, len(cards[i].Skills), cards[i].Streaming, cards[i].PushNotifications)
	}
	return out, nil
}
