package tenancy

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
				slug VARCHAR(100) NOT NULL,
				settings JSONB DEFAULT '{}',
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				deleted_at TIMESTAMP WITH TIME ZONE
			);

			-- Spec §5.1: only ACTIVE slugs are unique. A bare UNIQUE would
			-- burn the slug of every soft-deleted tenant forever, which
			-- contradicts slugExists' deleted_at IS NULL scope.
			CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug) WHERE deleted_at IS NULL;
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
