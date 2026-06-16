// Package patches - deployer.go implements the patch deployment engine.
// PatchDeployer coordinates the actual delivery of approved patches to
// target agents using pluggable deployment strategies (staged rollout,
// canary, all-at-once). It also handles post-install verification and
// reboot coordination.
package patches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Default deployer timings.
const (
	DefaultStageWaitDuration  = 15 * time.Minute
	DefaultSuccessThreshold   = 0.95 // 95% success required to continue
	DefaultMaxRetries         = 3
	DefaultInstallTimeout     = 10 * time.Minute
	DefaultHealthCheckTimeout = 60 * time.Second
	DefaultRebootStagger      = 30 * time.Second
	DefaultCanaryCount        = 1
)

// Deployment strategy names.
const (
	StrategyStaged     = "staged"
	StrategyCanary     = "canary"
	StrategyAllAtOnce  = "all_at_once"
)

// Per-target install status constants (separate from store status to
// keep the deployer's internal accounting independent of the model).
const (
	TargetStatusPending = "pending"
	TargetStatusRunning = "running"
	TargetStatusSuccess = "success"
	TargetStatusFailed  = "failed"
	TargetStatusSkipped = "skipped"
)

// DeployTarget is the minimum information the deployer needs to
// install a patch on a single endpoint.
type DeployTarget struct {
	AgentID  string
	Hostname string
}

// DeployResult is the aggregate outcome of a deployment.
type DeployResult struct {
	JobID      string          `json:"job_id"`
	Strategy   string          `json:"strategy"`
	Total      int             `json:"total"`
	Succeeded  int             `json:"succeeded"`
	Failed     int             `json:"failed"`
	Skipped    int             `json:"skipped"`
	Aborted    bool            `json:"aborted"`
	AbortReason string         `json:"abort_reason,omitempty"`
	Targets    []TargetResult  `json:"targets"`
	Stages     []StageResult   `json:"stages,omitempty"`
	Duration   time.Duration   `json:"duration_ms"`
}

// TargetResult captures the per-target install outcome.
type TargetResult struct {
	AgentID  string `json:"agent_id"`
	Hostname string `json:"hostname,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	Retries  int    `json:"retries"`
	Duration time.Duration `json:"duration_ms"`
}

// StageResult is the aggregate outcome for one stage of a staged or
// canary rollout.
type StageResult struct {
	Name       string         `json:"name"`
	Index      int            `json:"index"`
	Targets    []string       `json:"targets"`
	Succeeded  int            `json:"succeeded"`
	Failed     int            `json:"failed"`
	SuccessRate float64       `json:"success_rate"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
}

// DeploymentStrategy is the interface every rollout style
// (staged, canary, all-at-once) implements. Deploy returns once the
// strategy is complete or aborted.
type DeploymentStrategy interface {
	// Name returns the strategy name.
	Name() string
	// Deploy runs the strategy against the given targets. The
	// provided installFn is called for each target. The result is
	// always non-nil; non-fatal aborts set Aborted=true.
	Deploy(ctx context.Context, d *PatchDeployer, job *models.PatchJob, targets []DeployTarget) (*DeployResult, error)
}

