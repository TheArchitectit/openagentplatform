package policy

import (
	"context"
	"encoding/json"
	"time"
	"github.com/nats-io/nats.go"
)

func (e *PolicyEngine) runScheduler() {
	defer close(e.schedulerDoneCh)
	ticker := time.NewTicker(e.evalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.runScheduledEvaluation()
		}
	}
}

// runScheduledEvaluation evaluates every active policy against every
// agent. Failures are logged and do not abort the cycle.
func (e *PolicyEngine) runScheduledEvaluation() {
	e.wg.Add(1)
	defer e.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), e.evalInterval)
	defer cancel()

	results, err := e.EvaluateAll(ctx, "")
	if err != nil {
		e.log.Warn("scheduled policy evaluation failed", "err", err)
		return
	}
	totalViolations := 0
	nonCompliant := 0
	for _, agentResults := range results {
		for _, r := range agentResults {
			if !r.Compliant {
				nonCompliant++
				totalViolations += len(r.Violations)
			}
		}
	}
	e.log.Info("scheduled policy evaluation complete",
		"agents", len(results),
		"non_compliant", nonCompliant,
		"violations", totalViolations)
}

// onEvalRequest is the NATS handler for manual evaluation requests
// published on oap.events.policy.evaluate.
func (e *PolicyEngine) onEvalRequest(msg *nats.Msg) {
	e.wg.Add(1)
	defer e.wg.Done()

	var req PolicyEvaluationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		e.log.Warn("policy eval request decode failed", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := e.EvaluatePolicyManual(ctx, req)
	if err != nil {
		e.log.Warn("manual policy evaluation failed", "err", err)
		return
	}
	e.log.Info("manual policy evaluation complete",
		"policy_id", req.PolicyID,
		"agent_id", req.AgentID,
		"site_id", req.SiteID,
		"results", len(results),
		"violations", violationCount(results))

	// Publish the result so subscribers (e.g. the WebSocket hub) can
	// stream it to live dashboards.
	if e.publisher != nil {
		out, _ := json.Marshal(map[string]any{
			"type":    "policy.evaluated",
			"results": results,
		})
		_ = e.publisher.Publish(ctx, "oap.events.policy.evaluated", out)
	}
}

// onCheckResult is the NATS handler for check-result events. It
// re-evaluates the policies assigned to the agent that owns the check.
func (e *PolicyEngine) onCheckResult(msg *nats.Msg) {
	e.wg.Add(1)
	defer e.wg.Done()

	var evt struct {
		AgentID string `json:"agent_id"`
		CheckID string `json:"check_id"`
	}
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		return
	}
	if evt.AgentID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := e.EvaluateAllForAgent(ctx, evt.AgentID)
	if err != nil {
		e.log.Warn("event-driven policy evaluation failed",
			"agent_id", evt.AgentID, "err", err)
	}
}

func violationCount(rs []PolicyEvaluationResult) int {
	n := 0
	for _, r := range rs {
		n += len(r.Violations)
	}
	return n
}
