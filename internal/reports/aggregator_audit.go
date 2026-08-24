package reports

import (
	"context"
	"encoding/json"
	"fmt"
)

// AggregateAuditTrail returns audit events for the org within the
// trailing window (default 30d), newest first, capped at 1000 rows.
func (a *PGAggregator) AggregateAuditTrail(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	const q = `
		SELECT event_id, timestamp, actor_type, actor_id, action,
		       resource_type, resource_id, outcome
		FROM audit_events
		WHERE org_id = $1 AND timestamp >= NOW() - INTERVAL '30 days'
		ORDER BY timestamp DESC
		LIMIT 1000`
	rows, err := a.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("aggregate audit trail: %w", err)
	}
	defer rows.Close()
	type evRow struct {
		EventID      string `json:"event_id"`
		Timestamp    string `json:"timestamp"`
		ActorType    string `json:"actor_type"`
		ActorID      string `json:"actor_id,omitempty"`
		Action       string `json:"action"`
		ResourceType string `json:"resource_type,omitempty"`
		ResourceID   string `json:"resource_id,omitempty"`
		Outcome      string `json:"outcome,omitempty"`
	}
	events := []evRow{}
	for rows.Next() {
		var e evRow
		var actorID, resType, resID, outcome *string
		if err := rows.Scan(&e.EventID, &e.Timestamp, &e.ActorType, &actorID,
			&e.Action, &resType, &resID, &outcome); err != nil {
			return nil, fmt.Errorf("aggregate audit trail scan: %w", err)
		}
		if actorID != nil {
			e.ActorID = *actorID
		}
		if resType != nil {
			e.ResourceType = *resType
		}
		if resID != nil {
			e.ResourceID = *resID
		}
		if outcome != nil {
			e.Outcome = *outcome
		}
		events = append(events, e)
	}
	payload := map[string]any{"org_id": orgID, "event_count": len(events), "events": events}
	return json.Marshal(payload)
}
