package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
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

// ChecksSubject returns the NATS subject for incoming check commands.
func ChecksSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.checks", agentID)
}

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

	mu      sync.Mutex
	lastRun map[string]time.Time // key = check_id

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
		cfg:      cfg,
		agentID:  agentID,
		nc:       nc,
		log:      log,
		lastRun:  make(map[string]time.Time),
		batchBuf: make([]*CheckResultEnvelope, 0, cfg.BatchSize),
		batchCh:  make(chan struct{}, 1),
		closeCh:  make(chan struct{}),
	}
}

// Start launches the batch flusher goroutine.
func (e *ChecksExecutor) Start(ctx context.Context) {
	go e.runBatchFlusher(ctx)
}

// Close stops the batch flusher and flushes any pending results.
func (e *ChecksExecutor) Close() {
	e.closeOnce.Do(func() {
		close(e.closeCh)
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
