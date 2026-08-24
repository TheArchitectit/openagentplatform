// Package reports - aggregator_pg.go implements the PostgreSQL-backed
// DataAggregator. Each method runs read-only aggregate queries against
// the live platform tables (agents, check_results, alerts, patch_jobs,
// audit_events), always scoped by org_id.
//
// The per-domain aggregate methods live in aggregator_inventory.go,
// aggregator_compliance.go, aggregator_alerts.go, aggregator_audit.go,
// and aggregator_usage.go.
package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGAggregator is the production DataAggregator.
type PGAggregator struct {
	pool *pgxpool.Pool
}

// NewPGAggregator constructs a PGAggregator.
func NewPGAggregator(pool *pgxpool.Pool) *PGAggregator {
	return &PGAggregator{pool: pool}
}

// AggregateExecutiveSummary rolls up inventory, alerts, checks, and
// patches into one high-level view.
func (a *PGAggregator) AggregateExecutiveSummary(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	inventory, err := a.AggregateAgentInventory(ctx, orgID, params)
	if err != nil {
		return nil, err
	}
	alertsJSON, err := a.AggregateAlertSummary(ctx, orgID, params)
	if err != nil {
		return nil, err
	}
	checksJSON, err := a.AggregateCheckCompliance(ctx, orgID, params)
	if err != nil {
		return nil, err
	}
	patchesJSON, err := a.AggregatePatchCompliance(ctx, orgID, params)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"org_id":           orgID,
		"generated_at":     timeNowUTC(),
		"inventory":        json.RawMessage(inventory),
		"alerts":           json.RawMessage(alertsJSON),
		"check_compliance": json.RawMessage(checksJSON),
		"patch_compliance": json.RawMessage(patchesJSON),
	}
	return json.Marshal(payload)
}

func (a *PGAggregator) ok() error {
	if a == nil || a.pool == nil {
		return fmt.Errorf("reports: aggregator not initialised")
	}
	return nil
}

// timeNowUTC is the RFC3339 timestamp used in generated payloads.
func timeNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// windowClause maps params["window"] to a PostgreSQL interval literal.
// Only fixed suffixes are accepted so the value is safe to interpolate;
// anything else falls back to the default.
func windowClause(params map[string]string) string {
	switch params["window"] {
	case "1h":
		return "INTERVAL '1 hour'"
	case "24h":
		return "INTERVAL '24 hours'"
	case "7d":
		return "INTERVAL '7 days'"
	case "30d":
		return "INTERVAL '30 days'"
	default:
		return "INTERVAL '24 hours'"
	}
}

// windowValue returns the interval as a parameter value (for queries
// that bind it instead of interpolating).
func windowValue(params map[string]string) string {
	switch params["window"] {
	case "1h":
		return "1 hour"
	case "24h":
		return "24 hours"
	case "7d":
		return "7 days"
	case "30d":
		return "30 days"
	default:
		return "24 hours"
	}
}
