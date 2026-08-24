package tenancy

import (
	"context"
	"database/sql"
	"fmt"
)

// RLSPolicy represents a Row Level Security policy.
type RLSPolicy struct {
	// TableName is the table the policy applies to.
	TableName string
	// PolicyName is the policy name.
	PolicyName string
	// Command is the SQL command (SELECT, INSERT, UPDATE, DELETE, ALL).
	Command string
	// Using is the USING expression.
	Using string
	// WithCheck is the WITH CHECK expression (for INSERT/UPDATE).
	WithCheck string
}

// TenantMigrator handles tenant migrations.
type TenantMigrator struct {
	db *sql.DB
}

// NewTenantMigrator creates a new tenant migrator.
func NewTenantMigrator(db *sql.DB) *TenantMigrator {
	return &TenantMigrator{db: db}
}

// Migrate runs all pending migrations.
func (m *TenantMigrator) Migrate(ctx context.Context) error {
	// Create migrations table if not exists
	if err := m.createMigrationsTable(ctx); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	currentVersion, err := m.currentVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Run pending migrations
	for _, migration := range TenantMigrations {
		if migration.Version <= currentVersion {
			continue
		}

		if err := m.runMigration(ctx, migration); err != nil {
			return fmt.Errorf("failed to run migration %d: %w", migration.Version, err)
		}
	}

	return nil
}

// Rollback rolls back to a specific version.
func (m *TenantMigrator) Rollback(ctx context.Context, targetVersion int) error {
	currentVersion, err := m.currentVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Rollback migrations in reverse order
	for i := len(TenantMigrations) - 1; i >= 0; i-- {
		migration := TenantMigrations[i]
		if migration.Version <= targetVersion || migration.Version > currentVersion {
			continue
		}

		if err := m.rollbackMigration(ctx, migration); err != nil {
			return fmt.Errorf("failed to rollback migration %d: %w", migration.Version, err)
		}
	}

	return nil
}

// createMigrationsTable creates the migrations tracking table.
func (m *TenantMigrator) createMigrationsTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tenant_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

// currentVersion returns the current migration version.
func (m *TenantMigrator) currentVersion(ctx context.Context) (int, error) {
	var version int
	err := m.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) FROM tenant_migrations
	`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// runMigration runs a single migration.
func (m *TenantMigrator) runMigration(ctx context.Context, migration TenantMigration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Run migration SQL
	if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record migration
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenant_migrations (version, name) VALUES ($1, $2)
	`, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// rollbackMigration rolls back a single migration.
func (m *TenantMigrator) rollbackMigration(ctx context.Context, migration TenantMigration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Run rollback SQL
	if _, err := tx.ExecContext(ctx, migration.Down); err != nil {
		return fmt.Errorf("failed to execute rollback SQL: %w", err)
	}

	// Remove migration record
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM tenant_migrations WHERE version = $1
	`, migration.Version); err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	return tx.Commit()
}
