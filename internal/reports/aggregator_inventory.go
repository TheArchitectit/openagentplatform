package reports

import (
	"context"
	"encoding/json"
	"fmt"
)

// AggregateAgentInventory returns per-agent summary rows for the org.
func (a *PGAggregator) AggregateAgentInventory(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	const q = `
		SELECT org_id,
		       COUNT(*)                                              AS total_agents,
		       COUNT(*) FILTER (WHERE status = 'online')             AS online,
		       COUNT(*) FILTER (WHERE status = 'offline')            AS offline,
		       COUNT(*) FILTER (WHERE status NOT IN ('online','offline')) AS other_status,
		       COALESCE(array_agg(DISTINCT os) FILTER (WHERE os IS NOT NULL AND os <> ''), '{}') AS operating_systems,
		       COALESCE(array_agg(DISTINCT agent_version) FILTER (WHERE agent_version IS NOT NULL AND agent_version <> ''), '{}') AS versions,
		       MAX(last_seen)                                        AS last_activity
		FROM agents
		WHERE org_id = $1
		GROUP BY org_id`
	row := a.pool.QueryRow(ctx, q, orgID)
	var inv struct {
		OrgID       string   `json:"org_id"`
		TotalAgents int      `json:"total_agents"`
		Online      int      `json:"online"`
		Offline     int      `json:"offline"`
		OtherStatus int      `json:"other_status"`
		OSList      []string `json:"operating_systems"`
		Versions    []string `json:"versions"`
		LastActive  *string  `json:"last_activity"`
	}
	if err := row.Scan(&inv.OrgID, &inv.TotalAgents, &inv.Online, &inv.Offline,
		&inv.OtherStatus, &inv.OSList, &inv.Versions, &inv.LastActive); err != nil {
		return nil, fmt.Errorf("aggregate agent inventory: %w", err)
	}
	return json.Marshal(inv)
}
