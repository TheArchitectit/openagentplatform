package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// pgCheckStore is the default Postgres-backed implementation of checkStore.
// Tables assumed: check_definitions(id, org_id, name, description, check_type,
// config jsonb, interval_seconds, timeout_seconds, enabled, created_at,
// updated_at), check_assignments(id, check_id, agent_id, site_id, assigned_by,
// created_at). Tolerates missing tables (returns empty/zero results) so the
// API can run before migrations are applied.
type pgCheckStore struct {
	pool *pgxpool.Pool
}

// ErrCheckNotFound is returned by GetCheck when the id is missing.
var ErrCheckNotFound = errors.New("check not found")

// CheckListFilter is the filter applied to ListChecks.
type CheckListFilter struct {
	OrgID     string
	CheckType string
	Enabled   *bool
	Search    string
	Limit     int
	Offset    int
}

// InsertCheck creates a new check definition. Returns the persisted row
// (with DB-populated timestamps and id).
func (p *pgCheckStore) InsertCheck(ctx context.Context, c *models.CheckDefinition) error {
	if p.pool == nil {
		return errors.New("check_store: nil pool")
	}
	cfgJSON, err := json.Marshal(c.Config)
	if err != nil {
		return fmt.Errorf("check_store: marshal config: %w", err)
	}
	const q = `
		INSERT INTO check_definitions (
			id, org_id, name, description, check_type, config,
			interval_seconds, timeout_seconds, enabled,
			fail_threshold, warning_threshold, error_threshold,
			alert_severity, is_template, last_status,
			created_at, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2,''), ''), $3, $4, $5, $6,
			$7, $8, $9,
			$10, $11, $12,
			$13, $14, $15,
			COALESCE($16, NOW()), COALESCE($16, NOW())
		)
		RETURNING created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q,
		c.ID, c.OrgID, c.Name, c.Description, c.CheckType, cfgJSON,
		c.IntervalSeconds, c.TimeoutSeconds, c.Enabled,
		c.FailThreshold, c.WarnThreshold, c.ErrorThreshold,
		c.AlertSeverity, c.IsTemplate, c.LastStatus,
		c.CreatedAt,
	)
	if err := row.Scan(&c.CreatedAt, &c.UpdatedAt); err != nil {
		return fmt.Errorf("check_store: insert: %w", err)
	}
	return nil
}

// GetCheck returns one check definition by id, scoped to the given org.
// If orgID is non-empty, the query enforces org ownership; otherwise the
// caller is responsible for org verification.
func (p *pgCheckStore) GetCheck(ctx context.Context, orgID, id string) (*models.CheckDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("check_store: nil pool")
	}
	args := []any{id}
	where := []string{"id = $1"}
	if orgID != "" {
		args = append(args, orgID)
		where = append(where, fmt.Sprintf("org_id = $%d", len(args)))
	}
	q := `
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       check_type, config,
		       COALESCE(interval_seconds, 60), COALESCE(timeout_seconds, 30),
		       COALESCE(enabled, true),
		       COALESCE(fail_threshold, 0), COALESCE(warning_threshold, 0), COALESCE(error_threshold, 0),
		       COALESCE(alert_severity, ''), COALESCE(is_template, false), COALESCE(last_status, ''),
		       created_at, updated_at
		FROM check_definitions
		WHERE ` + joinAnd(where) + `
		LIMIT 1
	`
	c := &models.CheckDefinition{}
	var cfgRaw []byte
	err := p.pool.QueryRow(ctx, q, args...).Scan(
		&c.ID, &c.OrgID, &c.Name, &c.Description, &c.CheckType, &cfgRaw,
		&c.IntervalSeconds, &c.TimeoutSeconds, &c.Enabled,
		&c.FailThreshold, &c.WarnThreshold, &c.ErrorThreshold,
		&c.AlertSeverity, &c.IsTemplate, &c.LastStatus,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCheckNotFound
		}
		return nil, fmt.Errorf("check_store: get: %w", err)
	}
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &c.Config); err != nil {
			return nil, fmt.Errorf("check_store: unmarshal config: %w", err)
		}
	}
	if c.Config == nil {
		c.Config = map[string]any{}
	}
	return c, nil
}

// ListChecks returns a filtered, paginated slice plus the total count.
func (p *pgCheckStore) ListChecks(ctx context.Context, f CheckListFilter) ([]models.CheckDefinition, int, error) {
	if p.pool == nil {
		return nil, 0, errors.New("check_store: nil pool")
	}
	args := make([]any, 0, 5)
	where := make([]string, 0, 3)
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.OrgID != "" {
		add("org_id = $%d", f.OrgID)
	}
	if f.CheckType != "" {
		add("check_type = $%d", f.CheckType)
	}
	if f.Enabled != nil {
		add("enabled = $%d", *f.Enabled)
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d)", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + joinAnd(where)
	}

	var total int
	if err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM check_definitions "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("check_store: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       check_type, config,
		       COALESCE(interval_seconds, 60), COALESCE(timeout_seconds, 30),
		       COALESCE(enabled, true),
		       COALESCE(fail_threshold, 0), COALESCE(warning_threshold, 0), COALESCE(error_threshold, 0),
		       COALESCE(alert_severity, ''), COALESCE(is_template, false), COALESCE(last_status, ''),
		       created_at, updated_at
		FROM check_definitions
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("check_store: list: %w", err)
	}
	defer rows.Close()

	out := make([]models.CheckDefinition, 0, f.Limit)
	for rows.Next() {
		var c models.CheckDefinition
		var cfgRaw []byte
		if err := rows.Scan(
			&c.ID, &c.OrgID, &c.Name, &c.Description, &c.CheckType, &cfgRaw,
			&c.IntervalSeconds, &c.TimeoutSeconds, &c.Enabled,
			&c.FailThreshold, &c.WarnThreshold, &c.ErrorThreshold,
			&c.AlertSeverity, &c.IsTemplate, &c.LastStatus,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("check_store: scan: %w", err)
		}
		if len(cfgRaw) > 0 {
			if err := json.Unmarshal(cfgRaw, &c.Config); err != nil {
				return nil, 0, fmt.Errorf("check_store: unmarshal config: %w", err)
			}
		}
		if c.Config == nil {
			c.Config = map[string]any{}
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("check_store: rows err: %w", err)
	}
	return out, total, nil
}

// UpdateCheck applies a partial update to a check definition. Only the
// non-nil fields in the patch are persisted. The updated_at column is
// always bumped to NOW().
type CheckPatch struct {
	Name            *string
	Description     *string
	Config          map[string]any
	IntervalSeconds *int
	TimeoutSeconds  *int
	Enabled         *bool
	FailThreshold   *float64
	WarnThreshold   *float64
	ErrorThreshold  *float64
	AlertSeverity   *string
	IsTemplate      *bool
	LastStatus      *string
}

// DeleteCheck hard-deletes a check definition row, scoped to the given org.
// If orgID is non-empty, only rows owned by that org are deleted.
// Caller is responsible for checking assignment count first.
func (p *pgCheckStore) DeleteCheck(ctx context.Context, orgID, id string) error {
	if p.pool == nil {
		return errors.New("check_store: nil pool")
	}
	args := []any{id}
	where := "id = $1"
	if orgID != "" {
		args = append(args, orgID)
		where += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	q := "DELETE FROM check_definitions WHERE " + where
	_, err := p.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("check_store: delete: %w", err)
	}
	return nil
}
