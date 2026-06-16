package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/events"
)

// ComplianceRequest is the payload published by the dispatcher to
// request that an agent run a specific collector. It is published on
// oap.agents.<agentID>.compliance.
type ComplianceRequest struct {
	RequestID  string `json:"request_id"`
	AgentID    string `json:"agent_id"`
	Collector  string `json:"collector"`
	PolicyID   string `json:"policy_id,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// ComplianceResult is the payload published by the agent back to the
// server. It is published on oap.agents.<agentID>.compliance.results.
type ComplianceResult struct {
	RequestID  string          `json:"request_id"`
	AgentID    string          `json:"agent_id"`
	Collector  string          `json:"collector"`
	PolicyID   string          `json:"policy_id,omitempty"`
	Data       *ComplianceData `json:"data"`
	Error      string          `json:"error,omitempty"`
	ReceivedAt time.Time       `json:"received_at"`
}

// ComplianceStore is the persistence layer for compliance data. The
// dispatcher calls it after a result is received. Implementations
// typically write to the agent_compliance_data table (JSONB column).
type ComplianceStore interface {
	// StoreComplianceData persists the payload keyed by
	// (agent_id, policy_id, collector). A policy_id of "" is allowed
	// for ad-hoc collection.
	StoreComplianceData(ctx context.Context, agentID, policyID, collector string, data *ComplianceData) error
}

// PolicyReevaluator is the hook the dispatcher calls after new
// compliance data arrives. It triggers policy re-evaluation for the
// agent so the new evidence immediately affects violation state.
type PolicyReevaluator interface {
	// EvaluateAgent runs all policies assigned to the agent. The
	// dispatcher invokes it asynchronously.
	EvaluateAgent(ctx context.Context, agentID string)
}

// ComplianceDispatcher orchestrates server-side compliance data
// collection. It publishes collection requests to specific agents,
// subscribes to the matching result subject, persists the data, and
// notifies the policy engine.
//
// A single ComplianceDispatcher is safe for concurrent use; the NATS
// subscription and pending-request map are guarded by mu.
type ComplianceDispatcher struct {
	client    *events.Client
	store     ComplianceStore
	reeval    PolicyReevaluator
	log       *slog.Logger

	mu       sync.Mutex
	pending  map[string]chan *ComplianceResult // request_id -> result channel
	sub      *nats.Subscription
}

// ComplianceDispatcherConfig configures NewComplianceDispatcher.
type ComplianceDispatcherConfig struct {
	Client     *events.Client
	Store      ComplianceStore
	Reevaluator PolicyReevaluator
	Logger     *slog.Logger
}

// NewComplianceDispatcher constructs a dispatcher. The store and
// reevaluator may be nil; in that case the dispatcher still
// receives and logs results but does not persist or re-evaluate.
func NewComplianceDispatcher(cfg ComplianceDispatcherConfig) *ComplianceDispatcher {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &ComplianceDispatcher{
		client:   cfg.Client,
		store:    cfg.Store,
		reeval:   cfg.Reevaluator,
		log:      cfg.Logger,
		pending:  make(map[string]chan *ComplianceResult),
	}
}

// ComplianceRequestSubject returns the NATS subject used to request
// compliance data from a specific agent.
func ComplianceRequestSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.compliance", agentID)
}

// ComplianceResultSubjectPattern returns the wildcard pattern the
// dispatcher subscribes to in order to receive results from any agent.
func ComplianceResultSubjectPattern() string {
	return "oap.agents.*.compliance.results"
}

// Start subscribes to the result pattern. Returns an error if the
// subscription cannot be established.
func (d *ComplianceDispatcher) Start() error {
	if d.client == nil {
		return fmt.Errorf("compliance_dispatcher: nil events client")
	}
	sub, err := d.client.Subscribe(ComplianceResultSubjectPattern(), d.onResult)
	if err != nil {
		return fmt.Errorf("compliance_dispatcher: subscribe: %w", err)
	}
	d.sub = sub
	d.log.Info("compliance dispatcher started",
		"subject", ComplianceResultSubjectPattern())
	return nil
}

// Stop unsubscribes from the result pattern.
func (d *ComplianceDispatcher) Stop() {
	if d.sub != nil {
		_ = d.sub.Unsubscribe()
		d.sub = nil
	}
}

// Dispatch publishes a compliance collection request to the named
// agent and returns a channel that will receive the result. The
// channel is buffered with size 1 and is closed by the dispatcher
// after delivery. Callers that do not need to wait for the result
// may pass nil for the returned channel by ignoring it.
//
// The default timeout is 30 seconds; callers may supply a longer
// one via timeout. A timeout of zero means "wait indefinitely" and
// is generally unsafe.
func (d *ComplianceDispatcher) Dispatch(ctx context.Context, agentID, collector, policyID string, timeout time.Duration) (<-chan *ComplianceResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("compliance_dispatcher: agent_id required")
	}
	if collector == "" {
		return nil, fmt.Errorf("compliance_dispatcher: collector required")
	}
	if d.client == nil {
		return nil, fmt.Errorf("compliance_dispatcher: nil client")
	}

	req := ComplianceRequest{
		RequestID:  fmt.Sprintf("crq-%d", time.Now().UnixNano()),
		AgentID:    agentID,
		Collector:  collector,
		PolicyID:   policyID,
		TimeoutSec: int(timeout.Seconds()),
	}

	ch := make(chan *ComplianceResult, 1)
	d.mu.Lock()
	d.pending[req.RequestID] = ch
	d.mu.Unlock()

	cleanup := func() {
		d.mu.Lock()
		delete(d.pending, req.RequestID)
		d.mu.Unlock()
	}

	payload, err := json.Marshal(req)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("compliance_dispatcher: marshal: %w", err)
	}
	if err := d.client.Publish(ctx, ComplianceRequestSubject(agentID), payload); err != nil {
		cleanup()
		return nil, fmt.Errorf("compliance_dispatcher: publish: %w", err)
	}

	// If a timeout is set, schedule a cleanup goroutine that closes
	// the channel after the deadline. The channel is buffered so the
	// publisher never blocks.
	if timeout > 0 {
		go func() {
			select {
			case <-time.After(timeout):
				d.mu.Lock()
				pending, ok := d.pending[req.RequestID]
				if ok {
					delete(d.pending, req.RequestID)
				}
				d.mu.Unlock()
				if ok {
					// Push a timeout result and close.
					pending <- &ComplianceResult{
						RequestID:  req.RequestID,
						AgentID:    agentID,
						Collector:  collector,
						PolicyID:   policyID,
						Error:      "timeout",
						ReceivedAt: time.Now(),
					}
					close(pending)
				}
			}
		}()
	}

	return ch, nil
}

// onResult is the NATS message handler for compliance results.
func (d *ComplianceDispatcher) onResult(msg *nats.Msg) {
	var res ComplianceResult
	if err := json.Unmarshal(msg.Data, &res); err != nil {
		d.log.Warn("compliance result decode failed", "err", err)
		return
	}

	// Persist the data.
	if d.store != nil && res.Data != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := d.store.StoreComplianceData(ctx, res.AgentID, res.PolicyID, res.Collector, res.Data); err != nil {
			d.log.Warn("compliance store failed",
				"agent_id", res.AgentID, "collector", res.Collector, "err", err)
		}
		cancel()
	}

	// Trigger policy re-evaluation.
	if d.reeval != nil {
		go d.reeval.EvaluateAgent(context.Background(), res.AgentID)
	}

	// Deliver to any waiting caller.
	d.mu.Lock()
	ch, ok := d.pending[res.RequestID]
	if ok {
		delete(d.pending, res.RequestID)
	}
	d.mu.Unlock()
	if ok {
		ch <- &res
		close(ch)
	}
}
