package patches

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type StagedRollout struct {
	deployer   *PatchDeployer
	stageSizes []int // percentages; e.g. [10, 25, 50, 100]
}

// Name returns "staged".
func (s *StagedRollout) Name() string { return StrategyStaged }

// Deploy runs the staged rollout. The stages are computed by
// converting the cumulative percentage list into per-stage slices
// of targets. After each stage the engine waits StageWaitDuration
// and checks the success rate.
func (s *StagedRollout) Deploy(ctx context.Context, d *PatchDeployer, job *models.PatchJob, targets []DeployTarget) (*DeployResult, error) {
	result := &DeployResult{}
	if len(targets) == 0 {
		return result, nil
	}

	sizes := s.stageSizes
	if len(sizes) == 0 {
		sizes = d.cfg.DefaultStageSizes
	}

	// Build per-stage target lists from the cumulative percentages.
	stages := buildStageTargets(targets, sizes)
	result.Stages = make([]StageResult, 0, len(stages))

	for stageIdx, stage := range stages {
		if len(stage) == 0 {
			continue
		}
		stageStart := time.Now()
		d.log.Info("patch deploy: stage starting",
			"job_id", job.ID, "stage", stageIdx, "size", len(stage))

		stageResults := make([]TargetResult, 0, len(stage))
		stageSuccesses := 0
		for _, t := range stage {
			tr, ok := d.installWithRetries(ctx, t, job)
			stageResults = append(stageResults, tr)
			result.Targets = append(result.Targets, tr)
			if ok {
				stageSuccesses++
				result.Succeeded++
			} else {
				result.Failed++
			}
		}
		rate := float64(stageSuccesses) / float64(len(stage))
		stageRes := StageResult{
			Name:        fmt.Sprintf("stage-%d", stageIdx),
			Index:       stageIdx,
			Targets:     targetsToIDs(stage),
			Succeeded:   stageSuccesses,
			Failed:      len(stage) - stageSuccesses,
			SuccessRate: rate,
			StartedAt:   stageStart,
			FinishedAt:  time.Now(),
		}
		result.Stages = append(result.Stages, stageRes)

		// Check success rate; abort if below threshold.
		if rate < d.cfg.SuccessThreshold {
			result.Aborted = true
			result.AbortReason = fmt.Sprintf(
				"stage %d success rate %.2f below threshold %.2f",
				stageIdx, rate, d.cfg.SuccessThreshold)
			d.log.Warn("patch deploy: aborting staged rollout",
				"job_id", job.ID, "stage", stageIdx,
				"rate", rate, "threshold", d.cfg.SuccessThreshold)
			return result, nil
		}

		// Wait between stages (except after the last one).
		if stageIdx < len(stages)-1 {
			d.log.Info("patch deploy: waiting between stages",
				"job_id", job.ID, "wait", d.cfg.StageWaitDuration)
			select {
			case <-ctx.Done():
				result.Aborted = true
				result.AbortReason = "context cancelled during stage wait"
				return result, nil
			case <-time.After(d.cfg.StageWaitDuration):
			}
		}
	}
	return result, nil
}

// CanaryDeploy deploys to a small set of agents first, verifies,
// then deploys to the rest.
type CanaryDeploy struct {
	deployer *PatchDeployer
}

// Name returns "canary".
func (c *CanaryDeploy) Name() string { return StrategyCanary }

