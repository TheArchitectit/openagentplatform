// Package reports - aggregator_pg.go implements the PostgreSQL-backed
// DataAggregator. Each method runs read-only aggregate queries against
// the live platform tables (agents, check_results, alerts, patch_jobs,
// audit_events), always scoped by org_id.
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

// AggregateCheckCompliance returns check pass/fail summaries over the
// trailing window (default 24h, override with params["window"]).
func (a *PGAggregator) AggregateCheckCompliance(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	window := windowClause(params)

	// check_results carries agent_id but not org_id; join through agents.
	base := `
		SELECT c.check_id,
		       COUNT(*)                                            AS total_runs,
		       COUNT(*) FILTER (WHERE c.status IN ('ok','OK','pass','passed')) AS passed,
		       COUNT(*) FILTER (WHERE c.status NOT IN ('ok','OK','pass','passed')) AS failed,
		       MAX(c.timestamp)                                    AS last_run
		FROM check_results c
		JOIN agents ag ON ag.id = c.agent_id
		WHERE ag.org_id = $1 AND c.timestamp >= NOW() - ` + window + `
		GROUP BY c.check_id
		ORDER BY c.check_id`
	rows, err := a.pool.Query(ctx, base, orgID)
	if err != nil {
		return nil, fmt.Errorf("aggregate check compliance: %w", err)
	}
	defer rows.Close()
	type checkRow struct {
		CheckID  string  `json:"check_id"`
		Total    int64   `json:"total_runs"`
		Passed   int64   `json:"passed"`
		Failed   int64   `json:"failed"`
		LastRun  *string `json:"last_run"`
		PassRate float64 `json:"pass_rate"`
	}
	out := []checkRow{}
	for rows.Next() {
		var r checkRow
		if err := rows.Scan(&r.CheckID, &r.Total, &r.Passed, &r.Failed, &r.LastRun); err != nil {
			return nil, fmt.Errorf("aggregate check compliance scan: %w", err)
		}
		if r.Total > 0 {
			r.PassRate = float64(r.Passed) / float64(r.Total)
		}
		out = append(out, r)
	}
	payload := map[string]any{"org_id": orgID, "checks": out}
	return json.Marshal(payload)
}

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

// AggregatePatchCompliance returns patch-job state counts and target
// success rates for the org.
func (a *PGAggregator) AggregatePatchCompliance(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error) {
	if err := a.ok(); err != nil {
		return nil, err
	}
	stateQ := `
		SELECT state, COUNT(*)
		FROM patch_jobs
		WHERE org_id = $1
		GROUP BY state ORDER BY state`
	rows, err := a.pool.Query(ctx, stateQ, orgID)
	if err != nil {
		return nil, fmt.Errorf("aggregate patch compliance: %w", err)
	}
	states := map[string]int64{}
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("aggregate patch compliance scan: %w", err)
		}
		states[state] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	targetQ := `
		SELECT t.state, COUNT(*)
		FROM patch_job_targets t
		JOIN patch_jobs j ON j.id = t.job_id
		WHERE j.org_id = $1
		GROUP BY t.state ORDER BY t.state`
	trows, err := a.pool.Query(ctx, targetQ, orgID)
	if err != nil {
		return nil, fmt.Errorf("aggregate patch targets: %w", err)
	}
	defer trows.Close()
	targetStates := map[string]int64{}
	for trows.Next() {
		var state string
		var n int64
		if err := trows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("aggregate patch targets scan: %w", err)
		}
		targetStates[state] = n
	}
	payload := map[string]any{
		"org_id":        orgID,
		"jobs_by_state": states,
		"targets_by_state": targetStates,
	}
	return json.Marshal(payload)
}

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
		EventID      string  `json:"event_id"`
		Timestamp    string  `json:"timestamp"`
		ActorType    string  `json:"actor_type"`
		ActorID      string  `json:"actor_id,omitempty"`
		Action       string  `json:"action"`
		ResourceType string  `json:"resource_type,omitempty"`
		ResourceID   string  `json:"resource_id,omitempty"`
		Outcome      string  `json:"outcome,omitempty"`
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
		"org_id":               orgID,
		"report_runs_30d":      total,
		"runs_completed":       completed,
		"runs_failed":          failed,
		"total_generation_ms":  totalMs,
		"api_call_counts":      "not collected",
	}
	return json.Marshal(payload)
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
