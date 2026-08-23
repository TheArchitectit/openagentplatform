package secrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DBQuerier abstracts the database operations used by DBBackend,
// allowing tests to substitute a mock or real *sql.DB.
type DBQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	PingContext(ctx context.Context) error
}

// DBBackend stores secrets in a SQL database (SQLite, PostgreSQL, etc.).
// It implements SecretBackend and is suitable for single-node deployments
// where Vault/Infisical overhead is not justified.
type DBBackend struct {
	db        DBQuerier
	tableName string
}

// DBBackendConfig configures the DB-backed secret store.
type DBBackendConfig struct {
	TableName string // default: "secrets"
}

// NewDBBackend creates a DB-backed backend. The caller provides an already-open
// *sql.DB (the backend does not own connection lifecycle).
func NewDBBackend(db *sql.DB, cfg DBBackendConfig) (*DBBackend, error) {
	if db == nil {
		return nil, fmt.Errorf("db_backend: db is nil")
	}
	if cfg.TableName == "" {
		cfg.TableName = "secrets"
	}
	b := &DBBackend{db: db, tableName: cfg.TableName}
	if err := b.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("db_backend: migrate: %w", err)
	}
	return b, nil
}

// NewDBBackendFromQuerier creates a DBBackend using any DBQuerier implementation.
// Used for testing with mock databases.
func NewDBBackendFromQuerier(db DBQuerier, cfg DBBackendConfig) *DBBackend {
	if cfg.TableName == "" {
		cfg.TableName = "secrets"
	}
	return &DBBackend{db: db, tableName: cfg.TableName}
}

