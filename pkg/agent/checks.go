package agent

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/checkers"
)

// CheckCommand is what arrives on the agent's checks subject.
type CheckCommand struct {
	CheckID  string                 `json:"check_id"`
	Type     string                 `json:"type"`
	Target   string                 `json:"target,omitempty"`
	Timeout  int                    `json:"timeout_sec,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
	Script   string                 `json:"script,omitempty"`
	Command  string                 `json:"command,omitempty"`
	Args     []string               `json:"args,omitempty"`
	Expected string                 `json:"expected,omitempty"`

	// Scheduling hints (optional, agent may ignore).
	IntervalSec int `json:"interval_sec,omitempty"` // minimum interval between runs
}

// CheckResultEnvelope is the published response.
type CheckResultEnvelope struct {
	CheckID     string           `json:"check_id"`
	AgentID     string           `json:"agent_id"`
	Result      *checkers.Result `json:"result"`
	IssuedAt    int64            `json:"issued_at"`
	CompletedAt int64            `json:"completed_at"`
}

// ChecksSubject returns the NATS subject for incoming check commands.
func ChecksSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.checks", agentID)
}

// ChecksResultSubject returns the NATS subject for check results.
func ChecksResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.results", agentID)
}

// --- Exported expvar metrics ---
var (
	metricCheckCount       = expvar.NewInt("check_count")
	metricCheckFailureCount = expvar.NewInt("check_failure_count")
	metricCheckDurationMs  = expvar.NewInt("check_duration_ms")
	metricCheckRetries     = expvar.NewInt("check_retry_count")
	metricCheckTimeouts    = expvar.NewInt("check_timeout_count")
	metricCheckBatches     = expvar.NewInt("check_batch_count")
	metricCheckSkipped     = expvar.NewInt("check_skipped_count")
)

// ChecksConfig tunes the check executor.
type ChecksConfig struct {
	DefaultTimeoutSec int           // applied when CheckCommand.Timeout is 0
	MaxRetries        int           // total attempts on failure (default 3)
	RetryBackoff      time.Duration // base backoff (default 1s, exponential)
	BatchWindow       time.Duration // batch results for this duration (default 5s)
	BatchSize         int           // flush when buffer reaches this size
}

// DefaultChecksConfig returns sane defaults.
func DefaultChecksConfig() ChecksConfig {
	return ChecksConfig{
		DefaultTimeoutSec: 30,
		MaxRetries:        3,
		RetryBackoff:      1 * time.Second,
		BatchWindow:       5 * time.Second,
		BatchSize:         50,
	}
}

// ChecksExecutor owns batching, interval tracking, and metrics.
type ChecksExecutor struct {
	cfg     ChecksConfig
	agentID string
	nc      *NATSClient
	log     *slog.Logger

	mu          sync.Mutex
	lastRun     map[string]time.Time // key = check_id

	batchMu   sync.Mutex
	batchBuf  []*CheckResultEnvelope
	batchCh   chan struct{}
	closeCh   chan struct{}
	closed    bool
	closeOnce sync.Once
}

// NewChecksExecutor creates an executor. Call Start to begin the batch flusher.
func NewChecksExecutor(cfg ChecksConfig, agentID string, nc *NATSClient, log *slog.Logger) *ChecksExecutor {
	if cfg.DefaultTimeoutSec <= 0 {
		cfg.DefaultTimeoutSec = 30
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 1 * time.Second
	}
	if cfg.BatchWindow <= 0 {
		cfg.BatchWindow = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	return &ChecksExecutor{
		cfg:     cfg,
		agentID: agentID,
		nc:      nc,
		log:     log,
		lastRun: make(map[string]time.Time),
		batchBuf: make([]*CheckResultEnvelope, 0, cfg.BatchSize),
		batchCh: make(chan struct{}, 1),
		closeCh: make(chan struct{}),
	}
}

// Start launches the batch flusher goroutine.
func (e *ChecksExecutor) Start(ctx context.Context) {
	go e.runBatchFlusher(ctx)
}

// Close stops the batch flusher and flushes any pending results.
func (e *ChecksExecutor) Close() {
	e.closeOnce.Do(func() {
		e.closeCh <- struct{}{}
	})
}

// ShouldSkip reports whether a check should be skipped because it ran
// recently (within its requested interval). Returns the remaining wait
// duration if the check should be skipped.
func (e *ChecksExecutor) ShouldSkip(cmd *CheckCommand) (bool, time.Duration) {
	if cmd.IntervalSec <= 0 {
		return false, 0
	}
	key := cmd.CheckID
	if key == "" {
		// Use a composite key for same-type/target checks.
		key = fmt.Sprintf("%s:%s:%s", cmd.Type, cmd.Target, cmd.Expected)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if last, ok := e.lastRun[key]; ok {
		elapsed := time.Since(last)
		required := time.Duration(cmd.IntervalSec) * time.Second
		if elapsed < required {
			return true, required - elapsed
		}
	}
	return false, 0
}

// markRun records the timestamp of a successful dispatch.
func (e *ChecksExecutor) markRun(cmd *CheckCommand) {
	key := cmd.CheckID
	if key == "" {
		key = fmt.Sprintf("%s:%s:%s", cmd.Type, cmd.Target, cmd.Expected)
	}
	e.mu.Lock()
	e.lastRun[key] = time.Now()
	e.mu.Unlock()
}

// dispatch runs the check with timeout and retries, then enqueues the result.
func (e *ChecksExecutor) dispatch(ctx context.Context, agentID string, cmd *CheckCommand) {
	timeoutSec := cmd.Timeout
	if timeoutSec <= 0 {
		timeoutSec = e.cfg.DefaultTimeoutSec
	}

	var result *checkers.Result
	issuedAt := time.Now().Unix()

	for attempt := 0; attempt < e.cfg.MaxRetries; attempt++ {
		checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		start := time.Now()
		result = checkers.Run(checkCtx, &checkers.CheckRequest{
			Type:     cmd.Type,
			Target:   cmd.Target,
			Timeout:  timeoutSec,
			Options:  cmd.Options,
			Script:   cmd.Script,
			Command:  cmd.Command,
			Args:     cmd.Args,
			Expected: cmd.Expected,
		})
		cancel()

		dur := time.Since(start).Milliseconds()
		metricCheckDurationMs.Add(dur)
		metricCheckCount.Add(1)

		if result != nil && result.OK && result.Error == "" {
			break
		}
		metricCheckFailureCount.Add(1)

		if checkCtx.Err() == context.DeadlineExceeded {
			metricCheckTimeouts.Add(1)
			if result != nil && result.Error == "" {
				result.Error = "check timed out"
			}
		}

		if attempt < e.cfg.MaxRetries-1 {
			metricCheckRetries.Add(1)
			backoff := e.cfg.RetryBackoff * time.Duration(1<<attempt)
			e.log.Warn("check failed, retrying",
				"check_id", cmd.CheckID, "type", cmd.Type, "attempt", attempt+1,
				"backoff", backoff, "err", resultErrString(result))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}

	if result == nil {
		result = &checkers.Result{OK: false, Error: "all retries exhausted"}
	}

	env := &CheckResultEnvelope{
		CheckID:     cmd.CheckID,
		AgentID:     agentID,
		Result:      result,
		IssuedAt:    issuedAt,
		CompletedAt: time.Now().Unix(),
	}
	e.enqueue(ctx, env)
}

func resultErrString(r *checkers.Result) string {
	if r == nil {
		return "nil result"
	}
	return r.Error
}

// enqueue adds a result to the batch buffer, triggering a flush.
func (e *ChecksExecutor) enqueue(ctx context.Context, env *CheckResultEnvelope) {
	e.batchMu.Lock()
	e.batchBuf = append(e.batchBuf, env)
	full := len(e.batchBuf) >= e.cfg.BatchSize
	e.batchMu.Unlock()
	if full {
		select {
		case e.batchCh <- struct{}{}:
		default:
		}
	}
}

func (e *ChecksExecutor) runBatchFlusher(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.BatchWindow)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			e.flushBatch(context.Background())
			return
		case <-e.closeCh:
			e.flushBatch(context.Background())
			return
		case <-ticker.C:
			e.flushBatch(ctx)
		case <-e.batchCh:
			e.flushBatch(ctx)
		}
	}
}

func (e *ChecksExecutor) flushBatch(ctx context.Context) {
	e.batchMu.Lock()
	if len(e.batchBuf) == 0 {
		e.batchMu.Unlock()
		return
	}
	batch := e.batchBuf
	e.batchBuf = make([]*CheckResultEnvelope, 0, e.cfg.BatchSize)
	e.batchMu.Unlock()

	metricCheckBatches.Add(1)

	// Publish individually so each result lands on the standard result subject.
	// (Batching the envelope itself could be added later by switching to a
	// batch envelope schema; for now we coalesce flushes to reduce publish rate.)
	for _, env := range batch {
		data, err := json.Marshal(env)
		if err != nil {
			e.log.Warn("check result marshal failed", "err", err, "check_id", env.CheckID)
			continue
		}
		if err := e.nc.Publish(ctx, ChecksResultSubject(e.agentID), data); err != nil {
			e.log.Warn("check result publish failed", "err", err, "check_id", env.CheckID)
		}
	}
	e.log.Debug("check batch flushed", "count", len(batch))
}

// verifyPayload performs basic payload-match checks between dispatcher and
// handler. It logs a warning and returns false when the payload is malformed
// or carries an unknown check type.
func (e *ChecksExecutor) verifyPayload(data []byte) (*CheckCommand, bool) {
	var cmd CheckCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		e.log.Warn("checks: bad payload", "err", err)
		return nil, false
	}
	if cmd.Type == "" {
		e.log.Warn("checks: payload missing type", "data_len", len(data))
		return nil, false
	}
	if _, err := checkers.Get(cmd.Type); err != nil {
		e.log.Warn("checks: unknown check type", "type", cmd.Type)
		return &cmd, false // send a result back so the server learns
	}
	return &cmd, true
}

// HandleMsg processes a single raw NATS message.
func (e *ChecksExecutor) HandleMsg(ctx context.Context, agentID string, msg *nats.Msg) {
	cmd, ok := e.verifyPayload(msg.Data)
	if !ok && cmd == nil {
		return // completely unparseable
	}
	if cmd.CheckID == "" {
		cmd.CheckID = uuid.NewString()
	}

	// If payload was unknown, return a synthetic error result.
	if !ok {
		env := &CheckResultEnvelope{
			CheckID: cmd.CheckID,
			AgentID: agentID,
			Result: &checkers.Result{
				OK:    false,
				Error: fmt.Sprintf("unknown check type: %s", cmd.Type),
			},
			IssuedAt:    time.Now().Unix(),
			CompletedAt: time.Now().Unix(),
		}
		e.enqueue(ctx, env)
		return
	}

	// Interval gating.
	if skip, wait := e.ShouldSkip(cmd); skip {
		metricCheckSkipped.Add(1)
		e.log.Info("check skipped (within interval)",
			"check_id", cmd.CheckID, "type", cmd.Type, "wait", wait)
		env := &CheckResultEnvelope{
			CheckID: cmd.CheckID,
			AgentID: agentID,
			Result: &checkers.Result{
				OK:      true,
				Status:  "skipped",
				Message: fmt.Sprintf("skipped, next run in %s", wait.Round(time.Second)),
			},
			IssuedAt:    time.Now().Unix(),
			CompletedAt: time.Now().Unix(),
		}
		e.enqueue(ctx, env)
		return
	}

	e.log.Info("check received", "check_id", cmd.CheckID, "type", cmd.Type, "target", cmd.Target)
	e.markRun(cmd)
	e.dispatch(ctx, agentID, cmd)
}

// RunChecksHandler subscribes to the checks subject and dispatches each
// message to the executor. It blocks until ctx is cancelled or the
// subscription returns an error.
func RunChecksHandler(ctx context.Context, agentID string, nc *NATSClient, log *slog.Logger) (*nats.Subscription, error) {
	subject := ChecksSubject(agentID)
	exec := NewChecksExecutor(DefaultChecksConfig(), agentID, nc, log)
	exec.Start(ctx)

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		exec.HandleMsg(ctx, agentID, msg)
	})
	if err != nil {
		exec.Close()
		return nil, err
	}
	log.Info("checks handler subscribed", "subject", subject)
	return sub, nil
}

// ErrChecksPayloadInvalid indicates a payload could not be processed.
var ErrChecksPayloadInvalid = errors.New("checks: payload invalid")
