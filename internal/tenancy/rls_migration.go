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

// TenantMigration represents a tenant migration.
type TenantMigration struct {
	// Version is the migration version number.
	Version int
	// Name is the migration name.
	Name string
	// Up is the migration SQL.
	Up string
	// Down is the rollback SQL.
	Down string
}

// TenantMigrations contains all tenant migrations.
var TenantMigrations = []TenantMigration{
	{
		Version: 1,
		Name:    "create_tenants_table",
		Up: `
			CREATE TABLE IF NOT EXISTS tenants (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				name VARCHAR(255) NOT NULL,
				slug VARCHAR(100) NOT NULL UNIQUE,
				settings JSONB DEFAULT '{}',
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				deleted_at TIMESTAMP WITH TIME ZONE
			);

			CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug) WHERE deleted_at IS NULL;
			CREATE INDEX IF NOT EXISTS idx_tenants_deleted_at ON tenants(deleted_at) WHERE deleted_at IS NOT NULL;
		`,
		Down: `DROP TABLE IF EXISTS tenants;`,
	},
	{
		Version: 2,
		Name:    "add_tenant_id_to_tables",
		Up: `
			-- Add tenant_id to endpoints
			ALTER TABLE endpoints ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_endpoints_tenant_id ON endpoints(tenant_id);

			-- Add tenant_id to checks
			ALTER TABLE checks ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_checks_tenant_id ON checks(tenant_id);

			-- Add tenant_id to check_results
			ALTER TABLE check_results ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_check_results_tenant_id ON check_results(tenant_id);

			-- Add tenant_id to alerts
			ALTER TABLE alerts ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_alerts_tenant_id ON alerts(tenant_id);

			-- Add tenant_id to alert_rules
			ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_alert_rules_tenant_id ON alert_rules(tenant_id);

			-- Add tenant_id to policies
			ALTER TABLE policies ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_policies_tenant_id ON policies(tenant_id);

			-- Add tenant_id to scripts
			ALTER TABLE scripts ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_scripts_tenant_id ON scripts(tenant_id);

			-- Add tenant_id to secrets
			ALTER TABLE secrets ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_secrets_tenant_id ON secrets(tenant_id);

			-- Add tenant_id to secret_backends
			ALTER TABLE secret_backends ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_secret_backends_tenant_id ON secret_backends(tenant_id);

			-- Add tenant_id to audit_log
			ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
			CREATE INDEX IF NOT EXISTS idx_audit_log_tenant_id ON audit_log(tenant_id);
		`,
		Down: `
			ALTER TABLE endpoints DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE checks DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE check_results DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE alerts DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE alert_rules DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE policies DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE scripts DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE secrets DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE secret_backends DROP COLUMN IF EXISTS tenant_id;
			ALTER TABLE audit_log DROP COLUMN IF EXISTS tenant_id;
		`,
	},
	{
		Version: 3,
		Name:    "enable_rls",
		Up: `
			-- Enable RLS on all tenant-scoped tables
			ALTER TABLE endpoints ENABLE ROW LEVEL SECURITY;
			ALTER TABLE checks ENABLE ROW LEVEL SECURITY;
			ALTER TABLE check_results ENABLE ROW LEVEL SECURITY;
			ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
			ALTER TABLE alert_rules ENABLE ROW LEVEL SECURITY;
			ALTER TABLE policies ENABLE ROW LEVEL SECURITY;
			ALTER TABLE scripts ENABLE ROW LEVEL SECURITY;
			ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
			ALTER TABLE secret_backends ENABLE ROW LEVEL SECURITY;
			ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;

			-- Force RLS for table owners
			ALTER TABLE endpoints FORCE ROW LEVEL SECURITY;
			ALTER TABLE checks FORCE ROW LEVEL SECURITY;
			ALTER TABLE check_results FORCE ROW LEVEL SECURITY;
			ALTER TABLE alerts FORCE ROW LEVEL SECURITY;
			ALTER TABLE alert_rules FORCE ROW LEVEL SECURITY;
			ALTER TABLE policies FORCE ROW LEVEL SECURITY;
			ALTER TABLE scripts FORCE ROW LEVEL SECURITY;
			ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
			ALTER TABLE secret_backends FORCE ROW LEVEL SECURITY;
			ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;

			-- Create tenant isolation policies
			CREATE POLICY tenant_isolation_endpoints ON endpoints
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_checks ON checks
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_check_results ON check_results
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_alerts ON alerts
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_alert_rules ON alert_rules
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_policies ON policies
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_scripts ON scripts
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_secrets ON secrets
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_secret_backends ON secret_backends
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

			CREATE POLICY tenant_isolation_audit_log ON audit_log
				FOR ALL USING (tenant_id = current_setting('app.tenant_id')::uuid)
				WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
		`,
		Down: `
			DROP POLICY IF EXISTS tenant_isolation_endpoints ON endpoints;
			DROP POLICY IF EXISTS tenant_isolation_checks ON checks;
			DROP POLICY IF EXISTS tenant_isolation_check_results ON check_results;
			DROP POLICY IF EXISTS tenant_isolation_alerts ON alerts;
			DROP POLICY IF EXISTS tenant_isolation_alert_rules ON alert_rules;
			DROP POLICY IF EXISTS tenant_isolation_policies ON policies;
			DROP POLICY IF EXISTS tenant_isolation_scripts ON scripts;
			DROP POLICY IF EXISTS tenant_isolation_secrets ON secrets;
			DROP POLICY IF EXISTS tenant_isolation_secret_backends ON secret_backends;
			DROP POLICY IF EXISTS tenant_isolation_audit_log ON audit_log;

			ALTER TABLE endpoints DISABLE ROW LEVEL SECURITY;
			ALTER TABLE checks DISABLE ROW LEVEL SECURITY;
			ALTER TABLE check_results DISABLE ROW LEVEL SECURITY;
			ALTER TABLE alerts DISABLE ROW LEVEL SECURITY;
			ALTER TABLE alert_rules DISABLE ROW LEVEL SECURITY;
			ALTER TABLE policies DISABLE ROW LEVEL SECURITY;
			ALTER TABLE scripts DISABLE ROW LEVEL SECURITY;
			ALTER TABLE secrets DISABLE ROW LEVEL SECURITY;
			ALTER TABLE secret_backends DISABLE ROW LEVEL SECURITY;
			ALTER TABLE audit_log DISABLE ROW LEVEL SECURITY;
		`,
	},
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

// EnableRLS enables Row Level Security on a table.
func EnableRLS(ctx context.Context, db *sql.DB, tableName string) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", tableName))
	if err != nil {
		return fmt.Errorf("failed to enable RLS on %s: %w", tableName, err)
	}
	return nil
}

