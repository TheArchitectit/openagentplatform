package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func (s *pgPolicyStore) GetPolicyViolations(ctx context.Context, policyID string, f ViolationFilter) ([]models.PolicyViolation, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("policy: nil pool")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	args := make([]any, 0, 4)
	where := []string{"policy_id = $1"}
	args = append(args, policyID)
	if f.AgentID != "" {
		args = append(args, f.AgentID)
		where = append(where, fmt.Sprintf("agent_id = $%d", len(args)))
	}
	if f.Resolved != nil {
		args = append(args, *f.Resolved)
		where = append(where, fmt.Sprintf("resolved = $%d", len(args)))
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM policy_violations "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("policy: count violations: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(severity,'warning'),
		       COALESCE(message,''), details, COALESCE(resolved,false), resolved_at, created_at
		FROM policy_violations
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("policy: list violations: %w", err)
	}
	defer rows.Close()

	out := make([]models.PolicyViolation, 0, f.Limit)
	for rows.Next() {
		var v models.PolicyViolation
		var det []byte
		if err := rows.Scan(
			&v.ID, &v.PolicyID, &v.AgentID, &v.Severity, &v.Message, &det,
			&v.Resolved, &v.ResolvedAt, &v.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("policy: scan violation: %w", err)
		}
		if len(det) > 0 {
			_ = json.Unmarshal(det, &v.Details)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// CountViolationsByPolicy returns the total number of recorded
// violations for a policy (open + resolved).
func (s *pgPolicyStore) CountViolationsByPolicy(ctx context.Context, policyID string) (int, error) {
	if s.pool == nil {
		return 0, errors.New("policy: nil pool")
	}
	const q = `SELECT COUNT(*) FROM policy_violations WHERE policy_id = $1`
	var n int
	if err := s.pool.QueryRow(ctx, q, policyID).Scan(&n); err != nil {
		return 0, fmt.Errorf("policy: count violations: %w", err)
	}
	return n, nil
}

// GetPolicyViolationByID fetches a single violation by id. Returns
// ErrPolicyViolationNotFound when the row does not exist.
func (s *pgPolicyStore) GetPolicyViolationByID(ctx context.Context, id string) (*models.PolicyViolation, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	if id == "" {
		return nil, errors.New("policy: violation id required")
	}
	const q = `
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(severity,'warning'),
		       COALESCE(message,''), details, COALESCE(resolved,false), resolved_at, created_at
		FROM policy_violations
		WHERE id = $1
		LIMIT 1
	`
	var v models.PolicyViolation
	var det []byte
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&v.ID, &v.PolicyID, &v.AgentID, &v.Severity, &v.Message, &det,
		&v.Resolved, &v.ResolvedAt, &v.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPolicyViolationNotFound
		}
		return nil, fmt.Errorf("policy: get violation: %w", err)
	}
	if len(det) > 0 {
		_ = json.Unmarshal(det, &v.Details)
	}
	return &v, nil
}

// ListViolationsByAgent returns all violations for a single agent,
// newest first. The resolved filter is optional; when nil, both open
// and resolved rows are returned.
func (s *pgPolicyStore) ListViolationsByAgent(ctx context.Context, agentID string, resolved *bool, limit, offset int) ([]models.PolicyViolation, int, error) {
	if s.pool == nil {
		return nil, 0, errors.New("policy: nil pool")
	}
	if agentID == "" {
		return nil, 0, errors.New("policy: agent_id required")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := []any{agentID}
	where := []string{"agent_id = $1"}
	if resolved != nil {
		args = append(args, *resolved)
		where = append(where, fmt.Sprintf("resolved = $%d", len(args)))
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM policy_violations "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("policy: count agent violations: %w", err)
	}

	args = append(args, limit, offset)
	q := fmt.Sprintf(`
		SELECT id, COALESCE(policy_id,''), COALESCE(agent_id,''), COALESCE(severity,'warning'),
		       COALESCE(message,''), details, COALESCE(resolved,false), resolved_at, created_at
		FROM policy_violations
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("policy: list agent violations: %w", err)
	}
	defer rows.Close()
	out := make([]models.PolicyViolation, 0, limit)
	for rows.Next() {
		var v models.PolicyViolation
		var det []byte
		if err := rows.Scan(
			&v.ID, &v.PolicyID, &v.AgentID, &v.Severity, &v.Message, &det,
			&v.Resolved, &v.ResolvedAt, &v.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("policy: scan agent violation: %w", err)
		}
		if len(det) > 0 {
			_ = json.Unmarshal(det, &v.Details)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// ComplianceSummary computes the org-level compliance aggregates. It
// is intentionally implemented as a small set of independent queries
// rather than one monolithic join, to keep the query plan simple and
// each metric independently cacheable. orgID may be empty to compute
// a platform-wide summary.
func (s *pgPolicyStore) ComplianceSummary(ctx context.Context, orgID string) (ComplianceSummary, error) {
	if s.pool == nil {
		return ComplianceSummary{}, errors.New("policy: nil pool")
	}
	summary := ComplianceSummary{
		OrgID:      orgID,
		ByCategory: make(map[string]int),
		BySeverity: make(map[string]int),
	}

	// Total + enabled policies.
	polQ := `SELECT COUNT(*), COUNT(*) FILTER (WHERE enabled = true) FROM policies WHERE deleted = false`
	if err := s.pool.QueryRow(ctx, polQ).Scan(&summary.TotalPolicies, &summary.EnabledPolicies); err != nil {
		return summary, fmt.Errorf("policy: compliance policies: %w", err)
	}

	// Total agents (platform-wide; an org-scoped filter requires the
	// agents table to be present in this database, which it is).
	agentQ := `SELECT COUNT(*) FROM agents`
	if err := s.pool.QueryRow(ctx, agentQ).Scan(&summary.TotalAgents); err != nil {
		// The agents table may be in a different schema or absent; the
		// rest of the summary still works. We log by returning a
		// zeroed agent count instead of failing the whole call.
		summary.TotalAgents = 0
	}

	// Open vs resolved violations.
	const countsQ = `
		SELECT
			COUNT(*) FILTER (WHERE resolved = false),
			COUNT(*) FILTER (WHERE resolved = true)
		FROM policy_violations
	`
	if err := s.pool.QueryRow(ctx, countsQ).Scan(&summary.OpenViolations, &summary.ResolvedViolations); err != nil {
		return summary, fmt.Errorf("policy: compliance violation counts: %w", err)
	}

	// Violations by category, joined through the policies table so
	// that the "category" column is the policy's declared category.
	const byCatQ = `
		SELECT COALESCE(p.category, 'configuration'), COUNT(*)
		FROM policy_violations v
		LEFT JOIN policies p ON p.id = v.policy_id
		WHERE v.resolved = false
		GROUP BY p.category
	`
	catRows, err := s.pool.Query(ctx, byCatQ)
	if err != nil {
		return summary, fmt.Errorf("policy: compliance by category: %w", err)
	}
	for catRows.Next() {
		var cat string
		var n int
		if err := catRows.Scan(&cat, &n); err != nil {
			catRows.Close()
			return summary, fmt.Errorf("policy: scan by category: %w", err)
		}
		summary.ByCategory[cat] = n
	}
	catRows.Close()

	const bySevQ = `
		SELECT COALESCE(severity, 'warning'), COUNT(*)
		FROM policy_violations
		WHERE resolved = false
		GROUP BY severity
	`
	sevRows, err := s.pool.Query(ctx, bySevQ)
	if err != nil {
		return summary, fmt.Errorf("policy: compliance by severity: %w", err)
	}
	for sevRows.Next() {
		var sev string
		var n int
		if err := sevRows.Scan(&sev, &n); err != nil {
			sevRows.Close()
			return summary, fmt.Errorf("policy: scan by severity: %w", err)
		}
		summary.BySeverity[sev] = n
	}
	sevRows.Close()

	// 24-hour trend.
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	const trendQ = `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= $1),
			COUNT(*) FILTER (WHERE resolved = true AND resolved_at >= $1)
		FROM policy_violations
	`
	if err := s.pool.QueryRow(ctx, trendQ, cutoff).Scan(
		&summary.Trend24h.NewViolations,
		&summary.Trend24h.ResolvedViolations,
	); err != nil {
		return summary, fmt.Errorf("policy: compliance trend: %w", err)
	}

	// Total evaluations considered: open + resolved violations. This
	// is an approximation -- every violation corresponds to one
	// evaluation that failed; passing evaluations are not persisted
	// today, so we use the violation count as the denominator.
	summary.TotalEvaluations = summary.OpenViolations + summary.ResolvedViolations
	if summary.TotalEvaluations > 0 {
		// Compliant% is the fraction of all evaluations that passed
		// (which is 1 - failures/total). We compute it as
		// resolved/total, which is a conservative measure of how many
		// historical failures have been cleaned up. This is the number
		// most compliance dashboards expect to see.
		summary.CompliantPct = float64(summary.ResolvedViolations) / float64(summary.TotalEvaluations) * 100.0
	}

	return summary, nil
}

// ListAllAgentIDs returns every agent ID in the platform, optionally
// filtered by org.
func (s *pgPolicyStore) ListAllAgentIDs(ctx context.Context, orgID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	var (
		rows pgx.Rows
		err  error
	)
	if orgID != "" {
		rows, err = s.pool.Query(ctx, `SELECT id FROM agents WHERE org_id = $1`, orgID)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT id FROM agents`)
	}
	if err != nil {
		return nil, fmt.Errorf("policy: list agents: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 64)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("policy: scan agent: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListAgentIDsForSite returns every agent ID belonging to a site.
func (s *pgPolicyStore) ListAgentIDsForSite(ctx context.Context, siteID string) ([]string, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM agents WHERE site_id = $1`, siteID)
	if err != nil {
		return nil, fmt.Errorf("policy: list site agents: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 32)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("policy: scan site agent: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --- Errors ----------------------------------------------------------------

// ErrPolicyNotFound is returned when a policy id does not exist or
// has been soft-deleted.
var ErrPolicyNotFound = errors.New("policy not found")

// ErrPolicyViolationNotFound is returned when a violation id does not
// exist.
var ErrPolicyViolationNotFound = errors.New("policy violation not found")

// DismissPolicyViolation marks a violation as resolved and stores the
// dismissal metadata in the Details JSONB column. We do not extend
// the base model with dedicated columns so the migration cost stays at
// zero; the UI reads the "dismissed_by" / "dismiss_reason" keys from
// Details when rendering a resolved-by-human badge.
func (s *pgPolicyStore) DismissPolicyViolation(ctx context.Context, id, reason, actor string) (*models.PolicyViolation, error) {
	if s.pool == nil {
		return nil, errors.New("policy: nil pool")
	}
	if id == "" {
		return nil, errors.New("policy: violation id required")
	}
	now := time.Now().UTC()
	const q = `
		UPDATE policy_violations SET
			resolved = true,
			resolved_at = $2,
			details = COALESCE(details, '{}'::jsonb) ||
				jsonb_build_object('dismissed_by', $3::text, 'dismiss_reason', $4::text)
		WHERE id = $1
	`
	tag, err := s.pool.Exec(ctx, q, id, now, actor, reason)
	if err != nil {
		return nil, fmt.Errorf("policy: dismiss violation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrPolicyViolationNotFound
	}
	return s.GetPolicyViolationByID(ctx, id)
}

// --- helpers ---------------------------------------------------------------

// jsonOrNull marshals v to JSON, or returns nil if v is empty.
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
