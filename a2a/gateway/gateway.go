// Package gateway - gateway.go implements the A2A Gateway, the
// public-facing entry point that wires together the TaskManager,
// AgentCard registry, and skill-based router behind a unified API.
//
// The gateway enforces authentication (Bearer, mTLS, OAuth2),
// role-based access control (a2a:send, a2a:read, a2a:admin), and
// per-client rate limiting via a token-bucket algorithm.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
	"github.com/openagentplatform/openagentplatform/a2a/registry"
	"github.com/openagentplatform/openagentplatform/a2a/router"
)

// ============================================================
// Errors
// ============================================================

var (
	// ErrNilTaskManager is returned when a nil TaskManager is provided.
	ErrNilTaskManager = errors.New("a2a gateway: nil task manager")

	// ErrNilRegistry is returned when a nil registry is provided.
	ErrNilRegistry = errors.New("a2a gateway: nil registry")

	// ErrNilRouter is returned when a nil router is provided.
	ErrNilRouter = errors.New("a2a gateway: nil router")

	// ErrPermissionDenied is returned when the caller lacks the required permission.
	ErrPermissionDenied = errors.New("a2a gateway: permission denied")

	// ErrRateLimited is returned when the caller exceeds their rate limit.
	ErrRateLimited = errors.New("a2a gateway: rate limit exceeded")

	// ErrUnauthenticated is returned when no valid credentials are present.
	ErrUnauthenticated = errors.New("a2a gateway: unauthenticated")
)

// ============================================================
// RBAC permission constants
// ============================================================

const (
	// PermSend allows creating and submitting tasks.
	PermSend = "a2a:send"

	// PermRead allows reading task status and listings.
	PermRead = "a2a:read"

	// PermAdmin allows agent registration and administrative operations.
	PermAdmin = "a2a:admin"
)

// ============================================================
// Authentication types
// ============================================================

// AuthMethod represents the authentication mechanism used by a request.
type AuthMethod string

const (
	AuthNone   AuthMethod = "none"
	AuthBearer AuthMethod = "bearer"
	AuthMTLS   AuthMethod = "mtls"
	AuthOAuth2 AuthMethod = "oauth2"
)

// Identity represents an authenticated caller extracted from a request.
type Identity struct {
	Subject  string            // user/client ID
	Method   AuthMethod        // how they authenticated
	Scopes   []string          // OAuth2 scopes or custom claims
	Metadata map[string]string // additional claims (tenant, role, etc.)
}

// HasPermission returns true if the identity's scopes include the given
// permission, or if the identity is an admin.
func (id *Identity) HasPermission(perm string) bool {
	if id == nil {
		return false
	}
	for _, s := range id.Scopes {
		if s == perm || s == PermAdmin {
			return true
		}
	}
	return false
}

// ============================================================
// Token-bucket rate limiter
// ============================================================

// rateBucket is a single client's token bucket.
type rateBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimiter implements a per-key token-bucket rate limiter.
// It is safe for concurrent use.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	rate     float64       // tokens added per second
	burst    float64       // max bucket size
	now      func() time.Time
}

// NewRateLimiter creates a rate limiter that allows `rate` requests per
// second with bursts up to `burst` tokens per key.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*rateBucket),
		rate:    rate,
		burst:   burst,
		now:     time.Now,
	}
}

