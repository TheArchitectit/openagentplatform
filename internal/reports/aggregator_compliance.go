package reports

import (
	"context"
	"encoding/json"
	"fmt"
)

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
		"org_id":           orgID,
		"jobs_by_state":    states,
		"targets_by_state": targetStates,
	}
	return json.Marshal(payload)
}
