package telemetry

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TraceDB wraps the given pool's connection config with the otelpgx tracer
// so every query acquires a "db.query" span automatically.  The pool
// instance is returned unchanged; pgx reads the tracer from the pool config
// each time it acquires a new connection.
//
// Callers should pass the result back wherever they would have used the
// original pool so subsequent queries participate in the active trace.
//
// If pool is nil the function returns nil without panicking so callers can
// use the same code path in environments that omit a database.
func TraceDB(pool *pgxpool.Pool) *pgxpool.Pool {
	if pool == nil {
		return nil
	}
	cfg := pool.Config()
	if cfg.ConnConfig.Tracer == nil {
		cfg.ConnConfig.Tracer = otelpgx.NewTracer(
			otelpgx.WithIncludeQueryParameters(),
		)
	}
	return pool
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
