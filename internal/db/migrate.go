package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// migrationsFS holds the platform's canonical schema. It lives here because
// the Go server is the sole schema consumer and carries the truth inside its
// own binary: booting on an empty database yields a complete, current schema
// with no external step. The former deploy/migrations/ copies and the old
// Python/Alembic set are deleted — three places defining one schema is how
// the a2a-tables and policies.deleted startup failures happened (see
// openspec data-model spec KL #2, resolved).
//
// File naming is golang-migrate's convention: NNN_name.up.sql / .down.sql.
// Down files are deliberate no-ops — beta is roll-forward only, and a fresh
// database is rebuilt by destroying the compose volume.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every pending migration from the embedded set against dsn.
// Safe to call from several servers at once: the pgx migrate driver takes a
// Postgres session-level advisory lock, so concurrent boots serialise and
// late callers no-op once caught up.
//
// Uses a dedicated single connection, not the app pool: the advisory lock
// must be pinned to one session for the whole run, and DDL must not interleave
// with application queries. A statement_timeout bounds a wedged ALTER TABLE.
func Migrate(ctx context.Context, dsn string, log *slog.Logger) error {
	m, err := openMigrate(ctx, dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	v, dirty, _ := m.Version()
	log.Info("schema migrations applied", "version", v, "dirty", dirty)
	return nil
}

// openMigrate wires the embedded SQL against a dedicated, pinged Postgres
// connection. The returned handle owns that connection; the caller must
// Close() it (which also closes the underlying *sql.DB via the driver).
func openMigrate(ctx context.Context, dsn string) (*migrate.Migrate, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: migrations fs: %w", err)
	}
	src, err := iofs.New(sub, ".")
	if err != nil {
		return nil, fmt.Errorf("db: migrations source: %w", err)
	}

	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: migrate parse dsn: %w", err)
	}
	db := stdlib.OpenDB(*connCfg, stdlib.OptionBeforeConnect(
		func(_ context.Context, cc *pgx.ConnConfig) error {
			cc.RuntimeParams["statement_timeout"] = "300000" // 5 min
			return nil
		},
	))

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: migrate ping: %w", err)
	}

	drv, err := migratepgx.WithInstance(db, &migratepgx.Config{
		MigrationsTable:  "schema_migrations",
		StatementTimeout: 5 * time.Minute,
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("db: migrate driver instance: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("db: migrate new: %w", err)
	}
	m.LockTimeout = 60 * time.Second
	return m, nil
}

// MigrationStatus reports the ledger state of dsn: the current applied
// version (0 when the database has never been migrated), whether it is dirty
// (a migration failed halfway), and the newest version in the embedded
// source. Used by the cmd/migrate CLI.
func MigrationStatus(ctx context.Context, dsn string) (version uint, dirty bool, maxSource uint, err error) {
	m, err := openMigrate(ctx, dsn)
	if err != nil {
		return 0, false, 0, err
	}
	defer m.Close()

	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		version, dirty, err = 0, false, nil // never migrated — fine for a status report
	} else if err != nil {
		return 0, false, 0, fmt.Errorf("db: migrate version: %w", err)
	}

	// Newest version available in the embedded set. Computed from the file
	// names rather than the migrate source driver — golang-migrate exposes no
	// MaxSourceVersion, and the names are the canonical ordering anyway.
	names, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil {
		return version, dirty, 0, fmt.Errorf("db: migrate glob: %w", err)
	}
	for _, name := range names {
		base := name[strings.LastIndexByte(name, '/')+1:]
		n, perr := strconv.ParseUint(base[:strings.IndexByte(base, '_')], 10, 64)
		if perr != nil {
			return version, dirty, 0, fmt.Errorf("db: migrate version name %q: %w", base, perr)
		}
		if n > uint64(maxSource) {
			maxSource = uint(n)
		}
	}
	return version, dirty, maxSource, nil
}

// ForceVersion marks the ledger at version without running any SQL — the
// escape hatch out of a dirty state once a human has decided the outcome.
// Exposed for the cmd/migrate CLI.
func ForceVersion(ctx context.Context, dsn string, version int) error {
	m, err := openMigrate(ctx, dsn)
	if err != nil {
		return err
	}
	defer m.Close()
	return m.Force(version)
}
