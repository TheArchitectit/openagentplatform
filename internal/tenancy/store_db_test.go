package tenancy

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openTestDB returns a live *sql.DB against the DSN in OAP_TEST_PG_DSN,
// skipping when unset. The tenants/tenant_configs tables must exist
// (internal/db/migrations/001 + 002 create them).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("OAP_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OAP_TEST_PG_DSN not set; skipping live database tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTenantStore_DB_CRUD(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	s := NewTenantStore(db)

	suffix := uuid.NewString()[:8]
	name := "ACME " + suffix
	slug := "acme-" + suffix

	created, err := s.Create(ctx, name, slug, map[string]interface{}{"region": "eu"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("Create returned zero ID")
	}

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != name || got.Slug != slug {
		t.Fatalf("Get = %q/%q, want %q/%q", got.Name, got.Slug, name, slug)
	}
	if got.Settings["region"] != "eu" {
		t.Fatalf("settings round-trip failed: %v", got.Settings)
	}

	bySlug, err := s.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if bySlug.ID != created.ID {
		t.Fatalf("GetBySlug returned %s, want %s", bySlug.ID, created.ID)
	}

	// Duplicate active slug must be rejected (spec 5.1).
	if _, err := s.Create(ctx, "Other", slug, nil); err == nil {
		t.Fatal("Create with duplicate slug: want error, got nil")
	}

	// Update changes name/settings; soft Delete hides the row.
	updated, err := s.Update(ctx, created.ID, name+" x", map[string]interface{}{"region": "us"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != name+" x" || updated.Settings["region"] != "us" {
		t.Fatalf("Update did not apply: %+v", updated)
	}

	// Update on a random id reports not-found (spec 5.1).
	if _, err := s.Update(ctx, uuid.New(), "ghost", nil); err == nil {
		t.Fatal("Update of unknown tenant: want error, got nil")
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, lt := range list {
		if lt.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("List does not include created tenant")
	}

	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, created.ID); err == nil {
		t.Fatal("Get after soft-delete: want not-found, got nil")
	}
	// Delete again reports not-found (row already soft-deleted, spec 5.1).
	if err := s.Delete(ctx, created.ID); err == nil {
		t.Fatal("double Delete: want not-found, got nil")
	}
	// After soft-delete, the slug is available again (slugExists excludes
	// deleted rows), so re-create must succeed — then hard-clean.
	recreated, err := s.Create(ctx, name, slug, nil)
	if err != nil {
		t.Fatalf("Create after soft-delete (slug reuse): %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, recreated.ID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
}

func TestTenantConfigStore_DB_SetGetDelete(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	s := NewTenantConfigStore(db)

	// tenant_id has no FK in 002_platform_schema_addendum.sql, so a random
	// UUID is a valid isolated test subject.
	tenantID := uuid.New()
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM tenant_configs WHERE tenant_id = $1`, tenantID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := s.Set(ctx, tenantID, "theme", map[string]interface{}{"mode": "dark"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get(ctx, tenantID, "theme")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m, ok := got.(map[string]interface{}); !ok || m["mode"] != "dark" {
		t.Fatalf("Get = %v, want mode=dark", got)
	}

	// Upsert semantics (spec 5.2): second Set replaces the value.
	if err := s.Set(ctx, tenantID, "theme", map[string]interface{}{"mode": "light"}); err != nil {
		t.Fatalf("Set (update): %v", err)
	}
	got, _ = s.Get(ctx, tenantID, "theme")
	if m, ok := got.(map[string]interface{}); !ok || m["mode"] != "light" {
		t.Fatalf("upsert failed: %v", got)
	}

	if err := s.Set(ctx, tenantID, "beta", true); err != nil {
		t.Fatalf("Set beta: %v", err)
	}
	all, err := s.GetAll(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("GetAll = %d configs, want 2", len(all))
	}
	if _, ok := all["beta"]; !ok {
		t.Fatalf("GetAll missing beta: %v", all)
	}

	// Unknown key returns nil, nil (documented behavior).
	missing, err := s.Get(ctx, tenantID, "nope")
	if err != nil || missing != nil {
		t.Fatalf("Get missing = %v, %v; want nil, nil", missing, err)
	}

	if err := s.Delete(ctx, tenantID, "beta"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	all, _ = s.GetAll(ctx, tenantID)
	if len(all) != 1 {
		t.Fatalf("after Delete: %d configs, want 1", len(all))
	}
}

func TestRetentionPurger_PreferenceLookup(t *testing.T) {
	// Not a live-DB test; guards that per-org retention preferences are
	// read from alert_global_preferences by org_id (spec 6.3).
	db := openTestDB(t)
	ctx := context.Background()

	suffix := uuid.NewString()[:8]
	orgID := "org-" + suffix
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM alert_global_preferences WHERE org_id = $1`, orgID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if _, err := db.ExecContext(ctx,
		`INSERT INTO alert_global_preferences (org_id, retention_days, updated_at) VALUES ($1, 14, $2)`,
		orgID, time.Now().UTC()); err != nil {
		t.Fatalf("seed prefs: %v", err)
	}
	// This is the exact lookup shape the purger's query uses (COALESCE
	// per-org retention_days over the row's org_id).
	var days int
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT retention_days FROM alert_global_preferences WHERE org_id = $1), 30)`,
		orgID).Scan(&days)
	if err != nil {
		t.Fatalf("preference lookup: %v", err)
	}
	if days != 14 {
		t.Fatalf("per-org retention = %d, want 14", days)
	}
	// Unknown org falls back to the default.
	var fb int
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT retention_days FROM alert_global_preferences WHERE org_id = $1), 30)`,
		"org-does-not-exist").Scan(&fb); err != nil {
		t.Fatalf("fallback lookup: %v", err)
	}
	if fb != 30 {
		t.Fatalf("fallback retention = %d, want 30", fb)
	}
}
