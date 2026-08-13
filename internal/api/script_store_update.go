package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func (p *pgScriptStore) UpdateScript(ctx context.Context, orgID, id string, patch ScriptPatch) (*models.ScriptDefinition, error) {
	if p.pool == nil {
		return nil, errors.New("script_store: nil pool")
	}
	args := make([]any, 0, 6)
	sets := make([]string, 0, 6)
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
	if patch.Body != nil {
		add("body", *patch.Body)
	}
	if patch.Runtime != nil {
		add("runtime", *patch.Runtime)
	}
	if patch.TimeoutSeconds != nil {
		add("timeout_seconds", *patch.TimeoutSeconds)
	}
	if patch.Enabled != nil {
		add("enabled", *patch.Enabled)
	}
	if patch.Tags != nil {
		tagsJSON, err := json.Marshal(patch.Tags)
		if err != nil {
			return nil, fmt.Errorf("script_store: marshal tags: %w", err)
		}
		add("tags", tagsJSON)
	}
	if len(sets) == 0 {
		// Nothing to update — return the current row.
		return p.GetScript(ctx, orgID, id)
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	whereClause := "id = $%d AND deleted_at IS NULL"
	if orgID != "" {
		args = append(args, orgID)
		whereClause += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	q := fmt.Sprintf(`
		UPDATE script_definitions SET %s
		WHERE %s
		RETURNING id
	`, joinAnd(sets), whereClause)

	var newID string
	if err := p.pool.QueryRow(ctx, q, args...).Scan(&newID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScriptNotFound
		}
		return nil, fmt.Errorf("script_store: update: %w", err)
	}
	return p.GetScript(ctx, orgID, id)
}
