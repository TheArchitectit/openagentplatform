// Package relay implements the managed A2A relay service for OpenAgentPlatform.
//
// Features:
// - Relay service for cross-network agent communication
// - Authentication on both legs (not open forwarder)
// - Per-tenant traffic isolation
// - Usage metering for billing
// - End-to-end encryption (relay cannot read secrets)
package relay

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"
)

// RelayConfig configures the relay service.
type RelayConfig struct {
	// ListenAddr is the address to listen on.
	ListenAddr string
	// TLSConfig is the TLS configuration.
	TLSConfig *tls.Config
	// MaxConnections is the maximum number of concurrent connections.
	MaxConnections int
	// IdleTimeout is the timeout for idle connections.
	IdleTimeout time.Duration
}

// RelayConnection represents a relay connection.
type RelayConnection struct {
	// ID is the connection identifier.
	ID string `json:"id"`
	// TenantID is the tenant this connection belongs to.
	TenantID string `json:"tenant_id"`
	// SourceAgentID is the source agent ID.
	SourceAgentID string `json:"source_agent_id"`
	// TargetAgentID is the target agent ID.
	TargetAgentID string `json:"target_agent_id"`
	// EstablishedAt is when the connection was established.
	EstablishedAt time.Time `json:"established_at"`
	// LastActivityAt is the last time bytes flowed through this
	// connection. Idle reaping uses this, not EstablishedAt: a long-lived
	// but actively-relaying connection must not be closed for its age.
	LastActivityAt time.Time `json:"last_activity_at"`
	// BytesRelayed is the total bytes relayed.
	BytesRelayed int64 `json:"bytes_relayed"`
	// Status is the connection status.
	Status ConnectionStatus `json:"status"`
}

// ConnectionStatus represents the connection status.
type ConnectionStatus string

const (
	ConnectionStatusActive ConnectionStatus = "active"
	ConnectionStatusClosed ConnectionStatus = "closed"
)

// RelayMetrics contains relay usage metrics.
type RelayMetrics struct {
	// TenantID is the tenant ID.
	TenantID string `json:"tenant_id"`
	// ConnectionCount is the number of active connections.
	ConnectionCount int `json:"connection_count"`
	// TotalBytesRelayed is the total bytes relayed.
	TotalBytesRelayed int64 `json:"total_bytes_relayed"`
	// TotalConnections is the total number of connections established.
	TotalConnections int64 `json:"total_connections"`
}

// RelayService manages relay connections and traffic.
type RelayService struct {
	config      RelayConfig
	connections map[string]*RelayConnection
	metrics     map[string]*RelayMetrics
	mu          sync.RWMutex
	log         *slog.Logger
	matchEngine *MatchEngine
	forwarder   *Forwarder
	trust       *TrustConfig // I.3: identity + entitlement verification (RELAY-02)
	jti         *jtiCache    // I.3: token replay prevention (RELAY-02 §1.3)
	store       Store        // optional durable state (spec §8); nil = in-memory only

	// pendingBytes buffers per-tenant metering deltas between flushes
	// (spec §8.4: periodic flush, not per-RecordBytes writes).
	pendingBytes map[string]int64
	// pendingConns buffers per-tenant lifetime-connection-count deltas
	// between flushes (§8.5a: TotalConnections survives restarts).
	pendingConns map[string]int64
}

// NewRelayService creates a new relay service.
func NewRelayService(config RelayConfig, log *slog.Logger) *RelayService {
	if log == nil {
		log = slog.Default()
	}
	svc := &RelayService{
		config:      config,
		connections: make(map[string]*RelayConnection),
		metrics:     make(map[string]*RelayMetrics),
		pendingBytes: make(map[string]int64),
		pendingConns: make(map[string]int64),
		log:         log,
	}
	svc.matchEngine = NewMatchEngine(svc)
	svc.forwarder = NewForwarder(svc.matchEngine)
	return svc
}

