package bridge

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
)

// ============================================================
// Errors
// ============================================================

var (
	// ErrNilAdapterClient is returned when a nil AdapterClient is provided.
	ErrNilAdapterClient = errors.New("bridge: nil adapter client")

	// ErrNilGatewayForRPC is returned when a nil Gateway is provided.
	ErrNilGatewayForRPC = errors.New("bridge: nil gateway")

	// ErrTaskNotFound is returned when a task ID is not found.
	ErrTaskNotFound = errors.New("bridge: task not found")

	// ErrNoPreferredAdapter is returned when no adapter is specified and routing fails.
	ErrNoPreferredAdapter = errors.New("bridge: no preferred adapter available")
)

// ============================================================
// RPCBridge configuration
// ============================================================

// RPCConfig holds tuning parameters for the RPCBridge.

type RPCConfig struct {
	// CardSyncInterval is how often the bridge refreshes the adapter
	// list into the A2A registry. Default: 60s.
	CardSyncInterval time.Duration

	// Logger is an optional structured logger.
	Logger *slog.Logger
}

// Default RPC bridge configuration values.
const (
	defaultCardSyncInterval = 60 * time.Second
)

// ============================================================
// RPCBridge
// ============================================================

// RPCBridge ties the AdapterClient and Gateway together. It handles:
//
//   - Invoking adapters when A2A tasks are created
//   - Streaming adapter responses to Gateway SSE subscribers
//   - Cancelling adapter tasks when A2A tasks are cancelled
//   - Periodically syncing AgentCards from the adapter service
//     into the A2A registry
type RPCBridge struct {
	client *AdapterClient
	gw     *gateway.Gateway
	log    *slog.Logger
	cfg    RPCConfig

	mu      sync.Mutex
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// activeStreams tracks in-flight streaming invocations by task ID
	// so that Cancel can abort them.
	activeStreams   map[string]context.CancelFunc
	activeStreamsMu sync.Mutex
}

// NewRPCBridge constructs an RPCBridge.