// ForceRLS forces RLS for table owners.
func ForceRLS(ctx context.Context, db *sql.DB, tableName string) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s FORCE ROW LEVEL SECURITY", tableName))
	if err != nil {
		return fmt.Errorf("failed to force RLS on %s: %w", tableName, err)
	}
	return nil
}

// CreatePolicy creates a Row Level Security policy.
func CreatePolicy(ctx context.Context, db *sql.DB, policy RLSPolicy) error {
	query := fmt.Sprintf(`
		CREATE POLICY %s ON %s
		FOR %s
		USING (%s)
	`, policy.PolicyName, policy.TableName, policy.Command, policy.Using)

	if policy.WithCheck != "" {
		query += fmt.Sprintf(" WITH CHECK (%s)", policy.WithCheck)
	}

	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create policy %s: %w", policy.PolicyName, err)
	}
	return nil
}

// DropPolicy drops a Row Level Security policy.
func DropPolicy(ctx context.Context, db *sql.DB, tableName, policyName string) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s", policyName, tableName))
	if err != nil {
		return fmt.Errorf("failed to drop policy %s: %w", policyName, err)
	}
	return nil
}

// SetTenantContext sets the tenant context for RLS.
func SetTenantContext(ctx context.Context, db *sql.DB, tenantID string) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("SET app.tenant_id = '%s'", tenantID))
	if err != nil {
		return fmt.Errorf("failed to set tenant context: %w", err)
	}
	return nil
}

// ClearTenantContext clears the tenant context.
func ClearTenantContext(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "RESET app.tenant_id")
	if err != nil {
		return fmt.Errorf("failed to clear tenant context: %w", err)
	}
	return nil
}

// WithTenant executes a function with tenant context set.
func WithTenant(ctx context.Context, db *sql.DB, tenantID string, fn func(ctx context.Context) error) error {
	// Set tenant context
	if err := SetTenantContext(ctx, db, tenantID); err != nil {
		return fmt.Errorf("failed to set tenant context: %w", err)
	}

	// Clear tenant context when done
	defer func() {
		ClearTenantContext(ctx, db)
	}()

	return fn(ctx)
}