// SetStore installs the optional durable-state store (spec §8.1) and
// rehydrates/reconciles per §8.5: persisted tenant aggregates are loaded,
// stale 'active' rows from a previous process are marked closed. Called
// after NewRelayService, before ServeWS. A rehydration failure is logged
// and non-fatal: the relay starts with zeroed in-memory aggregates rather
// than refusing to serve.
func (s *RelayService) SetStore(ctx context.Context, store Store) error {
	s.mu.Lock()
	s.store = store
	s.mu.Unlock()

	metrics, err := store.LoadTenantMetrics(ctx)
	if err != nil {
		s.log.Warn("relay: metrics rehydration failed; starting from zero",
			"err", err)
	} else {
		s.mu.Lock()
		maps.Copy(s.metrics, metrics)
		s.mu.Unlock()
		s.log.Info("relay: tenant metrics rehydrated", "tenants", len(metrics))
	}

	if closed, err := store.CloseStaleActive(ctx, time.Now().UTC()); err != nil {
		s.log.Warn("relay: stale-active reconciliation failed", "err", err)
	} else if closed > 0 {
		s.log.Info("relay: reconciled crash-orphaned connections", "count", closed)
	}
	return nil
}

// Store returns the installed store (nil = in-memory mode).
func (s *RelayService) Store() Store { return s.store }

// FlushPendingBytes writes buffered metering deltas to the store (§8.4):
// per-tenant aggregate deltas plus a snapshot of each live connection's
// BytesRelayed, so a crash mid-connection leaves at most one flush interval
// stale in the ledger. Safe to call concurrently; a flush error keeps the
// deltas buffered for the next round.
func (s *RelayService) FlushPendingBytes(ctx context.Context) {
	if s.store == nil {
		return
	}
	s.mu.Lock()
	if len(s.pendingBytes) == 0 && len(s.pendingConns) == 0 {
		s.mu.Unlock()
		return
	}
	pending := s.pendingBytes
	s.pendingBytes = make(map[string]int64)
	pendingConns := s.pendingConns
	s.pendingConns = make(map[string]int64)
	// Snapshot the in-flight byte counts under the lock (§8.4: the flush
	// must update the connection's BytesRelayed, not only the aggregate).
	snapshots := make(map[string]int64, len(s.connections))
	for id, conn := range s.connections {
		if conn.Status == ConnectionStatusActive && conn.BytesRelayed > 0 {
			snapshots[id] = conn.BytesRelayed
		}
	}
	s.mu.Unlock()

	for tenantID, delta := range pending {
		if err := s.store.AddTenantBytes(ctx, tenantID, delta); err != nil {
			// Put the delta back so no metering is lost on a blip.
			s.mu.Lock()
			s.pendingBytes[tenantID] += delta
			s.mu.Unlock()
			s.log.Warn("relay: metrics flush failed; delta rebuffered",
				"tenant", tenantID, "delta", delta, "err", err)
		}
	}
	for tenantID, delta := range pendingConns {
		if err := s.store.AddTenantConnections(ctx, tenantID, delta); err != nil {
			s.mu.Lock()
			s.pendingConns[tenantID] += delta
			s.mu.Unlock()
			s.log.Warn("relay: connection-count flush failed; delta rebuffered",
				"tenant", tenantID, "delta", delta, "err", err)
		}
	}
	for connKey, bytes := range snapshots {
		if err := s.store.UpdateBytes(ctx, connKey, bytes); err != nil {
			// In-flight snapshot only; close/reap write the final count
			// synchronously. A failed periodic update self-heals on the
			// next flush or at close.
			s.log.Warn("relay: in-flight bytes persist failed", "connection_id", connKey, "err", err)
		}
	}
}

// StartFlushLoop runs the periodic byte-meter flush (§8.4, 30s default).
func (s *RelayService) StartFlushLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.FlushPendingBytes(ctx)
			}
		}
	}()
}

// SetTrustConfig installs the I.3 trust configuration (identity + entitlement
// verification). Must be called before ServeWS; nil leaves the service in
// fail-closed mode (every leg is rejected).
func (s *RelayService) SetTrustConfig(tc *TrustConfig) {
	s.trust = tc
	s.jti = newJtiCache()
}

// Trust returns the installed trust config (nil when unset).
func (s *RelayService) Trust() *TrustConfig {
	return s.trust
}

// MatchEngine returns the match engine.
func (s *RelayService) MatchEngine() *MatchEngine {
	return s.matchEngine
}

