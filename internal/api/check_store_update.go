package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func (p *pgCheckStore) UpdateCheck(ctx context.Context, orgID, id string, patch CheckPatch) (*models.CheckDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("check_store: nil pool")
	}
	args := make([]any, 0, 5)
	sets := make([]string, 0, 5)
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Description != nil {
		add("description", *patch.Description)
	}
	if patch.Config != nil {
		cfgJSON, err := json.Marshal(patch.Config)
		if err != nil {
			return nil, fmt.Errorf("check_store: marshal config: %w", err)
		}
		add("config", cfgJSON)
	}
	if patch.IntervalSeconds != nil {
		add("interval_seconds", *patch.IntervalSeconds)
	}
	if patch.TimeoutSeconds != nil {
		add("timeout_seconds", *patch.TimeoutSeconds)
	}
	if patch.Enabled != nil {
		add("enabled", *patch.Enabled)
	}
	if patch.FailThreshold != nil {
		add("fail_threshold", *patch.FailThreshold)
	}
	if patch.WarnThreshold != nil {
		add("warning_threshold", *patch.WarnThreshold)
	}
	if patch.ErrorThreshold != nil {
		add("error_threshold", *patch.ErrorThreshold)
	}
	if patch.AlertSeverity != nil {
		add("alert_severity", *patch.AlertSeverity)
	}
	if patch.IsTemplate != nil {
		add("is_template", *patch.IsTemplate)
	}
	if patch.LastStatus != nil {
		add("last_status", *patch.LastStatus)
	}
	if len(sets) == 0 {
		// Nothing to update — just return the current row.
		return p.GetCheck(ctx, orgID, id)
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	whereClause := "id = $%d"
	if orgID != "" {
		args = append(args, orgID)
		whereClause += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	q := fmt.Sprintf(`
		UPDATE check_definitions SET %s
		WHERE %s
		RETURNING id
	`, joinAnd(sets), whereClause)

	var newID string
	if err := p.pool.QueryRow(ctx, q, args...).Scan(&newID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCheckNotFound
		}
		return nil, fmt.Errorf("check_store: update: %w", err)
	}
	return p.GetCheck(ctx, orgID, id)
}
