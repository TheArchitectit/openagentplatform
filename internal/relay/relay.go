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
	ConnectionStatusError  ConnectionStatus = "error"
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
		log:         log,
	}
	svc.matchEngine = NewMatchEngine(svc)
	svc.forwarder = NewForwarder(svc.matchEngine)
	return svc
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
	s.mu.Unlock()

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
	defer s.mu.Unlock()

	conn, ok := s.connections[connectionID]
	if !ok {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	if conn.Status != ConnectionStatusActive {
		return fmt.Errorf("connection %s is not active", connectionID)
	}

	conn.Status = ConnectionStatusClosed

	// Update metrics
	if metrics, ok := s.metrics[conn.TenantID]; ok {
		metrics.ConnectionCount--
	}

	s.log.Info("relay connection closed",
		"connection_id", connectionID,
		"tenant_id", conn.TenantID,
		"bytes_relayed", conn.BytesRelayed,
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
	defer s.mu.Unlock()

	closed := 0
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
				closed++
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