// Forwarder returns the forwarder.
func (s *RelayService) Forwarder() *Forwarder {
	return s.forwarder
}

// EstablishConnection establishes a relay connection between two agents.
func (s *RelayService) EstablishConnection(ctx context.Context, tenantID, sourceAgentID, targetAgentID string) (*RelayConnection, error) {
	// Validate inputs
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID is required")
	}
	if sourceAgentID == "" {
		return nil, fmt.Errorf("source agent ID is required")
	}
	if targetAgentID == "" {
		return nil, fmt.Errorf("target agent ID is required")
	}

	// The active-connection check and the insert MUST be atomic: the limit scan
	// is only correct if no concurrent establishment can interleave between
	// counting and inserting (RELAY spec §3.2 — reject once the tenant has
	// reached the limit, under concurrency). A separate RLock scan + Lock
	// insert allowed concurrent establishments to all observe the pre-cap
	// count and overshoot; the E.3 load stage caught this.
	s.mu.Lock()
	activeCount := 0
	for _, conn := range s.connections {
		if conn.TenantID == tenantID && conn.Status == ConnectionStatusActive {
			activeCount++
		}
	}
	if s.config.MaxConnections > 0 && activeCount >= s.config.MaxConnections {
		s.mu.Unlock()
		return nil, fmt.Errorf("connection limit reached for tenant %s", tenantID)
	}

	// Create connection
	connID := fmt.Sprintf("relay_%s_%s_%s_%d", tenantID, sourceAgentID, targetAgentID, time.Now().UnixNano())
	now := time.Now().UTC()
	conn := &RelayConnection{
		ID:             connID,
		TenantID:       tenantID,
		SourceAgentID:  sourceAgentID,
		TargetAgentID:  targetAgentID,
		EstablishedAt:  now,
		LastActivityAt: now,
		Status:         ConnectionStatusActive,
	}

	s.connections[connID] = conn

	// Update metrics
	metrics, ok := s.metrics[tenantID]
	if !ok {
		metrics = &RelayMetrics{TenantID: tenantID}
		s.metrics[tenantID] = metrics
	}
	metrics.ConnectionCount++
	metrics.TotalConnections++
	// Buffer the lifetime-count delta for the periodic flush (§8.5a);
	// only connections persisted to the store are counted, so a store
	// blip doesn't bill a connection the ledger never recorded.
	persisted := s.store != nil
	s.mu.Unlock()

	// Persist synchronously (spec §8.3). Failure never aborts admission —
	// the in-memory record stands and the store write is retried never; the
	// gap is bounded to this connection's ledger row (logged, not fatal).
	if persisted {
		if err := s.store.InsertConnection(ctx, conn); err != nil {
			s.log.Warn("relay: connection persist failed", "connection_id", connID, "err", err)
			persisted = false
		} else {
			s.mu.Lock()
			s.pendingConns[tenantID]++
			s.mu.Unlock()
		}
	}

	s.log.Info("relay connection established",
		"connection_id", connID,
		"tenant_id", tenantID,
		"source", sourceAgentID,
		"target", targetAgentID,
	)

	return conn, nil
}

// CloseConnection closes a relay connection.
func (s *RelayService) CloseConnection(ctx context.Context, connectionID string) error {
	s.mu.Lock()

	conn, ok := s.connections[connectionID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("connection %s not found", connectionID)
	}

	if conn.Status != ConnectionStatusActive {
		s.mu.Unlock()
		return fmt.Errorf("connection %s is not active", connectionID)
	}

	conn.Status = ConnectionStatusClosed

	// Update metrics
	if metrics, ok := s.metrics[conn.TenantID]; ok {
		metrics.ConnectionCount--
	}
	bytesRelayed := conn.BytesRelayed
	closedAt := time.Now().UTC()
	s.mu.Unlock()

	// Persist close + final byte count synchronously (spec §8.3).
	if s.store != nil {
		if err := s.store.UpdateBytes(ctx, connectionID, bytesRelayed); err != nil {
			s.log.Warn("relay: final bytes persist failed", "connection_id", connectionID, "err", err)
		}
		if err := s.store.MarkClosed(ctx, connectionID, closedAt); err != nil {
			s.log.Warn("relay: close persist failed", "connection_id", connectionID, "err", err)
		}
	}

	s.log.Info("relay connection closed",
		"connection_id", connectionID,
		"tenant_id", conn.TenantID,
		"bytes_relayed", bytesRelayed,
	)

	return nil
}

