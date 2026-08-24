package db

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Option customises the pool config before creation.
type Option func(*pgxpool.Config)

// WithTracing wires the otelpgx tracer into connection config. It must be
// applied before pool creation — pgxpool.Config is immutable afterwards,
// which is why the deprecated telemetry.TraceDB no-op could never work.
func WithTracing() Option {
	return func(cfg *pgxpool.Config) {
		cfg.ConnConfig.Tracer = otelpgx.NewTracer(
			otelpgx.WithIncludeQueryParameters(),
		)
	}
}

func NewPool(ctx context.Context, dsn string, opts ...Option) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	cfg.MaxConns = 25
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	for _, opt := range opts {
		opt(cfg)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
