package monitoring

import (
	"sort"
	"time"
)

// ComplianceResult represents one policy evaluation for one agent.
type ComplianceResult struct {
	AgentID     string    `json:"agent_id"`
	PolicyID    string    `json:"policy_id"`
	Compliant   bool      `json:"compliant"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// AgentComplianceScore is an agent's passing-policy percentage.
type AgentComplianceScore struct {
	AgentID string  `json:"agent_id"`
	Passed  int     `json:"passed"`
	Total   int     `json:"total"`
	Score   float64 `json:"score"`
}

// ComplianceScorecard summarizes compliance per agent and across all evaluations.
type ComplianceScorecard struct {
	OverallScore float64                `json:"overall_score"`
	Passed       int                    `json:"passed"`
	Total        int                    `json:"total"`
	Agents       []AgentComplianceScore `json:"agents"`
	ComputedAt   time.Time              `json:"computed_at"`
}

// Scorecard computes a compliance scorecard from evaluation results.
type Scorecard struct{ now func() time.Time }

func NewScorecard() *Scorecard { return &Scorecard{now: time.Now} }

func (s *Scorecard) Compute(results []ComplianceResult) ComplianceScorecard {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	card := ComplianceScorecard{Agents: make([]AgentComplianceScore, 0), ComputedAt: now()}
	grouped := make(map[string]*AgentComplianceScore)
	for _, result := range results {
		score := grouped[result.AgentID]
		if score == nil {
			score = &AgentComplianceScore{AgentID: result.AgentID}
			grouped[result.AgentID] = score
		}
		score.Total++
		card.Total++
		if result.Compliant {
			score.Passed++
			card.Passed++
		}
	}
	for _, score := range grouped {
		score.Score = percentage(score.Passed, score.Total)
		card.Agents = append(card.Agents, *score)
	}
	sort.Slice(card.Agents, func(i, j int) bool { return card.Agents[i].AgentID < card.Agents[j].AgentID })
	card.OverallScore = percentage(card.Passed, card.Total)
	return card
}

func percentage(passed, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(passed) * 100 / float64(total)
}
