package reports

import (
	"context"
	"encoding/json"
	"fmt"
)

// AggregateUsageSummary returns run/billing-adjacent counters. There is
// no dedicated API-usage table yet, so this reports report-run activity
// only; the field names make that explicit rather than implying data we
// do not collect.
func (a *PGAggregator) AggregateUsageSummary(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	const q = `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status = 'completed'),
		       COUNT(*) FILTER (WHERE status = 'failed'),
		       COALESCE(SUM(duration_ms), 0)
		FROM report_runs
		WHERE org_id = $1 AND started_at >= NOW() - INTERVAL '30 days'`
	var total, completed, failed int64
	var totalMs int64
	if err := a.pool.QueryRow(ctx, q, orgID).Scan(&total, &completed, &failed, &totalMs); err != nil {
		return nil, fmt.Errorf("aggregate usage summary: %w", err)
	}
	payload := map[string]any{
		"org_id":              orgID,
		"report_runs_30d":     total,
		"runs_completed":      completed,
		"runs_failed":         failed,
		"total_generation_ms": totalMs,
		"api_call_counts":     "not collected",
	}
	return json.Marshal(payload)
}
