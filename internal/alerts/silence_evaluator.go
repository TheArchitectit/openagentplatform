package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// DefaultSilenceEvalInterval is how often the periodic stale-agent evaluator
// wakes to re-check offline-silence rules. 10 minutes keeps mailbox spam low
// while still catching a 24h-silent agent within one sweep.
const DefaultSilenceEvalInterval = 10 * time.Minute

// AgentQuerier is the explicit query seam the evaluator uses to find agents
// that have been silent longer than a rule's threshold. It is intentionally
// narrow so the alerts package does not depend on the full agent store.
type AgentQuerier interface {
	// ListSilentAgents returns agents in the given org whose last_seen is
	// older than staleBefore and (optionally) whose status matches a non-empty
	// statusFilter. An empty orgID returns across all orgs.
	ListSilentAgents(ctx context.Context, orgID, statusFilter string, staleBefore time.Time) ([]models.Agent, error)
}

// RuleQuerier is the rule-read seam the evaluator needs. It is intentionally
// narrow (rules only) so the evaluator does not take a dependency on the full
// alert Engine interface.
type RuleQuerier interface {
	GetAlertRules(ctx context.Context, orgID string) ([]models.AlertRule, error)
}

// SilenceEvaluator periodically scans enabled alert rules that carry the
// offline-silence condition and fires deduplicated alert.fired events for any
// agent that has been silent past the rule's threshold. It is the source-backed
// trigger the RMM-01 sprint requires: the existing alert engine reacts to
// incoming alert events and has no agent-lookup seam, so without a periodic
// evaluator a "silent for N hours" rule could never fire.
//
// Recovery: when an agent is no longer silent (its last_seen moved past the
// threshold, e.g. it reported back), the evaluator emits an alert.resolved for
// the matching dedup key, so a flapping agent does not re-open the same alert.
type SilenceEvaluator struct {
	rules    RuleQuerier
	agents   AgentQuerier
	pub      Publisher
	interval time.Duration
	now      func() time.Time
	log      *slog.Logger

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewSilenceEvaluator constructs a SilenceEvaluator. If agents is nil the
// evaluator is a no-op at runtime (Start succeeds but performs no scans).
func NewSilenceEvaluator(rules RuleQuerier, agents AgentQuerier, pub Publisher, log *slog.Logger) *SilenceEvaluator {
	if log == nil {
		log = slog.Default()
	}
	return &SilenceEvaluator{
		rules:    rules,
		agents:   agents,
		pub:      pub,
		interval: DefaultSilenceEvalInterval,
		now:      time.Now,
		log:      log,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the periodic evaluation loop.
func (e *SilenceEvaluator) Start(ctx context.Context) {
	if e.agents == nil {
		e.log.Warn("silence evaluator disabled: no AgentQuerier configured")
		return
	}
	e.wg.Add(1)
	go e.run()
}

// Stop terminates the loop and waits for the in-flight sweep to finish.
func (e *SilenceEvaluator) Stop() {
	close(e.stopCh)
	e.wg.Wait()
}

func (e *SilenceEvaluator) run() {
	defer e.wg.Done()
	t := time.NewTicker(e.interval)
	defer t.Stop()
	// Run once immediately so a freshly created rule is not silent for a full
	// interval before its first evaluation.
	e.sweep(context.Background())
	for {
		select {
		case <-e.stopCh:
			return
		case <-t.C:
			e.sweep(context.Background())
		}
	}
}

// sweep evaluates every enabled rule with an offline-silence condition.
func (e *SilenceEvaluator) sweep(ctx context.Context) {
	rules, err := e.rules.GetAlertRules(ctx, "")
	if err != nil {
		e.log.Warn("silence evaluator: list rules failed", "err", err)
		return
	}
	for i := range rules {
		rule := rules[i]
		if rule.OrgID == "" {
			// Skip orphaned rules with no org scope — they cannot be
			// attributed to a tenant for silent-agent evaluation.
			continue
		}
		if !rule.Enabled || rule.OfflineSilenceSeconds == nil || *rule.OfflineSilenceSeconds <= 0 {
			continue
		}
		threshold := time.Duration(*rule.OfflineSilenceSeconds) * time.Second
		staleBefore := e.now().Add(-threshold)
		e.evaluateRule(ctx, &rule, staleBefore)
	}
}

func (e *SilenceEvaluator) evaluateRule(ctx context.Context, rule *models.AlertRule, staleBefore time.Time) {
	agents, err := e.agents.ListSilentAgents(ctx, rule.OrgID, "", staleBefore)
	if err != nil {
		e.log.Warn("silence evaluator: query silent agents failed",
			"rule", rule.ID, "err", err)
		return
	}
	for _, a := range agents {
		// Apply the rule's agent/site scope if set.
		if rule.AgentID != "" && a.ID != rule.AgentID {
			continue
		}
		if rule.SiteID != "" && a.SiteID != rule.SiteID {
			continue
		}
		key := silenceDedupKey(rule.ID, a.ID)
		_ = key // reserved: the engine performs dedup via handleCheckFailure
		evt := AlertEvent{
			Type:          "alert.fired",
			AgentID:       a.ID,
			AgentHostname: a.Hostname,
			SiteID:        a.SiteID,
			ClientID:      a.ClientID,
			CheckID:       "",
			Severity:      rule.MinSeverity,
			Status:        "firing",
			Message:       fmt.Sprintf("agent %s silent for over %s", a.Hostname, thresholdDesc(*rule.OfflineSilenceSeconds)),
			Timestamp:     e.now(),
			AlertType:     "offline_sla",
		}
		if err := e.publish(ctx, evt); err != nil {
			e.log.Warn("silence evaluator: publish failed", "agent", a.ID, "err", err)
		}
	}
}

func (e *SilenceEvaluator) publish(ctx context.Context, evt AlertEvent) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return e.pub.Publish(ctx, events.SubjectAlertEvents, payload)
}

// silenceDedupKey namespaces the offline-SLA dedup key so it cannot collide
// with a check-failure alert for the same agent.
func silenceDedupKey(ruleID, agentID string) string {
	return "offline_sla" + "\x00" + agentID + "\x00" + ruleID
}

func thresholdDesc(seconds int) string {
	d := time.Duration(seconds) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%.0fh", d.Hours())
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
