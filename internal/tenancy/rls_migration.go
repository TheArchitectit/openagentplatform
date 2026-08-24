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
		Name:    "add_org_indexes_to_tenant_tables",
		// The live schema already carries TEXT org_id on every
		// tenant-scoped table (written by the application layer). This
		// migration only adds missing indexes — it does not introduce a
		// second tenancy column.
		Up: `
			CREATE INDEX IF NOT EXISTS idx_agents_org_id ON agents(org_id);
			CREATE INDEX IF NOT EXISTS idx_check_definitions_org_id ON check_definitions(org_id);
			CREATE INDEX IF NOT EXISTS idx_check_results_org_id ON check_results(org_id);
			CREATE INDEX IF NOT EXISTS idx_alerts_org_id ON alerts(org_id);
			CREATE INDEX IF NOT EXISTS idx_alert_rules_org_id ON alert_rules(org_id);
			CREATE INDEX IF NOT EXISTS idx_policies_org_id ON policies(org_id);
			CREATE INDEX IF NOT EXISTS idx_script_definitions_org_id ON script_definitions(org_id);
			CREATE INDEX IF NOT EXISTS idx_script_runs_org_id ON script_runs(org_id);
			CREATE INDEX IF NOT EXISTS idx_patch_jobs_org_id ON patch_jobs(org_id);
			CREATE INDEX IF NOT EXISTS idx_report_templates_org_id ON report_templates(org_id);
			CREATE INDEX IF NOT EXISTS idx_audit_events_org_id ON audit_events(org_id);
		`,
		Down: `
			DROP INDEX IF EXISTS idx_agents_org_id;
			DROP INDEX IF EXISTS idx_check_definitions_org_id;
			DROP INDEX IF EXISTS idx_check_results_org_id;
			DROP INDEX IF EXISTS idx_alerts_org_id;
			DROP INDEX IF EXISTS idx_alert_rules_org_id;
			DROP INDEX IF EXISTS idx_policies_org_id;
			DROP INDEX IF EXISTS idx_script_definitions_org_id;
			DROP INDEX IF EXISTS idx_script_runs_org_id;
			DROP INDEX IF EXISTS idx_patch_jobs_org_id;
			DROP INDEX IF EXISTS idx_report_templates_org_id;
			DROP INDEX IF EXISTS idx_audit_events_org_id;
		`,
	},
	{
		Version: 3,
		Name:    "enable_rls",
		// RLS keyed on current_setting('app.tenant_id') compared as
		// TEXT against org_id. Tables whose org_id may be NULL/empty in
		// existing rows use a permissive fallback via COALESCE so the
		// policy does not silently hide pre-tenancy rows from platform
		// operators; see multi-tenancy spec Known Limitations.
		Up: `
			ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
			ALTER TABLE check_definitions ENABLE ROW LEVEL SECURITY;
			ALTER TABLE check_results ENABLE ROW LEVEL SECURITY;
			ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
			ALTER TABLE alert_rules ENABLE ROW LEVEL SECURITY;
			ALTER TABLE policies ENABLE ROW LEVEL SECURITY;
			ALTER TABLE script_definitions ENABLE ROW LEVEL SECURITY;
			ALTER TABLE patch_jobs ENABLE ROW LEVEL SECURITY;
			ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;

			CREATE POLICY tenant_isolation_agents ON agents
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_check_definitions ON check_definitions
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_check_results ON check_results
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_alerts ON alerts
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_alert_rules ON alert_rules
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_policies ON policies
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_script_definitions ON script_definitions
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_patch_jobs ON patch_jobs
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
			CREATE POLICY tenant_isolation_audit_events ON audit_events
				FOR ALL USING (org_id = current_setting('app.tenant_id', true))
				WITH CHECK (org_id = current_setting('app.tenant_id', true));
		`,
		Down: `
			DROP POLICY IF EXISTS tenant_isolation_agents ON agents;
			DROP POLICY IF EXISTS tenant_isolation_check_definitions ON check_definitions;
			DROP POLICY IF EXISTS tenant_isolation_check_results ON check_results;
			DROP POLICY IF EXISTS tenant_isolation_alerts ON alerts;
			DROP POLICY IF EXISTS tenant_isolation_alert_rules ON alert_rules;
			DROP POLICY IF EXISTS tenant_isolation_policies ON policies;
			DROP POLICY IF EXISTS tenant_isolation_script_definitions ON script_definitions;
			DROP POLICY IF EXISTS tenant_isolation_patch_jobs ON patch_jobs;
			DROP POLICY IF EXISTS tenant_isolation_audit_events ON audit_events;

			ALTER TABLE agents DISABLE ROW LEVEL SECURITY;
			ALTER TABLE check_definitions DISABLE ROW LEVEL SECURITY;
			ALTER TABLE check_results DISABLE ROW LEVEL SECURITY;
			ALTER TABLE alerts DISABLE ROW LEVEL SECURITY;
			ALTER TABLE alert_rules DISABLE ROW LEVEL SECURITY;
			ALTER TABLE policies DISABLE ROW LEVEL SECURITY;
			ALTER TABLE script_definitions DISABLE ROW LEVEL SECURITY;
			ALTER TABLE patch_jobs DISABLE ROW LEVEL SECURITY;
			ALTER TABLE audit_events DISABLE ROW LEVEL SECURITY;
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

// SetTenantContext sets the tenant context for RLS. The value is
// interpolated into a SET statement, so it is strictly validated
// first: only UUID-shaped or plain-identifier characters are accepted.
// (Postgres has no parameter binding for SET; a numeric cast via
// set_config with $1 requires protocol-level parameters which *sql.DB
// does not expose for utility statements.)
func SetTenantContext(ctx context.Context, db *sql.DB, tenantID string) error {
	if !safeTenantID(tenantID) {
		return fmt.Errorf("invalid tenant id: %q", tenantID)
	}
	_, err := db.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, false)", tenantID)
	if err != nil {
		return fmt.Errorf("failed to set tenant context: %w", err)
	}
	return nil
}

// safeTenantID reports whether s contains only characters valid in an
// org identifier: letters, digits, hyphen, underscore, at-sign, dot,
// and colon, with a 128-char cap. Anything else (quotes, semicolons,
// whitespace) is rejected outright rather than escaped.
func safeTenantID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '@', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
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