// Deploy splits the targets into a canary group (default 1 agent)
// and the rest. The canary is deployed first; if it succeeds, the
// remaining targets are deployed in parallel.
func (c *CanaryDeploy) Deploy(ctx context.Context, d *PatchDeployer, job *models.PatchJob, targets []DeployTarget) (*DeployResult, error) {
	result := &DeployResult{}
	if len(targets) == 0 {
		return result, nil
	}

	canaryN := d.cfg.CanaryCount
	if canaryN > len(targets) {
		canaryN = len(targets)
	}

	// Shuffle so the canary is not always the same target.
	shuffled := make([]DeployTarget, len(targets))
	copy(shuffled, targets)
	shuffleTargets(shuffled)

	canary := shuffled[:canaryN]
	rest := shuffled[canaryN:]

	canaryStart := time.Now()
	d.log.Info("patch deploy: canary starting",
		"job_id", job.ID, "canary_size", len(canary))

	canaryResults := make([]TargetResult, 0, len(canary))
	canarySuccesses := 0
	for _, t := range canary {
		tr, ok := d.installWithRetries(ctx, t, job)
		canaryResults = append(canaryResults, tr)
		result.Targets = append(result.Targets, tr)
		if ok {
			canarySuccesses++
			result.Succeeded++
		} else {
			result.Failed++
		}
	}
	canaryRate := float64(canarySuccesses) / float64(len(canary))
	result.Stages = append(result.Stages, StageResult{
		Name:        "canary",
		Index:       0,
		Targets:     targetsToIDs(canary),
		Succeeded:   canarySuccesses,
		Failed:      len(canary) - canarySuccesses,
		SuccessRate: canaryRate,
		StartedAt:   canaryStart,
		FinishedAt:  time.Now(),
	})

	// If canary fails, abort.
	if canaryRate < d.cfg.SuccessThreshold {
		result.Aborted = true
		result.AbortReason = fmt.Sprintf(
			"canary success rate %.2f below threshold %.2f",
			canaryRate, d.cfg.SuccessThreshold)
		d.log.Warn("patch deploy: canary failed, aborting",
			"job_id", job.ID, "rate", canaryRate)
		return result, nil
	}

	// Deploy the rest in parallel.
	if len(rest) > 0 {
		restStart := time.Now()
		d.log.Info("patch deploy: canary passed, deploying rest",
			"job_id", job.ID, "rest_size", len(rest))
		restResults, restSuccesses := d.deployParallel(ctx, rest, job)
		result.Targets = append(result.Targets, restResults...)
		result.Succeeded += restSuccesses
		result.Failed += len(rest) - restSuccesses
		result.Stages = append(result.Stages, StageResult{
			Name:        "rest",
			Index:       1,
			Targets:     targetsToIDs(rest),
			Succeeded:   restSuccesses,
			Failed:      len(rest) - restSuccesses,
			SuccessRate: successRate(restSuccesses, len(rest)),
			StartedAt:   restStart,
			FinishedAt:  time.Now(),
		})
	}
	return result, nil
}

// AllAtOnce deploys to every target simultaneously.
type AllAtOnce struct {
	deployer *PatchDeployer
}

// Name returns "all_at_once".
func (a *AllAtOnce) Name() string { return StrategyAllAtOnce }

// Deploy runs all target installs in parallel and collects results.
func (a *AllAtOnce) Deploy(ctx context.Context, d *PatchDeployer, job *models.PatchJob, targets []DeployTarget) (*DeployResult, error) {
	result := &DeployResult{}
	if len(targets) == 0 {
		return result, nil
	}
	d.log.Info("patch deploy: all-at-once starting",
		"job_id", job.ID, "targets", len(targets))
	results, successes := d.deployParallel(ctx, targets, job)
	result.Targets = results
	result.Succeeded = successes
	result.Failed = len(targets) - successes
	return result, nil
}

// deployParallel installs the patch on each target concurrently and
// returns the per-target results and the number of successes.
func (d *PatchDeployer) deployParallel(ctx context.Context, targets []DeployTarget, job *models.PatchJob) ([]TargetResult, int) {
	results := make([]TargetResult, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(idx int, target DeployTarget) {
			defer wg.Done()
			tr, _ := d.installWithRetries(ctx, target, job)
			results[idx] = tr
		}(i, t)
	}
	wg.Wait()
	return results, countSuccesses(results)
}

// buildStageTargets converts a list of cumulative percentages into
// per-stage target slices. For example, [10, 25, 50, 100] with 10
// targets yields [1, 2, 3, 4] (rounded counts). The final stage
// always includes any remaining targets.
func buildStageTargets(targets []DeployTarget, sizes []int) [][]DeployTarget {
	if len(sizes) == 0 || len(targets) == 0 {
		return nil
	}
	out := make([][]DeployTarget, 0, len(sizes))
	prev := 0
	for i, pct := range sizes {
		cum := (len(targets) * pct) / 100
		if cum <= prev {
			cum = prev + 1
		}
		if cum > len(targets) {
			cum = len(targets)
		}
		if i == len(sizes)-1 {
			cum = len(targets)
		}
		out = append(out, targets[prev:cum])
		prev = cum
		if prev >= len(targets) {
			break
		}
	}
	return out
}

// targetsToIDs extracts the agent ids from a target slice.
func targetsToIDs(targets []DeployTarget) []string {
	ids := make([]string, len(targets))
	for i, t := range targets {
		ids[i] = t.AgentID
	}
	return ids
}

// successRate returns the success rate as a float in [0, 1].
func successRate(successes, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(successes) / float64(total)
}

