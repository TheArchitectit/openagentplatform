package tenancy

import (
	"context"
	"database/sql"
	"fmt"
)

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