// Allow consumes one token from the bucket identified by key. It returns
// true if the request is permitted, false if rate-limited.
func (rl *RateLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &rateBucket{
			tokens:     rl.burst - 1,
			lastRefill: now,
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastRefill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ============================================================
// Gateway configuration
// ============================================================

// Config holds optional gateway configuration. Zero-value defaults are
// sensible (no rate limit, no auth required).
type Config struct {
	// RateLimit is the per-client request rate (req/s). Zero = unlimited.
	RateLimit float64

	// RateBurst is the max burst size per client. Zero = no bursting.
	RateBurst float64

	// RequireAuth forces authentication on all requests.
	RequireAuth bool

	// AllowedAuthMethods restricts which auth methods are accepted.
	// Empty = all methods accepted.
	AllowedAuthMethods []AuthMethod
}

// ============================================================
// Gateway
// ============================================================

// Gateway is the central A2A facade. It wires together the business
// logic (TaskManager), agent discovery (Registry, Router), and
// transport-layer concerns (auth, RBAC, rate limiting, subscriptions).
type Gateway struct {
	tasks  *manager.TaskManager
	agents *registry.Registry
	route  *router.Router
	hub    *SubscriberHub
	auth   *Authenticator
	limiter *RateLimiter
	config Config
}

// NewGateway constructs a Gateway with the given dependencies. All
// three core components (manager, registry, router) are required.
func NewGateway(tasks *manager.TaskManager, agents *registry.Registry, rt *router.Router, cfg Config) (*Gateway, error) {
	if tasks == nil {
		return nil, ErrNilTaskManager
	}
	if agents == nil {
		return nil, ErrNilRegistry
	}
	if rt == nil {
		return nil, ErrNilRouter
	}

	var limiter *RateLimiter
	if cfg.RateLimit > 0 {
		burst := cfg.RateBurst
		if burst < 1 {
			burst = cfg.RateLimit
		}
		limiter = NewRateLimiter(cfg.RateLimit, burst)
	}

	return &Gateway{
		tasks:   tasks,
		agents:  agents,
		route:   rt,
		hub:     NewSubscriberHub(),
		auth:    NewAuthenticator(cfg),
		limiter: limiter,
		config:  cfg,
	}, nil
}

// Hub returns the SubscriberHub for this gateway.
func (g *Gateway) Hub() *SubscriberHub {
	return g.hub
}

// ============================================================
// Core business operations
// ============================================================

// SendTask creates and dispatches a new task. The caller must have
// a2a:send permission. If no agent is specified, the router selects one
// based on task tags/skills.
func (g *Gateway) SendTask(ctx context.Context, id *Identity, t *models.Task) (*models.Task, error) {
	if err := g.authorize(id, PermSend); err != nil {
		return nil, err
	}
	if err := g.checkRate(id); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("a2a gateway: nil task")
	}

	// Auto-route: if no agent specified, use the router
	agentURL := t.AgentID
	if agentURL == "" {
		card, err := g.route.Route(t, "")
		if err != nil {
			return nil, fmt.Errorf("a2a gateway: route: %w", err)
		}
		agentURL = card.Endpoint
	}

	created, err := g.tasks.CreateTask(ctx, t.ContextID, agentURL, t.Metadata)
	if err != nil {
		return nil, fmt.Errorf("a2a gateway: create task: %w", err)
	}

	// Notify subscribers
	g.hub.Publish(created.ID, models.TaskStatusUpdate{
		TaskID:    created.ID,
		Status:    created.Status,
		UpdatedAt: created.UpdatedAt,
	})

	return created, nil
}

// GetTask retrieves a task by ID. Requires a2a:read permission.
func (g *Gateway) GetTask(ctx context.Context, id *Identity, taskID string) (*models.Task, error) {
	if err := g.authorize(id, PermRead); err != nil {
		return nil, err
	}
	if err := g.checkRate(id); err != nil {
		return nil, err
	}
	return g.tasks.GetTask(ctx, taskID)
}

// ListTasks returns filtered tasks. Requires a2a:read permission.
func (g *Gateway) ListTasks(ctx context.Context, id *Identity, filter manager.TaskFilter) ([]models.Task, int, error) {
	if err := g.authorize(id, PermRead); err != nil {
		return nil, 0, err
	}
	if err := g.checkRate(id); err != nil {
		return nil, 0, err
	}
	return g.tasks.ListTasks(ctx, filter)
}

// CancelTask cancels a running task. Requires a2a:send permission.
func (g *Gateway) CancelTask(ctx context.Context, id *Identity, taskID string) error {
	if err := g.authorize(id, PermSend); err != nil {
		return err
	}
	if err := g.checkRate(id); err != nil {
		return err
	}

	// Fetch the current task to get its version for optimistic concurrency.
	current, err := g.tasks.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("a2a gateway: cancel: %w", err)
	}

	if _, err := g.tasks.CancelTask(ctx, taskID, int(current.Version)); err != nil {
		return fmt.Errorf("a2a gateway: cancel: %w", err)
	}

	// Publish cancellation event
	g.hub.Publish(taskID, models.TaskStatusUpdate{
		TaskID:    taskID,
		Status:    models.TaskStatusCancelled,
		UpdatedAt: time.Now().UTC(),
	})

	return nil
}

