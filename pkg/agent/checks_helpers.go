package agent

import (
	"context"
	"encoding/json"
	"expvar"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/checkers"
)

// --- Exported expvar metrics ---
var (
	metricCheckCount        = expvar.NewInt("check_count")
	metricCheckFailureCount = expvar.NewInt("check_failure_count")
	metricCheckDurationMs   = expvar.NewInt("check_duration_ms")
	metricCheckRetries      = expvar.NewInt("check_retry_count")
	metricCheckTimeouts     = expvar.NewInt("check_timeout_count")
	metricCheckBatches      = expvar.NewInt("check_batch_count")
	metricCheckSkipped      = expvar.NewInt("check_skipped_count")
)

// CheckResultEnvelope is the published response.
type CheckResultEnvelope struct {
	CheckID     string           `json:"check_id"`
	AgentID     string           `json:"agent_id"`
	Result      *checkers.Result `json:"result"`
	IssuedAt    int64            `json:"issued_at"`
	CompletedAt int64            `json:"completed_at"`
}

// ChecksResultSubject returns the NATS subject for check results.
func ChecksResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.results", agentID)
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
