package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openTestDB returns a live *sql.DB against the DSN in OAP_TEST_PG_DSN,
// skipping when unset. The user_org_bindings/app_state tables and the
// tenants table must exist (internal/db/migrations 001 + 004).
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

// freshDB clears the bootstrap-touched tables so each test starts from an
// uninitialized database (beta: the test DB is disposable).
func freshDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM user_org_bindings`,
		`DELETE FROM app_state`,
		`DELETE FROM tenants WHERE name LIKE 'Bootstrap Test %' OR slug LIKE 'bt-%'`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("clean (%s): %v", q, err)
		}
	}
	return db
}

func TestClaim_DisabledWhenNoExpectedToken(t *testing.T) {
	db := freshDB(t)
	s := NewStore(db)
	_, _, err := s.Claim(context.Background(), "anything", "", "Bootstrap Test A", "bt-a", "sub-1")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Claim with empty expected token: err = %v, want disabled", err)
	}
}

func TestClaim_InvalidToken(t *testing.T) {
	db := freshDB(t)
	s := NewStore(db)
	_, _, err := s.Claim(context.Background(), "wrong", "right", "Bootstrap Test B", "bt-b", "sub-2")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
	// Nothing may have been created by a rejected attempt.
	if ok, _ := s.IsComplete(context.Background()); ok {
		t.Fatal("latch set after invalid token")
	}
}

func TestClaim_FirstClaimWins(t *testing.T) {
	db := freshDB(t)
	s := NewStore(db)
	ctx := context.Background()

	orgID, created, err := s.Claim(ctx, "tok", "tok", "Bootstrap Test C", "bt-c", "sub-3")
	if err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	if !created || orgID == "" {
		t.Fatalf("first Claim = (%q,%v), want (orgID,true)", orgID, created)
	}
	if ok, err := s.IsComplete(ctx); err != nil || !ok {
		t.Fatalf("IsComplete = (%v,%v), want true,nil", ok, err)
	}

	// A second caller (even with the right token) is permanently locked out.
	if _, _, err := s.Claim(ctx, "tok", "tok", "Bootstrap Test D", "bt-d", "sub-4"); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second Claim err = %v, want ErrAlreadyInitialized", err)
	}

	// Spec §14.3: post-bootstrap rejections are 403 "regardless of token
	// value" — the latch answer wins over the token answer.
	if _, _, err := s.Claim(ctx, "wrong", "tok", "Bootstrap Test D2", "bt-d2", "sub-4b"); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("latched+wrong-token err = %v, want ErrAlreadyInitialized", err)
	}

	// The first subject is bound as admin.
	bOrg, bRole, err := s.Binding(ctx, "sub-3")
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if bOrg != orgID || bRole != "admin" {
		t.Fatalf("binding = (%q,%q), want (%q,admin)", bOrg, bRole, orgID)
	}
	// The losing subject has no binding.
	if _, _, err := s.Binding(ctx, "sub-4"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Binding(sub-4) err = %v, want sql.ErrNoRows", err)
	}
}

func TestUniqueOrgID(t *testing.T) {
	db := freshDB(t)
	s := NewStore(db)
	ctx := context.Background()

	// Zero orgs: not unique.
	if id, err := s.UniqueOrgID(ctx); err != nil || id != "" {
		t.Fatalf("UniqueOrgID empty db = (%q,%v), want (\"\",nil)", id, err)
	}

	// One org: its id.
	orgID, _, err := s.Claim(ctx, "tok", "tok", "Bootstrap Test E", "bt-e", "sub-5")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if id, err := s.UniqueOrgID(ctx); err != nil || id != orgID {
		t.Fatalf("UniqueOrgID one org = (%q,%v), want (%q,nil)", id, err, orgID)
	}

	// Two orgs: not unique. The second tenant is inserted directly (the
	// bootstrap latch blocks a second Claim).
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (name, slug) VALUES ('Bootstrap Test F', 'bt-f')`); err != nil {
		t.Fatalf("second tenant: %v", err)
	}
	if id, err := s.UniqueOrgID(ctx); err != nil || id != "" {
		t.Fatalf("UniqueOrgID two orgs = (%q,%v), want (\"\",nil)", id, err)
	}
}

func TestBind_Upsert(t *testing.T) {
	db := freshDB(t)
	s := NewStore(db)
	ctx := context.Background()

	if err := s.Bind(ctx, "sub-6", "org-x", "operator"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// Re-binding the same subject replaces the role, not errors.
	if err := s.Bind(ctx, "sub-6", "org-y", "viewer"); err != nil {
		t.Fatalf("re-Bind: %v", err)
	}
	bOrg, bRole, err := s.Binding(ctx, "sub-6")
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if bOrg != "org-y" || bRole != "viewer" {
		t.Fatalf("binding = (%q,%q), want org-y/viewer", bOrg, bRole)
	}
}