// RegisterAgent adds a new agent card to the registry. Requires a2a:admin.
func (g *Gateway) RegisterAgent(ctx context.Context, id *Identity, card *models.AgentCard) error {
	if err := g.authorize(id, PermAdmin); err != nil {
		return err
	}
	if err := g.checkRate(id); err != nil {
		return err
	}
	if err := card.Validate(); err != nil {
		return fmt.Errorf("a2a gateway: invalid agent card: %w", err)
	}
	return g.agents.Register(ctx, card)
}

// ListAgents returns all registered agent cards. Requires a2a:read.
func (g *Gateway) ListAgents(ctx context.Context, id *Identity) ([]models.AgentCard, error) {
	if err := g.authorize(id, PermRead); err != nil {
		return nil, err
	}
	if err := g.checkRate(id); err != nil {
		return nil, err
	}
	return g.agents.ListCards(true), nil
}

// ============================================================
// Internal helpers
// ============================================================

// authorize checks that the identity has the required permission.
func (g *Gateway) authorize(id *Identity, perm string) error {
	if !g.config.RequireAuth && id == nil {
		return nil
	}
	if id == nil {
		return ErrUnauthenticated
	}
	if !id.HasPermission(perm) {
		return fmt.Errorf("%w: requires %s", ErrPermissionDenied, perm)
	}
	return nil
}

// checkRate applies per-client rate limiting using the identity subject.
func (g *Gateway) checkRate(id *Identity) error {
	if g.limiter == nil {
		return nil
	}
	key := "anonymous"
	if id != nil {
		key = id.Subject
	}
	if !g.limiter.Allow(key) {
		return ErrRateLimited
	}
	return nil
}

// ============================================================
// Internal bridge-facing operations
// ============================================================
//
// These methods are used by internal components (such as the RPC bridge
// in a2a/bridge/rpc.go) that have already been authenticated and
// authorized at the transport layer. They skip the per-call auth
// and rate-limit checks.

// UpdateTaskStatus transitions a task to a new status via an event.
// Used by the RPC bridge to drive task lifecycle after adapter calls.
func (g *Gateway) UpdateTaskStatus(ctx context.Context, taskID, event string, version int) (*models.Task, error) {
	return g.tasks.UpdateStatus(ctx, taskID, event, version)
}

// AddMessage appends a message to a task's conversation history.
func (g *Gateway) AddMessage(ctx context.Context, taskID string, msg models.Message, version int) (*models.Task, error) {
	return g.tasks.AddMessage(ctx, taskID, msg, version)
}

// AddArtifact attaches an artifact to a task.
func (g *Gateway) AddArtifact(ctx context.Context, taskID, name, description, mimeType string, parts []models.Part) (*models.Artifact, error) {
	return g.tasks.AddArtifact(ctx, taskID, name, description, mimeType, parts)
}

// GetTaskInternal retrieves a task by ID without auth/rate checks.
// Intended for internal bridge use.
func (g *Gateway) GetTaskInternal(ctx context.Context, taskID string) (*models.Task, error) {
	return g.tasks.GetTask(ctx, taskID)
}