// GetConnection retrieves a connection by ID.
func (s *RelayService) GetConnection(ctx context.Context, connectionID string) (*RelayConnection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conn, ok := s.connections[connectionID]
	if !ok {
		return nil, fmt.Errorf("connection %s not found", connectionID)
	}
	return conn, nil
}

// ListConnections returns all connections for a tenant.
func (s *RelayService) ListConnections(ctx context.Context, tenantID string) []*RelayConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var conns []*RelayConnection
	for _, conn := range s.connections {
		if conn.TenantID == tenantID {
			conns = append(conns, conn)
		}
	}
	return conns
}

// RecordBytes records bytes relayed through a connection.
func (s *RelayService) RecordBytes(ctx context.Context, connectionID string, bytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, ok := s.connections[connectionID]
	if !ok {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	conn.BytesRelayed += bytes
	conn.LastActivityAt = time.Now().UTC()

	// Update metrics
	if metrics, ok := s.metrics[conn.TenantID]; ok {
		metrics.TotalBytesRelayed += bytes
	}

	// Buffer the delta for the periodic store flush (spec §8.4) — per-frame
	// writes would put the billing store on the data-plane hot path.
	if s.store != nil {
		s.pendingBytes[conn.TenantID] += bytes
	}

	return nil
}

// GetMetrics returns relay metrics for a tenant.
func (s *RelayService) GetMetrics(ctx context.Context, tenantID string) *RelayMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics, ok := s.metrics[tenantID]
	if !ok {
		return &RelayMetrics{TenantID: tenantID}
	}
	return metrics
}

// AllMetrics returns a snapshot of every tenant's metrics, keyed by tenant ID.
// It reuses existing accounting state — no new counters are created (RELAY-04
// scope boundary: no new accounting or billing export).
func (s *RelayService) AllMetrics(ctx context.Context) map[string]*RelayMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]*RelayMetrics, len(s.metrics))
	for id, m := range s.metrics {
		cp := *m
		out[id] = &cp
	}
	return out
}

// CleanupIdleConnections closes idle connections.
func (s *RelayService) CleanupIdleConnections(ctx context.Context) int {
	s.mu.Lock()

	closed := 0
	var closedKeys []string
	for _, conn := range s.connections {
		if conn.Status == ConnectionStatusActive {
			// Idle means no bytes have flowed for the timeout window — not
			// merely that the connection is old. Reaping by EstablishedAt
			// age killed long-lived healthy relays.
			last := conn.LastActivityAt
			if last.IsZero() {
				last = conn.EstablishedAt // legacy connections predating the field
			}
			if time.Since(last) > s.config.IdleTimeout {
				conn.Status = ConnectionStatusClosed
				if metrics, ok := s.metrics[conn.TenantID]; ok {
					metrics.ConnectionCount--
				}
				closedKeys = append(closedKeys, conn.ID)
				closed++
			}
		}
	}
	s.mu.Unlock()

	// Persist reaps (spec §8.3): final bytes + close, outside the lock.
	if s.store != nil && len(closedKeys) > 0 {
		for _, key := range closedKeys {
			conn, _ := s.GetConnection(ctx, key)
			if conn == nil {
				continue
			}
			if err := s.store.UpdateBytes(ctx, key, conn.BytesRelayed); err != nil {
				s.log.Warn("relay: reaped bytes persist failed", "connection_id", key, "err", err)
			}
			if err := s.store.MarkClosed(ctx, key, time.Now().UTC()); err != nil {
				s.log.Warn("relay: reaped close persist failed", "connection_id", key, "err", err)
			}
		}
	}

	if closed > 0 {
		s.log.Info("cleaned up idle connections", "count", closed)
	}

	return closed
}

// StartCleanupLoop launches a goroutine that cleans up idle connections.
func (s *RelayService) StartCleanupLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CleanupIdleConnections(ctx)
			}
		}
	}()
}
