package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func NewEngine(cfg Config) *PolicyEngine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.QueueGroup == "" {
		cfg.QueueGroup = "oap-policy-engine"
	}

	// Wire the OPA engine's builtin resolver so policies can call
	// oap.agent.* etc. if the caller didn't already set one.
	if cfg.OPA != nil && cfg.Resolver != nil {
		cfg.OPA.SetBuiltinResolver(cfg.Resolver)
	}

	return &PolicyEngine{
		store:        cfg.Store,
		opa:          cfg.OPA,
		publisher:    cfg.Publisher,
		resolver:     cfg.Resolver,
		log:          cfg.Logger,
		client:       cfg.Client,
		now:          cfg.Now,
		violations:   cfg.Violations,
		evalInterval: cfg.Interval,
		batchSize:    cfg.BatchSize,
		stopCh:       make(chan struct{}),
	}
}

// Start subscribes to the NATS evaluation-request subject and starts
// the scheduled evaluation loop. Returns an error if subscription fails.
func (e *PolicyEngine) Start(ctx context.Context) error {
	if e.client == nil {
		return errors.New("policy_engine: nil subscriber")
	}
	if e.opa == nil {
		return errors.New("policy_engine: nil OPA engine")
	}
	if e.store == nil {
		return errors.New("policy_engine: nil store")
	}

	// Listen for manual evaluation requests.
	sub, err := e.client.SubscribeQueue(SubjectPolicyEvaluate, "oap-policy-engine", e.onEvalRequest)
	if err != nil {
		return fmt.Errorf("policy_engine: subscribe evaluate: %w", err)
	}
	e.evalSub = sub

	// Listen for check-result events to trigger re-evaluation.
	if sub, err := e.client.SubscribeQueue(events.SubjectCheckResultEvent, "oap-policy-engine", e.onCheckResult); err != nil {
		e.log.Warn("policy_engine: check-result subscribe failed", "err", err)
	} else {
		e.sub = sub
	}

	// Start the scheduler.
	e.schedulerDoneCh = make(chan struct{})
	go e.runScheduler()

	e.log.Info("policy engine started",
		"subject", SubjectPolicyEvaluate,
		"interval", e.evalInterval,
		"queue", "oap-policy-engine")
	return nil
}

// Stop unsubscribes and stops the scheduler.
func (e *PolicyEngine) Stop() {
	if e.evalSub != nil {
		_ = e.evalSub.Unsubscribe()
	}
	if e.sub != nil {
		_ = e.sub.Unsubscribe()
	}
	close(e.stopCh)
	if e.schedulerDoneCh != nil {
		<-e.schedulerDoneCh
	}
	e.wg.Wait()
}

// EvaluatePolicy evaluates a single policy against a single agent,
// returning the result. Persists any violations to the store.
func (e *PolicyEngine) EvaluatePolicy(ctx context.Context, policy *models.Policy, agentID string) (PolicyEvaluationResult, error) {
	if policy == nil {
		return PolicyEvaluationResult{}, errors.New("policy_engine: nil policy")
	}
	if agentID == "" {
		return PolicyEvaluationResult{}, errors.New("policy_engine: agent_id required")
	}
	if !policy.Enabled {
		return PolicyEvaluationResult{
			PolicyID:   policy.ID,
			PolicyName: policy.Name,
			AgentID:    agentID,
			Compliant:  true,
			EvaluatedAt: e.now(),
			Details:    map[string]any{"skipped": "policy_disabled"},
		}, nil
	}

	// Build the input map. Builtins are resolved via the OPA engine's
	// resolver, but we also include the most common fields inline so
	// policies can use plain Rego without depending on builtins.
	input := map[string]any{
		"agent_id": agentID,
		"policy": map[string]any{
			"id":       policy.ID,
			"name":     policy.Name,
			"category": policy.Category,
			"severity": policy.Severity,
		},
	}

	start := e.now()
	res, err := e.opa.Eval(ctx, policy.ID, policy.RegoBody, input)
	elapsed := e.now().Sub(start)

	if err != nil {
		e.log.Warn("policy eval error",
			"policy_id", policy.ID,
			"agent_id", agentID,
			"err", err)
		return PolicyEvaluationResult{}, err
	}

	out := PolicyEvaluationResult{
		PolicyID:    policy.ID,
		PolicyName:  policy.Name,
		AgentID:     agentID,
		Compliant:   res.Allowed,
		Violations:  res.Violations,
		Details:     res.Details,
		EvaluatedAt: e.now(),
		Duration:    elapsed.String(),
	}

	// Route the result through the ViolationManager when one is
	// configured. The manager handles dedup, auto-resolve, and alert
	// publication. When no manager is set, fall back to the legacy
	// direct-insert behaviour so existing deployments keep working.
	if e.violations != nil {
		if _, err := e.violations.OnViolation(ctx, policy.ID, agentID, res); err != nil {
			e.log.Warn("violation manager handling failed",
				"policy_id", policy.ID, "agent_id", agentID, "err", err)
		}
	} else if !res.Allowed {
		for _, v := range res.Violations {
			pv := &models.PolicyViolation{
				ID:       uuid.New().String(),
				PolicyID: policy.ID,
				AgentID:  agentID,
				Severity: policy.Severity,
				Message:  v.Message,
				Details:  v.Details,
				CreatedAt: e.now(),
			}
			if err := e.store.InsertPolicyViolation(ctx, pv); err != nil {
				e.log.Warn("insert policy violation failed",
					"policy_id", policy.ID, "agent_id", agentID, "err", err)
			}
		}
	}

	return out, nil
}

