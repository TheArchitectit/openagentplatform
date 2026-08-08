package policy

import (
	"context"
	"log/slog"
	"sync"
	"time"
	"github.com/nats-io/nats.go"
)

// SubjectPolicyEvaluate is the NATS subject the engine subscribes to
// for manual evaluation requests.
const SubjectPolicyEvaluate = "oap.events.policy.evaluate"

// PolicyEvaluationRequest is the JSON payload published on
// oap.events.policy.evaluate. Either AgentID or SiteID (or both) may
// be set; if both are empty, every agent is evaluated.

type PolicyEvaluationRequest struct {
	PolicyID  string `json:"policy_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	SiteID    string `json:"site_id,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
	Initiator string `json:"initiator,omitempty"`
}

// PolicyEvaluationResult is the outcome of evaluating a single
// (policy, agent) pair.
type PolicyEvaluationResult struct {
	PolicyID   string     `json:"policy_id"`
	PolicyName string     `json:"policy_name"`
	AgentID    string     `json:"agent_id"`
	Compliant  bool       `json:"compliant"`
	Violations []Violation `json:"violations,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	Duration   string     `json:"duration"`
}

// PolicyEngine evaluates Rego policies against agents. It subscribes
// to oap.events.policy.evaluate for manual triggers, runs scheduled
// sweeps on a configurable interval, and re-evaluates on check-result
// events from the ingest pipeline.
type PolicyEngine struct {
	store    Store
	opa      *OPAEngine
	publisher Publisher
	resolver BuiltinResolver
	log      *slog.Logger
	client   Subscriber
	now      func() time.Time

	// violations is the ViolationManager that dedupes policy failures
	// and emits policy_violation alerts on oap.events.alerts. May be
	// nil; when nil, violations are still persisted directly (legacy
	// behavior) but no alerts are raised.
	violations *ViolationManager

	// Evaluation timing.
	evalInterval    time.Duration
	batchSize       int
	stopCh          chan struct{}
	wg              sync.WaitGroup
	sub             *nats.Subscription
	evalSub         *nats.Subscription
	schedulerDoneCh chan struct{}
}

// Publisher is the subset of the events.Client interface used by the engine.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// Subscriber is the subset of the events.Client interface used by the engine.
type Subscriber interface {
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
	SubscribeQueue(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// Config configures NewEngine. All fields except Store and OPA are
// optional.
type Config struct {
	Store       Store
	OPA         *OPAEngine
	Publisher   Publisher
	Client      Subscriber
	Resolver    BuiltinResolver
	Logger      *slog.Logger
	Interval    time.Duration // Scheduled evaluation period. Default 5m.
	BatchSize   int           // Agents per batch during scheduled eval. Default 100.
	Now         func() time.Time
	QueueGroup  string
	// Violations is the optional ViolationManager. When set, the
	// engine hands every evaluation result to the manager so it can
	// dedup, auto-resolve, and publish policy_violation alerts.
	Violations *ViolationManager
}

// NewEngine constructs a PolicyEngine. Store and OPA are required.
