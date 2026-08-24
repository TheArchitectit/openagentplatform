package reports

import (
	"context"
	"encoding/json"
	"fmt"
)

// AggregateAlertSummary returns alert counts by severity and state over
// the trailing window (default 7d, override with params["window"]).
func (a *PGAggregator) AggregateAlertSummary(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	window := windowValue(params)
	const q = `
		SELECT severity, state, COUNT(*)
		FROM alerts
		WHERE org_id = $1 AND created_at >= NOW() - $2::interval
		GROUP BY severity, state
		ORDER BY severity, state`
	rows, err := a.pool.Query(ctx, q, orgID, window)
	if err != nil {
		return nil, fmt.Errorf("aggregate alert summary: %w", err)
	}
	defer rows.Close()
	type sevState struct {
		Severity string `json:"severity"`
		State    string `json:"state"`
		Count    int64  `json:"count"`
	}
	breakdown := []sevState{}
	total := int64(0)
	for rows.Next() {
		var r sevState
		if err := rows.Scan(&r.Severity, &r.State, &r.Count); err != nil {
			return nil, fmt.Errorf("aggregate alert summary scan: %w", err)
		}
		total += r.Count
		breakdown = append(breakdown, r)
	}
	payload := map[string]any{
		"org_id":    orgID,
		"total":     total,
		"breakdown": breakdown,
	}
	return json.Marshal(payload)
}
