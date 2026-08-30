package bootstrap

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
)

// ErrAlreadyInitialized means the one-time bootstrap latch is set; the
// endpoint responds 403 (auth-rbac spec §14.3).
var ErrAlreadyInitialized = errors.New("bootstrap: already initialized")

// ErrInvalidToken means the presented token failed constant-time comparison;
// the endpoint responds 401 (§14.2).
var ErrInvalidToken = errors.New("bootstrap: invalid token")

// Store owns the bootstrap control state: the app_state latch and the
// user→org bindings consulted at login. It shares the *sql.DB handle the
// tenancy stores use (server_init.go).
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// IsComplete reports whether the bootstrap latch ('bootstrap_complete' in
// app_state) is set.
func (s *Store) IsComplete(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM app_state WHERE key = 'bootstrap_complete'`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("bootstrap: read latch: %w", err)
	}
	return true, nil
}

// Claim atomically completes first-boot bootstrap: it inserts the
// bootstrap_complete latch, creates the org (tenants row), and records the
// caller as its admin — all in one transaction so a concurrent second call
// sees either nothing or the full result (spec §14.3: never duplicates).
// The tenant insert is plain SQL here (not via tenancy.TenantStore) so the
// three writes share one transaction; org_id is the TEXT convention.
func (s *Store) Claim(ctx context.Context, token, expectedToken, orgName, orgSlug, subject string) (orgID string, created bool, err error) {
	if expectedToken == "" {
		return "", false, errors.New("bootstrap: endpoint disabled (no token configured)")
	}
	if orgName == "" || orgSlug == "" || subject == "" {
		return "", false, errors.New("bootstrap: org_name, org_slug and subject are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, fmt.Errorf("bootstrap: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Latch first, BEFORE the token check: spec §14.3 says post-bootstrap
	// calls answer 403 "regardless of token value". The latch is a
	// singleton and this read locks no rows when absent, so an
	// unauthenticated-to-this-endpoint caller cannot weaponise the probe.
	// INSERT ... RETURNING id lets a loser of the race see the existing
	// row. ON CONFLICT DO NOTHING + re-read is deliberate — the conflict
	// is the normal "someone got here first" path, not an error.
	var existing string
	latchErr := tx.QueryRowContext(ctx,
		`SELECT value FROM app_state WHERE key = 'bootstrap_complete' FOR UPDATE`).Scan(&existing)
	switch {
	case latchErr == nil:
		return "", false, ErrAlreadyInitialized
	case !errors.Is(latchErr, sql.ErrNoRows):
		return "", false, fmt.Errorf("bootstrap: check latch: %w", latchErr)
	}

	// Single-org guard (spec §14.1: exactly one org): if any live tenant
	// already exists, bootstrap has no business creating a second.
	var nTenants int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM tenants WHERE deleted_at IS NULL`).Scan(&nTenants); err != nil {
		return "", false, fmt.Errorf("bootstrap: count tenants: %w", err)
	}
	if nTenants > 0 {
		return "", false, ErrAlreadyInitialized
	}

	// Constant-time token compare (spec §14.2), once we know a claim is
	// actually possible.
	if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
		return "", false, ErrInvalidToken
	}

	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO tenants (name, slug, created_at, updated_at)
		VALUES ($1, $2, now(), now())
		RETURNING id::text`, orgName, orgSlug).Scan(&id); err != nil {
		return "", false, fmt.Errorf("bootstrap: create tenant: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO user_org_bindings (subject, org_id, role) VALUES ($1, $2, 'admin')`,
		subject, id); err != nil {
		return "", false, fmt.Errorf("bootstrap: bind admin: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO app_state (key, value) VALUES ('bootstrap_complete', $1)`, id); err != nil {
		return "", false, fmt.Errorf("bootstrap: set latch: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("bootstrap: commit: %w", err)
	}
	return id, true, nil
}

// Binding returns the org + role recorded for an IdP subject, or
// sql.ErrNoRows when unbound. Login resolution consults this (spec §14.4).
func (s *Store) Binding(ctx context.Context, subject string) (orgID, role string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT org_id, role FROM user_org_bindings WHERE subject = $1`, subject).
		Scan(&orgID, &role)
	return orgID, role, err
}

// Bind records (or replaces) a subject→org binding. Used by the single-org
// auto-bind convenience and by out-of-band provisioning.
func (s *Store) Bind(ctx context.Context, subject, orgID, role string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_org_bindings (subject, org_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (subject) DO UPDATE SET org_id = $2, role = $3`,
		subject, orgID, role)
	if err != nil {
		return fmt.Errorf("bootstrap: bind: %w", err)
	}
	return nil
}

// UniqueOrgID returns the org id when exactly one live tenant exists, or ""
// otherwise — the beta auto-bind condition (spec §14.4c).
func (s *Store) UniqueOrgID(ctx context.Context) (string, error) {
	var ids []string
	rows, err := s.db.QueryContext(ctx,
		`SELECT id::text FROM tenants WHERE deleted_at IS NULL LIMIT 2`)
	if err != nil {
		return "", fmt.Errorf("bootstrap: list tenants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("bootstrap: scan tenant: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return "", nil
}
