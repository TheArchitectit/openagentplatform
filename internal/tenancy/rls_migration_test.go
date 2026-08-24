package tenancy

import (
	"strings"
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
		"add_org_indexes_to_tenant_tables",
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

// TestRLSTables pins migration 3 against the LIVE table names. The
// original migrations targeted a different product (endpoints/checks/
// audit_log/secrets) and would have failed on first run; see the
// remediation plan W6.
func TestRLSTables(t *testing.T) {
	expectedTables := []string{
		"agents",
		"check_definitions",
		"check_results",
		"alerts",
		"alert_rules",
		"policies",
		"script_definitions",
		"patch_jobs",
		"audit_events",
	}

	m3 := TenantMigrations[2]
	for _, table := range expectedTables {
		if !strings.Contains(m3.Up, "ALTER TABLE "+table+" ENABLE ROW LEVEL SECURITY") {
			t.Errorf("migration 3 missing ENABLE ROW LEVEL SECURITY for table %q", table)
		}
		if !strings.Contains(m3.Down, "DROP POLICY IF EXISTS tenant_isolation_"+table+" ON "+table) {
			t.Errorf("migration 3 rollback missing policy drop for table %q", table)
		}
	}
}

// The rewritten migrations must never reference the diverged table
// names from the original wrong-project draft.
func TestNoDivergedTableNames(t *testing.T) {
	diverged := []string{"endpoints", "audit_log", "secret_backends"}
	for _, m := range TenantMigrations {
		for _, d := range diverged {
			if strings.Contains(m.Up, "TABLE "+d+" ") || strings.Contains(m.Up, "ON "+d+"\n") ||
				strings.Contains(m.Up, "ON "+d+" ") {
				t.Errorf("migration %d references diverged table %q", m.Version, d)
			}
		}
	}
}

// RLS policies key on TEXT org_id compared to app.tenant_id — not the
// UUID tenant_id column of the original draft.
func TestRLSUsesOrgIDTextModel(t *testing.T) {
	m3 := TenantMigrations[2]
	if strings.Contains(m3.Up, "::uuid") {
		t.Error("migration 3 still casts tenant context to uuid")
	}
	if !strings.Contains(m3.Up, "org_id = current_setting('app.tenant_id', true)") {
		t.Error("migration 3 missing org_id = current_setting policy expression")
	}
}

func TestRLSPolicy(t *testing.T) {
	policy := RLSPolicy{
		TableName:  "agents",
		PolicyName: "tenant_isolation_agents",
		Command:    "ALL",
		Using:      "org_id = current_setting('app.tenant_id', true)",
		WithCheck:  "org_id = current_setting('app.tenant_id', true)",
	}

	if policy.TableName != "agents" {
		t.Errorf("expected table name 'agents', got %q", policy.TableName)
	}
	if policy.Command != "ALL" {
		t.Errorf("expected command 'ALL', got %q", policy.Command)
	}
	if policy.PolicyName != "tenant_isolation_agents" {
		t.Errorf("expected policy name 'tenant_isolation_agents', got %q", policy.PolicyName)
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
	if !strings.Contains(m1.Up, "CREATE TABLE IF NOT EXISTS tenants") {
		t.Error("migration 1 missing CREATE TABLE statement")
	}
	if !strings.Contains(m1.Up, "id UUID PRIMARY KEY") {
		t.Error("migration 1 missing id column")
	}
	if !strings.Contains(m1.Up, "name VARCHAR(255)") {
		t.Error("migration 1 missing name column")
	}
	if !strings.Contains(m1.Up, "slug VARCHAR(100)") {
		t.Error("migration 1 missing slug column")
	}
}

func TestTenantMigrationDownSQL(t *testing.T) {
	// Verify migration 1 rollback drops tenants table
	m1 := TenantMigrations[0]
	if !strings.Contains(m1.Down, "DROP TABLE IF EXISTS tenants") {
		t.Error("migration 1 rollback missing DROP TABLE statement")
	}
}

// Migration 2 adds org_id indexes on the live tables.
func TestTenantMigrationOrgIndexes(t *testing.T) {
	m2 := TenantMigrations[1]
	expectedTables := []string{
		"agents",
		"check_definitions",
		"check_results",
		"alerts",
		"alert_rules",
		"policies",
		"script_definitions",
		"patch_jobs",
		"audit_events",
	}
	for _, table := range expectedTables {
		if !strings.Contains(m2.Up, "CREATE INDEX IF NOT EXISTS") || !strings.Contains(m2.Up, table+"(org_id)") {
			t.Errorf("migration 2 missing org_id index for table %q", table)
		}
	}
}

// SetTenantContext must reject injection payloads outright.
func TestSetTenantContextRejectsUnsafeIDs(t *testing.T) {
	cases := map[string]bool{
		"org-123":              true,
		"a-b_c@d.e:f":          true,
		"550e8400-e29b-41d4":   true,
		"":                     false,
		"org'); DROP TABLE agents;--": false,
		"org' OR '1'='1":       false,
		"org id with spaces":   false,
		"org;select":           false,
		strings.Repeat("a", 129): false,
	}
	for input, want := range cases {
		if got := safeTenantID(input); got != want {
			t.Errorf("safeTenantID(%q) = %v, want %v", input, got, want)
		}
	}
}