// PatchDeployerConfig bundles the configurable parameters for a
// PatchDeployer.
type PatchDeployerConfig struct {
	// SuccessThreshold is the minimum success rate (0-1) required to
	// continue past a stage or accept an overall result. Default 0.95.
	SuccessThreshold float64
	// MaxRetries is the number of times a failed per-target install
	// is retried before being marked as a failure. Default 3.
	MaxRetries int
	// StageWaitDuration is the pause between stages of a staged
	// rollout. Default 15 minutes.
	StageWaitDuration time.Duration
	// DefaultStageSizes is used by the staged strategy when the job
	// has no explicit stages. Default [10, 25, 50, 100] (percentages).
	DefaultStageSizes []int
	// InstallTimeout is the per-target install timeout. Default 10m.
	InstallTimeout time.Duration
	// HealthCheckTimeout is the per-target post-install health check
	// timeout. Default 60s.
	HealthCheckTimeout time.Duration
	// RebootStagger is the delay between consecutive reboots. Default 30s.
	RebootStagger time.Duration
	// CanaryCount is the number of agents used in the first wave of
	// a canary deployment. Default 1.
	CanaryCount int
	// HealthCheckFn is an optional health check function executed
	// after each install. If nil, health checks are skipped.
	HealthCheckFn func(ctx context.Context, agentID string) error
	// RollbackFn is an optional rollback function executed when a
	// post-install verify fails. If nil, rollback is logged but not
	// performed.
	RollbackFn func(ctx context.Context, agentID string) error
	// IsAgentOnlineFn returns true if the named agent is currently
	// online. If nil, online checks are skipped (treated as always
	// online).
	IsAgentOnlineFn func(ctx context.Context, agentID string) bool
	// Logger is the slog logger. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// PatchDeployer orchestrates the delivery of a patch to its targets.
// It is stateless across jobs: callers construct one and reuse it.
type PatchDeployer struct {
	cfg PatchDeployerConfig
	nc  *nats.Conn
	log *slog.Logger
}

// NewPatchDeployer constructs a deployer with the given config and
// NATS client. Zero-valued config fields are filled with defaults.
func NewPatchDeployer(cfg PatchDeployerConfig, nc *nats.Conn) *PatchDeployer {
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = DefaultSuccessThreshold
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.StageWaitDuration <= 0 {
		cfg.StageWaitDuration = DefaultStageWaitDuration
	}
	if len(cfg.DefaultStageSizes) == 0 {
		cfg.DefaultStageSizes = []int{10, 25, 50, 100}
	}
	if cfg.InstallTimeout <= 0 {
		cfg.InstallTimeout = DefaultInstallTimeout
	}
	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = DefaultHealthCheckTimeout
	}
	if cfg.RebootStagger <= 0 {
		cfg.RebootStagger = DefaultRebootStagger
	}
	if cfg.CanaryCount <= 0 {
		cfg.CanaryCount = DefaultCanaryCount
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &PatchDeployer{cfg: cfg, nc: nc, log: cfg.Logger}
}

// Deploy selects the strategy from the job and runs it. The strategy
// is determined by PatchJob.PackageVersion or, if empty, defaults to
// the staged rollout. A job with zero targets is a no-op that returns
// an empty success result.
func (d *PatchDeployer) Deploy(ctx context.Context, job *models.PatchJob, targets []DeployTarget) (*DeployResult, error) {
	if job == nil {
		return nil, errors.New("patches: nil job")
	}
	if targets == nil {
		targets = []DeployTarget{}
	}

	strategy := d.strategyFor(job)
	d.log.Info("patch deploy: starting",
		"job_id", job.ID,
		"strategy", strategy.Name(),
		"targets", len(targets),
	)

	start := time.Now()
	result, err := strategy.Deploy(ctx, d, job, targets)
	if result != nil {
		result.JobID = job.ID
		result.Strategy = strategy.Name()
		result.Total = len(targets)
		result.Duration = time.Since(start)
	}
	if err != nil {
		d.log.Warn("patch deploy: failed",
			"job_id", job.ID, "err", err)
		return result, err
	}
	d.log.Info("patch deploy: complete",
		"job_id", job.ID,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"aborted", result.Aborted,
		"duration", result.Duration,
	)
	return result, nil
}

// strategyFor returns the strategy to use for the given job. The
// strategy is encoded in the job's Title field with a prefix:
// "staged:", "canary:", or "all_at_once:". If no prefix is present,
// the staged strategy is used.
func (d *PatchDeployer) strategyFor(job *models.PatchJob) DeploymentStrategy {
	prefix := job.Title
	for i := 0; i < len(prefix); i++ {
		if prefix[i] == ':' {
			switch prefix[:i] {
			case StrategyCanary:
				return &CanaryDeploy{deployer: d}
			case StrategyAllAtOnce:
				return &AllAtOnce{deployer: d}
			}
			break
		}
	}
	return &StagedRollout{deployer: d, stageSizes: d.cfg.DefaultStageSizes}
}

// InstallOnAgent publishes a PatchInstallCommand to the agent's
// patch_install subject and waits for the result, with timeout. It
// returns nil on success or an error describing the failure.
func (d *PatchDeployer) InstallOnAgent(ctx context.Context, agentID string, job *models.PatchJob) (*patcher.InstallResult, error) {
	if d.nc == nil {
		return nil, errors.New("patches: deployer: no nats connection")
	}
	if !d.isOnline(ctx, agentID) {
		return nil, fmt.Errorf("agent %s is offline", agentID)
	}

	requestID := uuid.NewString()
	patch := &patcher.PatchInfo{
		Name:             job.PackageName,
		AvailableVersion: job.PackageVersion,
	}
	cmd := patcher.PatchInstallCommand{
		RequestID:  requestID,
		Patch:      patch,
		TimeoutSec: int(d.cfg.InstallTimeout.Seconds()),
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("patches: marshal install cmd: %w", err)
	}

	reply, err := d.nc.Request(patcher.PatchInstallSubject(agentID), payload, d.cfg.InstallTimeout)
	if err != nil {
		return nil, fmt.Errorf("install request to %s: %w", agentID, err)
	}
	var env patcher.PatchInstallResultEnvelope
	if err := json.Unmarshal(reply.Data, &env); err != nil {
		return nil, fmt.Errorf("decode install result from %s: %w", agentID, err)
	}
	if env.Error != "" {
		return nil, fmt.Errorf("agent %s install error: %s", agentID, env.Error)
	}
	if env.Result == nil {
		return nil, fmt.Errorf("agent %s returned nil result", agentID)
	}
	return env.Result, nil
}

// isOnline returns true if the agent is currently online. If no
// IsAgentOnlineFn is configured, all agents are considered online.
func (d *PatchDeployer) isOnline(ctx context.Context, agentID string) bool {
	if d.cfg.IsAgentOnlineFn == nil {
		return true
	}
	return d.cfg.IsAgentOnlineFn(ctx, agentID)
}

// runHealthCheck invokes the configured health check for the given
// agent. A nil HealthCheckFn is treated as success.
func (d *PatchDeployer) runHealthCheck(ctx context.Context, agentID string) error {
	if d.cfg.HealthCheckFn == nil {
		return nil
	}
	hctx, cancel := context.WithTimeout(ctx, d.cfg.HealthCheckTimeout)
	defer cancel()
	return d.cfg.HealthCheckFn(hctx, agentID)
}

// runRollback invokes the configured rollback function for the given
// agent. A nil RollbackFn logs the intent but does not error.
func (d *PatchDeployer) runRollback(ctx context.Context, agentID string) error {
	if d.cfg.RollbackFn == nil {
		d.log.Warn("patch deploy: rollback needed but no rollback function configured",
			"agent_id", agentID)
		return nil
	}
	if err := d.cfg.RollbackFn(ctx, agentID); err != nil {
		d.log.Warn("patch deploy: rollback failed",
			"agent_id", agentID, "err", err)
		return err
	}
	d.log.Info("patch deploy: rolled back", "agent_id", agentID)
	return nil
}

// verifyInstall runs the post-install verification for a single
// target. It triggers a patch scan and a health check, then returns
// nil on success or an error on failure. On failure, it also calls
// the rollback function.
func (d *PatchDeployer) verifyInstall(ctx context.Context, agentID string, job *models.PatchJob) error {
	// 1. Trigger a patch scan (best-effort, log on failure).
	if d.nc != nil {
		scanPayload, err := json.Marshal(patcher.PatchScanCommand{
			RequestID:  uuid.NewString(),
			TimeoutSec: 60,
		})
		if err == nil {
			if err := d.nc.Publish(patcher.PatchScanSubject(agentID), scanPayload); err != nil {
				d.log.Warn("patch deploy: post-install scan publish failed",
					"agent_id", agentID, "err", err)
			}
		}
	}
	// 2. Health check.
	if err := d.runHealthCheck(ctx, agentID); err != nil {
		d.log.Warn("patch deploy: health check failed",
			"agent_id", agentID, "err", err)
		_ = d.runRollback(ctx, agentID)
		return fmt.Errorf("post-install health check failed: %w", err)
	}
	return nil
}

// installWithRetries installs a patch on one target, retrying on
// failure up to MaxRetries. Returns the TargetResult capturing the
// final outcome and a boolean indicating success.
func (d *PatchDeployer) installWithRetries(ctx context.Context, target DeployTarget, job *models.PatchJob) (TargetResult, bool) {
	res := TargetResult{AgentID: target.AgentID, Hostname: target.Hostname, Status: TargetStatusRunning}
	start := time.Now()
	defer func() {
		res.Duration = time.Since(start)
	}()

	var lastErr error
	for attempt := 0; attempt <= d.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			res.Retries = attempt
			d.log.Info("patch deploy: retrying",
				"agent_id", target.AgentID, "attempt", attempt)
		}
		iresult, err := d.InstallOnAgent(ctx, target.AgentID, job)
		if err == nil && iresult != nil && iresult.Success {
			// Post-install verification.
			if verr := d.verifyInstall(ctx, target.AgentID, job); verr != nil {
				lastErr = verr
				res.Status = TargetStatusFailed
				res.Error = verr.Error()
				continue
			}
			res.Status = TargetStatusSuccess
			return res, true
		}
		if err != nil {
			lastErr = err
			res.Error = err.Error()
		} else if iresult != nil {
			lastErr = fmt.Errorf("install reported failure: %s", iresult.ErrorMessage)
			res.Error = lastErr.Error()
		} else {
			lastErr = errors.New("nil install result")
			res.Error = lastErr.Error()
		}
		res.Status = TargetStatusFailed
	}
	if lastErr != nil {
		d.log.Warn("patch deploy: target failed after retries",
			"agent_id", target.AgentID, "retries", d.cfg.MaxRetries, "err", lastErr)
	}
	return res, false
}

// countSuccesses returns the number of successful target results.
func countSuccesses(results []TargetResult) int {
	n := 0
	for _, r := range results {
		if r.Status == TargetStatusSuccess {
			n++
		}
	}
	return n
}

// StagedRollout deploys in stages of increasing size. Between each
// stage the engine waits StageWaitDuration and evaluates the success
// rate; if it falls below SuccessThreshold, the deployment is
// aborted.
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
	AgentID  string
	Hostname string
	JobID    string
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
