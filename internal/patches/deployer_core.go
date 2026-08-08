package patches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
