// store.go — optional durable state for the relay (a2a-relay spec §8).
//
// Default remains in-memory (§8.1): nil Store = zero behavior change. When
// installed via SetStore, the service writes connection establish/close
// through synchronously (§8.3) and flushes byte-metering deltas on a ticker
// + graceful shutdown (§8.4); boot rehydrates tenant aggregates and closes
// crash-orphaned "active" rows (§8.5). Live legs stay in memory only (§8.6).
package relay

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the persistence seam (spec §8.1). Methods return errors; the
// service logs (never panics/aborts) on store failures — the data plane must
// not go down because the billing store blipped (§8.3).
type Store interface {
	// InsertConnection persists a newly established connection.
	InsertConnection(ctx context.Context, c *RelayConnection) error
	// MarkClosed marks the connection closed at time closedAt.
	MarkClosed(ctx context.Context, connKey string, closedAt time.Time) error
	// UpdateBytes overwrites the connection's relayed-byte count.
	UpdateBytes(ctx context.Context, connKey string, bytes int64) error
	// AddBytes adds delta to the tenant's aggregate meter.
	AddTenantBytes(ctx context.Context, tenantID string, delta int64) error
	// AddTenantConnections adds delta to the tenant's lifetime connection
	// counter (§8.5a: TotalConnections is a billing aggregate and must
	// survive restarts).
	AddTenantConnections(ctx context.Context, tenantID string, delta int64) error
	// LoadTenantMetrics returns every tenant's persisted aggregates,
	// keyed by tenant ID (rehydration, §8.5a).
	LoadTenantMetrics(ctx context.Context) (map[string]*RelayMetrics, error)
	// CloseStaleActive marks every persisted 'active' row closed
	// (crash reconciliation, §8.5b); returns how many rows it closed.
	CloseStaleActive(ctx context.Context, at time.Time) (int64, error)
	// Close flushes and releases the underlying pool.
	Close()
}

// PGStore implements Store against PostgreSQL (pgx pool).
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore builds a PGStore backed by pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// InsertConnection persists a newly established connection (§8.3).
func (s *PGStore) InsertConnection(ctx context.Context, c *RelayConnection) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO relay_connections
			(conn_key, tenant_id, source_agent_id, target_agent_id,
			 established_at, last_activity_at, bytes_relayed, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (conn_key) DO NOTHING`,
		c.ID, c.TenantID, c.SourceAgentID, c.TargetAgentID,
		c.EstablishedAt, c.LastActivityAt, c.BytesRelayed, string(c.Status))
	if err != nil {
		return fmt.Errorf("relay store: insert connection: %w", err)
	}
	return nil
}

// MarkClosed marks the connection closed (§8.3).
func (s *PGStore) MarkClosed(ctx context.Context, connKey string, closedAt time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE relay_connections SET status='closed', closed_at=$2 WHERE conn_key=$1 AND status='active'`,
		connKey, closedAt)
	if err != nil {
		return fmt.Errorf("relay store: mark closed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Unknown or already-closed key: same contract as CloseConnection's
		// no-double-close (§2.4) — report it, don't silently succeed.
		return fmt.Errorf("relay store: connection %s not found or not active", connKey)
	}
	return nil
}

// UpdateBytes overwrites the connection's byte count (flush path, §8.4).
func (s *PGStore) UpdateBytes(ctx context.Context, connKey string, bytes int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE relay_connections SET bytes_relayed=$2 WHERE conn_key=$1`,
		connKey, bytes)
	if err != nil {
		return fmt.Errorf("relay store: update bytes: %w", err)
	}
	return nil
}

// AddTenantBytes adds delta to the tenant aggregate byte meter (§8.4).
func (s *PGStore) AddTenantBytes(ctx context.Context, tenantID string, delta int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO relay_metrics (tenant_id, total_bytes_relayed, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tenant_id) DO UPDATE
		SET total_bytes_relayed = relay_metrics.total_bytes_relayed + EXCLUDED.total_bytes_relayed,
		    updated_at = now()`,
		tenantID, delta)
	if err != nil {
		return fmt.Errorf("relay store: add tenant bytes: %w", err)
	}
	return nil
}

// AddTenantConnections adds delta to the tenant's lifetime connection count
// (§8.5a). Additive upsert — never overwrites concurrent tenants' rows.
func (s *PGStore) AddTenantConnections(ctx context.Context, tenantID string, delta int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO relay_metrics (tenant_id, total_connections, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tenant_id) DO UPDATE
		SET total_connections = relay_metrics.total_connections + EXCLUDED.total_connections,
		    updated_at = now()`,
		tenantID, delta)
	if err != nil {
		return fmt.Errorf("relay store: add tenant connections: %w", err)
	}
	return nil
}

// LoadTenantMetrics rehydrates all tenant aggregates (§8.5a).
func (s *PGStore) LoadTenantMetrics(ctx context.Context) (map[string]*RelayMetrics, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tenant_id, connection_count, total_connections, total_bytes_relayed FROM relay_metrics`)
	if err != nil {
		return nil, fmt.Errorf("relay store: load metrics: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*RelayMetrics)
	for rows.Next() {
		m := &RelayMetrics{}
		if err := rows.Scan(&m.TenantID, &m.ConnectionCount, &m.TotalConnections, &m.TotalBytesRelayed); err != nil {
			return nil, fmt.Errorf("relay store: scan metrics: %w", err)
		}
		out[m.TenantID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("relay store: iterate metrics: %w", err)
	}
	return out, nil
}

// CloseStaleActive marks crash-orphaned 'active' rows closed (§8.5b).
func (s *PGStore) CloseStaleActive(ctx context.Context, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE relay_connections SET status='closed', closed_at=$1 WHERE status='active'`, at)
	if err != nil {
		return 0, fmt.Errorf("relay store: close stale active: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Close releases the underlying pool.
func (s *PGStore) Close() {
	s.pool.Close()
}