// migrate creates the secrets table if it doesn't exist.
func (b *DBBackend) migrate(ctx context.Context) error {
	schema := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		path TEXT NOT NULL,
		version INTEGER NOT NULL,
		data JSONB NOT NULL,
		labels JSONB,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (path, version)
	)`, b.tableName)
	_, err := b.db.ExecContext(ctx, schema)
	return err
}

// Get retrieves a secret by path and optional version.
func (b *DBBackend) Get(ctx context.Context, path string, version *int) (*SecretValue, error) {
	var row *sql.Row
	if version != nil {
		row = b.db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT path, version, data, created_at FROM %s WHERE path=$1 AND version=$2", b.tableName),
			path, *version)
	} else {
		row = b.db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT path, version, data, created_at FROM %s WHERE path=$1 ORDER BY version DESC LIMIT 1", b.tableName),
			path)
	}

	var (
		dbPath    string
		dbVersion int
		dataJSON  []byte
		createdAt time.Time
	)
	if err := row.Scan(&dbPath, &dbVersion, &dataJSON, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("secret not found: %s", path)
		}
		return nil, fmt.Errorf("db_backend get: %w", err)
	}

	data := make(map[string]any)
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		return nil, fmt.Errorf("db_backend unmarshal: %w", err)
	}

	return &SecretValue{
		Path:    dbPath,
		Version: dbVersion,
		Data:    data,
		Metadata: SecretMetadata{
			Version:   dbVersion,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
		CreatedAt: createdAt,
	}, nil
}

// Set writes a secret, auto-incrementing the version.
func (b *DBBackend) Set(ctx context.Context, path string, data map[string]any, opts SetOptions) (*SecretVersion, error) {
	if opts.CAS > 0 {
		var currentVersion int
		err := b.db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COALESCE(MAX(version),0) FROM %s WHERE path=$1", b.tableName),
			path).Scan(&currentVersion)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("db_backend cas check: %w", err)
		}
		if currentVersion != opts.CAS {
			return nil, fmt.Errorf("CAS mismatch: expected %d, got %d", opts.CAS, currentVersion)
		}
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("db_backend marshal: %w", err)
	}

	var labelsJSON []byte
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}

	// Get next version.
	var nextVersion int
	err = b.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COALESCE(MAX(version),0)+1 FROM %s WHERE path=$1", b.tableName),
		path).Scan(&nextVersion)
	if err != nil {
		return nil, fmt.Errorf("db_backend next version: %w", err)
	}

	_, err = b.db.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (path, version, data, labels, created_at) VALUES ($1,$2,$3,$4,CURRENT_TIMESTAMP)", b.tableName),
		path, nextVersion, dataJSON, labelsJSON)
	if err != nil {
		return nil, fmt.Errorf("db_backend insert: %w", err)
	}

	return &SecretVersion{Path: path, Version: nextVersion}, nil
}

// Delete removes a secret (all versions or specific versions).
func (b *DBBackend) Delete(ctx context.Context, path string, opts DeleteOptions) error {
	if len(opts.Versions) > 0 {
		placeholders := make([]string, len(opts.Versions))
		args := make([]any, 0, len(opts.Versions)+1)
		args = append(args, path)
		for i, v := range opts.Versions {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, v)
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE path=$1 AND version IN (%s)",
			b.tableName, strings.Join(placeholders, ","))
		_, err := b.db.ExecContext(ctx, query, args...)
		return err
	}

	_, err := b.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE path=$1", b.tableName), path)
	return err
}

// List enumerates secret paths under a prefix.
func (b *DBBackend) List(ctx context.Context, opts ListOptions) ([]string, error) {
	var rows *sql.Rows
	var err error
	if opts.Prefix != "" {
		rows, err = b.db.QueryContext(ctx,
			fmt.Sprintf("SELECT DISTINCT path FROM %s WHERE path LIKE $1 ORDER BY path", b.tableName),
			opts.Prefix+"%")
	} else {
		rows, err = b.db.QueryContext(ctx,
			fmt.Sprintf("SELECT DISTINCT path FROM %s ORDER BY path", b.tableName))
	}
	if err != nil {
		return nil, fmt.Errorf("db_backend list: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("db_backend list scan: %w", err)
		}
		paths = append(paths, path)
	}
	if opts.Limit > 0 && len(paths) > opts.Limit {
		paths = paths[:opts.Limit]
	}
	return paths, rows.Err()
}

// Metadata returns metadata for a secret path.
func (b *DBBackend) Metadata(ctx context.Context, path string) (*SecretMetadata, error) {
	var (
		dbVersion int
		createdAt time.Time
	)
	err := b.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT version, created_at FROM %s WHERE path=$1 ORDER BY version DESC LIMIT 1", b.tableName),
		path).Scan(&dbVersion, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("secret not found: %s", path)
		}
		return nil, fmt.Errorf("db_backend metadata: %w", err)
	}

	return &SecretMetadata{
		Version:   dbVersion,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, nil
}

// Rotate creates a new version of the secret.
func (b *DBBackend) Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error) {
	var data map[string]any
	if opts.NewData != nil {
		data = opts.NewData
	} else {
		val, err := b.Get(ctx, path, nil)
		if err != nil {
			return nil, fmt.Errorf("db_backend rotate get current: %w", err)
		}
		data = val.Data
	}

	ver, err := b.Set(ctx, path, data, SetOptions{})
	if err != nil {
		return nil, fmt.Errorf("db_backend rotate set: %w", err)
	}

	if opts.PreserveVersions > 0 {
		_, _ = b.db.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE path=$1 AND version NOT IN (SELECT version FROM %s WHERE path=$1 ORDER BY version DESC LIMIT $2)", b.tableName, b.tableName),
			path, opts.PreserveVersions)
	}

	return ver, nil
}

// Healthcheck pings the database.
func (b *DBBackend) Healthcheck(ctx context.Context) error {
	return b.db.PingContext(ctx)
}

// Close is a no-op (the caller owns the *sql.DB).
func (b *DBBackend) Close(ctx context.Context) error {
	return nil
}

// SupportsDynamic returns false (static KV store).
func (b *DBBackend) SupportsDynamic() bool {
	return false
}

// RevokeLease is a no-op for the DB backend.
func (b *DBBackend) RevokeLease(ctx context.Context, leaseID string) error {
	return ErrNotSupported
}