// shuffleTargets permutes the target slice in place using a simple
// non-cryptographic shuffle.
func shuffleTargets(targets []DeployTarget) {
	// Sort by agent id first to make the result deterministic when
	// the random seed is zero; then Fisher-Yates shuffle.
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].AgentID < targets[j].AgentID
	})
	now := time.Now().UnixNano()
	for i := len(targets) - 1; i > 0; i-- {
		j := int(now % int64(i+1))
		targets[i], targets[j] = targets[j], targets[i]
	}
}

// RebootQueue tracks pending reboots during a maintenance window.
type RebootQueue struct {
	mu      sync.Mutex
	pending []RebootRequest
	log     *slog.Logger
}

// RebootRequest represents a queued reboot for one agent.
type RebootRequest struct {
	AgentID   string
	Hostname  string
	JobID     string
	NotBefore time.Time
}

// NewRebootQueue creates an empty reboot queue.
func NewRebootQueue(log *slog.Logger) *RebootQueue {
	if log == nil {
		log = slog.Default()
	}
	return &RebootQueue{log: log}
}

// Enqueue adds a reboot request to the queue.
func (q *RebootQueue) Enqueue(r RebootRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, r)
}

// Len returns the number of pending reboot requests.
func (q *RebootQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Drain returns and clears the pending reboot requests in order.
func (q *RebootQueue) Drain() []RebootRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.pending
	q.pending = nil
	return out
}

// CoordinateReboots processes a list of reboot requests in a staggered
// sequence. It runs a pre-reboot health check on each agent, waits
// the configured stagger, then runs a post-reboot health check. The
// function respects NotBefore on each request and is safe to cancel
// via ctx.
func (d *PatchDeployer) CoordinateReboots(ctx context.Context, reboots []RebootRequest) []TargetResult {
	results := make([]TargetResult, 0, len(reboots))
	for _, r := range reboots {
		select {
		case <-ctx.Done():
			results = append(results, TargetResult{
				AgentID: r.AgentID,
				Status:  TargetStatusFailed,
				Error:   "context cancelled",
			})
			return results
		default:
		}
		if !r.NotBefore.IsZero() {
			wait := time.Until(r.NotBefore)
			if wait > 0 {
				select {
				case <-ctx.Done():
					results = append(results, TargetResult{
						AgentID: r.AgentID,
						Status:  TargetStatusFailed,
						Error:   "context cancelled during reboot wait",
					})
					return results
				case <-time.After(wait):
				}
			}
		}
		tr := TargetResult{AgentID: r.AgentID, Hostname: r.Hostname, Status: TargetStatusRunning}
		start := time.Now()
		// Pre-reboot health check.
		if err := d.runHealthCheck(ctx, r.AgentID); err != nil {
			tr.Status = TargetStatusFailed
			tr.Error = fmt.Sprintf("pre-reboot health check failed: %v", err)
			tr.Duration = time.Since(start)
			results = append(results, tr)
			d.log.Warn("reboot: pre-check failed",
				"agent_id", r.AgentID, "err", err)
			continue
		}
		// Publish the reboot directive to the agent.
		if d.nc != nil {
			rebootPayload, _ := json.Marshal(patcher.RebootCommand{
				RequestID: uuid.NewString(),
				JobID:     r.JobID,
				Reason:    "patch deployment",
				KBs:       nil, // KBs are tracked at the per-KB level
			})
			if err := d.nc.Publish(patcher.RebootSubject(r.AgentID), rebootPayload); err != nil {
				d.log.Warn("reboot: publish directive failed",
					"agent_id", r.AgentID, "err", err)
				tr.Status = TargetStatusFailed
				tr.Error = fmt.Sprintf("reboot publish failed: %v", err)
				tr.Duration = time.Since(start)
				results = append(results, tr)
				continue
			}
			d.log.Info("reboot: directive published", "agent_id", r.AgentID)
		}
		// Stagger.
		select {
		case <-ctx.Done():
			tr.Status = TargetStatusFailed
			tr.Error = "context cancelled during stagger"
			tr.Duration = time.Since(start)
			results = append(results, tr)
			return results
		case <-time.After(d.cfg.RebootStagger):
		}
		// Post-reboot health check.
		if err := d.runHealthCheck(ctx, r.AgentID); err != nil {
			tr.Status = TargetStatusFailed
			tr.Error = fmt.Sprintf("post-reboot health check failed: %v", err)
			tr.Duration = time.Since(start)
			results = append(results, tr)
			d.log.Warn("reboot: post-check failed",
				"agent_id", r.AgentID, "err", err)
			continue
		}
		tr.Status = TargetStatusSuccess
		tr.Duration = time.Since(start)
		results = append(results, tr)
		d.log.Info("reboot: coordinated",
			"agent_id", r.AgentID, "duration", tr.Duration)
	}
	return results
}
