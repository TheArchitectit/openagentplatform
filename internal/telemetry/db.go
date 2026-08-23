package telemetry

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TraceDB is a no-op for already-created pools — pgxpool.Config is immutable
// after pool creation, so the tracer cannot be injected post-hoc.
//
// Deprecated: Use TraceDBFromDSN to wire the otelpgx tracer before pool
// creation, or set cfg.ConnConfig.Tracer in your own pgxpool.Config before
// calling pgxpool.NewWithConfig.
func TraceDB(pool *pgxpool.Pool) *pgxpool.Pool {
	return pool // no-op; tracer must be set before pool creation
}

// TraceDBFromDSN parses dsn, builds a pool, and wires the otelpgx tracer
// into the connection config before the pool is created.  Provided for
// callers that want a one-shot helper rather than wrapping an existing
// pool.
func TraceDBFromDSN(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithIncludeQueryParameters(),
	)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
