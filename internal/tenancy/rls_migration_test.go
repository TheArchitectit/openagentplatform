package tenancy

import (
	"testing"
)

func TestTenantMigrationVersions(t *testing.T) {
	// Verify migrations are in order
	for i := 1; i < len(TenantMigrations); i++ {
		if TenantMigrations[i].Version <= TenantMigrations[i-1].Version {
			t.Errorf("migration %d version <= migration %d version",
				TenantMigrations[i].Version, TenantMigrations[i-1].Version)
		}
	}
}

func TestTenantMigrationSQL(t *testing.T) {
	// Verify all migrations have SQL
	for _, m := range TenantMigrations {
		if m.Up == "" {
			t.Errorf("migration %d has empty Up SQL", m.Version)
		}
		if m.Down == "" {
			t.Errorf("migration %d has empty Down SQL", m.Version)
		}
		if m.Name == "" {
			t.Errorf("migration %d has empty name", m.Version)
		}
	}
}

func TestTenantMigrationNames(t *testing.T) {
	expected := []string{
		"create_tenants_table",
		"add_tenant_id_to_tables",
		"enable_rls",
	}

	if len(TenantMigrations) != len(expected) {
		t.Fatalf("expected %d migrations, got %d", len(expected), len(TenantMigrations))
	}

	for i, name := range expected {
		if TenantMigrations[i].Name != name {
			t.Errorf("migration %d: expected name %q, got %q", i+1, name, TenantMigrations[i].Name)
		}
	}
}

func TestTenantMigrationTables(t *testing.T) {
	// Verify migration 1 creates tenants table
	m1 := TenantMigrations[0]
	if m1.Version != 1 {
		t.Errorf("expected version 1, got %d", m1.Version)
	}
	if m1.Name != "create_tenants_table" {
		t.Errorf("expected name 'create_tenants_table', got %q", m1.Name)
	}

	// Verify migration 2 adds tenant_id to tables
	m2 := TenantMigrations[1]
	if m2.Version != 2 {
		t.Errorf("expected version 2, got %d", m2.Version)
	}
	if m2.Name != "add_tenant_id_to_tables" {
		t.Errorf("expected name 'add_tenant_id_to_tables', got %q", m2.Name)
	}

	// Verify migration 3 enables RLS
	m3 := TenantMigrations[2]
	if m3.Version != 3 {
		t.Errorf("expected version 3, got %d", m3.Version)
	}
	if m3.Name != "enable_rls" {
		t.Errorf("expected name 'enable_rls', got %q", m3.Name)
	}
}

func TestRLSTables(t *testing.T) {
	// Verify all expected tables have RLS
	expectedTables := []string{
		"endpoints",
		"checks",
		"check_results",
		"alerts",
		"alert_rules",
		"policies",
		"scripts",
		"secrets",
		"secret_backends",
		"audit_log",
	}

	// Check migration 3 SQL contains all tables
	m3 := TenantMigrations[2]
	for _, table := range expectedTables {
		if !containsSubstring(m3.Up, table) {
			t.Errorf("migration 3 missing table %q", table)
		}
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRLSPolicy(t *testing.T) {
	policy := RLSPolicy{
		TableName:  "endpoints",
		PolicyName: "tenant_isolation_endpoints",
		Command:    "ALL",
		Using:      "tenant_id = current_setting('app.tenant_id')::uuid",
		WithCheck:  "tenant_id = current_setting('app.tenant_id')::uuid",
	}

	if policy.TableName != "endpoints" {
		t.Errorf("expected table name 'endpoints', got %q", policy.TableName)
	}
	if policy.Command != "ALL" {
		t.Errorf("expected command 'ALL', got %q", policy.Command)
	}
	if policy.PolicyName != "tenant_isolation_endpoints" {
		t.Errorf("expected policy name 'tenant_isolation_endpoints', got %q", policy.PolicyName)
	}
}

func TestTenantMigrationRollback(t *testing.T) {
	// Verify all migrations have rollback SQL
	for _, m := range TenantMigrations {
		if m.Down == "" {
			t.Errorf("migration %d has empty Down SQL", m.Version)
		}
	}
}

func TestTenantMigrationUpSQL(t *testing.T) {
	// Verify migration 1 creates tenants table with correct columns
	m1 := TenantMigrations[0]
	if !containsSubstring(m1.Up, "CREATE TABLE IF NOT EXISTS tenants") {
		t.Error("migration 1 missing CREATE TABLE statement")
	}
	if !containsSubstring(m1.Up, "id UUID PRIMARY KEY") {
		t.Error("migration 1 missing id column")
	}
	if !containsSubstring(m1.Up, "name VARCHAR(255)") {
		t.Error("migration 1 missing name column")
	}
	if !containsSubstring(m1.Up, "slug VARCHAR(100)") {
		t.Error("migration 1 missing slug column")
	}
}

func TestTenantMigrationDownSQL(t *testing.T) {
	// Verify migration 1 rollback drops tenants table
	m1 := TenantMigrations[0]
	if !containsSubstring(m1.Down, "DROP TABLE IF EXISTS tenants") {
		t.Error("migration 1 rollback missing DROP TABLE statement")
	}
}

func TestTenantMigrationAddTenantID(t *testing.T) {
	// Verify migration 2 adds tenant_id to all tables
	m2 := TenantMigrations[1]
	expectedTables := []string{
		"endpoints",
		"checks",
		"check_results",
		"alerts",
		"alert_rules",
		"policies",
		"scripts",
		"secrets",
		"secret_backends",
		"audit_log",
	}

	for _, table := range expectedTables {
		if !containsSubstring(m2.Up, "ALTER TABLE "+table+" ADD COLUMN IF NOT EXISTS tenant_id") {
			t.Errorf("migration 2 missing tenant_id for table %q", table)
		}
	}
}

func TestTenantMigrationEnableRLS(t *testing.T) {
	// Verify migration 3 enables RLS on all tables
	m3 := TenantMigrations[2]
	expectedTables := []string{
		"endpoints",
		"checks",
		"check_results",
		"alerts",
		"alert_rules",
		"policies",
		"scripts",
		"secrets",
		"secret_backends",
		"audit_log",
	}

	for _, table := range expectedTables {
		if !containsSubstring(m3.Up, "ALTER TABLE "+table+" ENABLE ROW LEVEL SECURITY") {
			t.Errorf("migration 3 missing ENABLE ROW LEVEL SECURITY for table %q", table)
		}
		if !containsSubstring(m3.Up, "ALTER TABLE "+table+" FORCE ROW LEVEL SECURITY") {
			t.Errorf("migration 3 missing FORCE ROW LEVEL SECURITY for table %q", table)
		}
	}
}

func TestTenantMigrationPolicies(t *testing.T) {
	// Verify migration 3 creates policies for all tables
	m3 := TenantMigrations[2]
	expectedTables := []string{
		"endpoints",
		"checks",
		"check_results",
		"alerts",
		"alert_rules",
		"policies",
		"scripts",
		"secrets",
		"secret_backends",
		"audit_log",
	}

	for _, table := range expectedTables {
		policyName := "tenant_isolation_" + table
		if !containsSubstring(m3.Up, "CREATE POLICY "+policyName+" ON "+table) {
			t.Errorf("migration 3 missing policy %q for table %q", policyName, table)
		}
	}
}