// EvaluateAllForAgent evaluates every active policy that is assigned
// to the agent (directly or via site) and returns the aggregate results.
func (e *PolicyEngine) EvaluateAllForAgent(ctx context.Context, agentID string) ([]PolicyEvaluationResult, error) {
	assignments, err := e.store.ListAssignmentsForAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("policy_engine: list assignments: %w", err)
	}
	out := make([]PolicyEvaluationResult, 0, len(assignments))
	for _, a := range assignments {
		pol, err := e.store.GetPolicy(ctx, "", a.PolicyID)
		if err != nil {
			e.log.Warn("get policy failed", "policy_id", a.PolicyID, "err", err)
			continue
		}
		r, err := e.EvaluatePolicy(ctx, pol, agentID)
		if err != nil {
			e.log.Warn("evaluate policy failed",
				"policy_id", a.PolicyID, "agent_id", agentID, "err", err)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// EvaluateSite evaluates every policy that is either site-assigned to
// the site or globally assigned, against every agent in the site.
func (e *PolicyEngine) EvaluateSite(ctx context.Context, siteID string) (map[string][]PolicyEvaluationResult, error) {
	agents, err := e.store.ListAgentIDsForSite(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("policy_engine: list site agents: %w", err)
	}
	out := make(map[string][]PolicyEvaluationResult, len(agents))
	for _, agentID := range agents {
		results, err := e.EvaluateAllForAgent(ctx, agentID)
		if err != nil {
			e.log.Warn("evaluate agent failed", "agent_id", agentID, "err", err)
			continue
		}
		out[agentID] = results
	}
	return out, nil
}

// EvaluateAll evaluates every active policy against every agent in the
// org. Used for the manual "evaluate-site" and "evaluate-all" API calls.
func (e *PolicyEngine) EvaluateAll(ctx context.Context, orgID string) (map[string][]PolicyEvaluationResult, error) {
	agents, err := e.store.ListAllAgentIDs(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("policy_engine: list all agents: %w", err)
	}
	out := make(map[string][]PolicyEvaluationResult, len(agents))
	for _, agentID := range agents {
		results, err := e.EvaluateAllForAgent(ctx, agentID)
		if err != nil {
			e.log.Warn("evaluate agent failed", "agent_id", agentID, "err", err)
			continue
		}
		out[agentID] = results
	}
	return out, nil
}

// EvaluatePolicyManual is the entry point for the API's
// POST /policies/{id}/evaluate endpoint. It evaluates a single policy
// against the target agent(s).
func (e *PolicyEngine) EvaluatePolicyManual(ctx context.Context, req PolicyEvaluationRequest) ([]PolicyEvaluationResult, error) {
	if req.PolicyID == "" {
		return nil, errors.New("policy_id required")
	}
	pol, err := e.store.GetPolicy(ctx, "", req.PolicyID)
	if err != nil {
		return nil, err
	}
	// Ensure the policy is in the OPA cache.
	if err := e.opa.CompilePolicy(ctx, pol.ID, pol.RegoBody); err != nil {
		return nil, fmt.Errorf("policy_engine: compile: %w", err)
	}

	if req.AgentID != "" {
		r, err := e.EvaluatePolicy(ctx, pol, req.AgentID)
		if err != nil {
			return nil, err
		}
		return []PolicyEvaluationResult{r}, nil
	}
	if req.SiteID != "" {
		agents, err := e.store.ListAgentIDsForSite(ctx, req.SiteID)
		if err != nil {
			return nil, err
		}
		out := make([]PolicyEvaluationResult, 0, len(agents))
		for _, agentID := range agents {
			r, err := e.EvaluatePolicy(ctx, pol, agentID)
			if err != nil {
				e.log.Warn("evaluate policy failed", "agent_id", agentID, "err", err)
				continue
			}
			out = append(out, r)
		}
		return out, nil
	}
	// No specific target: evaluate against every agent in the org.
	agents, err := e.store.ListAllAgentIDs(ctx, req.OrgID)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyEvaluationResult, 0, len(agents))
	for _, agentID := range agents {
		r, err := e.EvaluatePolicy(ctx, pol, agentID)
		if err != nil {
			e.log.Warn("evaluate policy failed", "agent_id", agentID, "err", err)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// CompileAndStore compiles a policy and inserts it into the store. The
// compiled query is added to the OPA cache; subsequent Evaluate calls
// reuse it.
func (e *PolicyEngine) CompileAndStore(ctx context.Context, p *models.Policy) error {
	if err := e.opa.CompilePolicy(ctx, p.ID, p.RegoBody); err != nil {
		return err
	}
	return e.store.InsertPolicy(ctx, p)
}

// UpdateAndRecompile recompiles a policy and updates the store row.
// The previous cached compiled query is invalidated first.
func (e *PolicyEngine) UpdateAndRecompile(ctx context.Context, p *models.Policy) error {
	e.opa.InvalidateCache(p.ID)
	if err := e.opa.CompilePolicy(ctx, p.ID, p.RegoBody); err != nil {
		return err
	}
	return e.store.UpdatePolicy(ctx, p)
}

// InvalidateCache removes a compiled policy from the OPA cache. API
// handlers call this after a soft-delete.
func (e *PolicyEngine) InvalidateCache(policyID string) {
	e.opa.InvalidateCache(policyID)
}

// SeedDefaults inserts the built-in Rego policies into the database if
// they don't already exist. Called from main on startup.
func (e *PolicyEngine) SeedDefaults(ctx context.Context) (seeded, skipped int, err error) {
	for name, def := range AllDefaultRegoPolicies {
		enforcement := "monitor"
		// antivirus, firewall, encryption, agent running are "enforce";
		// everything else starts in monitor.
		switch name {
		case "antivirus_installed", "firewall_enabled", "disk_encryption",
			"monitoring_agent_running", "no_suspicious_services":
			enforcement = "enforce"
		}
		// Use a deterministic ID derived from the name so re-seeding
		// updates the same row.
		policyID := "default-" + name
		existing, getErr := e.store.GetPolicy(ctx, "", policyID)
		now := e.now()
		if getErr == nil && existing != nil {
			skipped++
			// Ensure the cache reflects the current body.
			if err := e.opa.CompilePolicy(ctx, policyID, existing.RegoBody); err != nil {
				e.log.Warn("seed: compile existing policy failed", "policy_id", policyID, "err", err)
			}
			continue
		}
		if !errors.Is(getErr, ErrPolicyNotFound) {
			return seeded, skipped, fmt.Errorf("policy seed: get %s: %w", name, getErr)
		}
		p := &models.Policy{
			ID:              policyID,
			Name:            name,
			Description:     def.Description,
			RegoBody:        def.Rego,
			EnforcementMode: enforcement,
			Severity:        def.Severity,
			Category:        def.Category,
			Enabled:         true,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := e.store.InsertPolicy(ctx, p); err != nil {
			return seeded, skipped, fmt.Errorf("policy seed: insert %s: %w", name, err)
		}
		if err := e.opa.CompilePolicy(ctx, policyID, def.Rego); err != nil {
			return seeded, skipped, fmt.Errorf("policy seed: compile %s: %w", name, err)
		}
		seeded++
	}
	return seeded, skipped, nil
}

// --- Scheduler and NATS handlers ------------------------------------------

// runScheduler fires EvaluateAll on the configured interval until Stop
// is called.
