package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/openagentplatform/openagentplatform/secrets"
	"github.com/openagentplatform/openagentplatform/secrets/infisical"
	"github.com/openagentplatform/openagentplatform/secrets/vault"
)

func newAuditService(pool *pgxpool.Pool) *audit.AuditService {
	return audit.New(pool)
}

// buildSecretBackends inspects environment variables and registers the
// appropriate secret backends. Returns the populated registry and the
// list of backend names that were actually registered.
func buildSecretBackends(log *slog.Logger) (*secrets.BackendRegistry, []string) {
	registry := secrets.NewBackendRegistry()
	var names []string

	// Vault takes precedence when VAULT_ADDR is set.
	if addr := os.Getenv("VAULT_ADDR"); addr != "" {
		token := os.Getenv("VAULT_TOKEN")
		v, err := vault.New(context.Background(), vault.Config{
			Address:    addr,
			AuthMethod: vault.AuthToken,
			Token:      token,
		})
		if err != nil {
			log.Warn("vault backend init failed; skipping", "err", err)
		} else {
			registry.Register("vault", v)
			names = append(names, "vault")
			log.Info("secrets: registered vault backend", "addr", addr)
		}
	}

	// Infisical is registered when INFISICAL_CLIENT_ID is set.
	if clientID := os.Getenv("INFISICAL_CLIENT_ID"); clientID != "" {
		clientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")
		i, err := infisical.New(context.Background(), infisical.Config{
			SiteURL:      getEnvDefault("INFISICAL_SITE_URL", "https://app.infisical.com"),
			ProjectID:    os.Getenv("INFISICAL_PROJECT_ID"),
			Environment:  getEnvDefault("INFISICAL_ENVIRONMENT", "dev"),
			AuthMethod:   infisical.AuthUniversal,
			ClientID:     clientID,
			ClientSecret: clientSecret,
		})
		if err != nil {
			log.Warn("infisical backend init failed; skipping", "err", err)
		} else {
			registry.Register("infisical", i)
			names = append(names, "infisical")
			log.Info("secrets: registered infisical backend")
		}
	}

	// Kubernetes CSI driver when running inside a cluster.
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		ns := getEnvDefault("OAP_K8S_NAMESPACE", "default")
		k := secrets.NewK8sCSIBackend(secrets.K8sCSIConfig{
			Namespace: ns,
			MountPath: getEnvDefault("OAP_K8S_MOUNT_PATH", "/var/secrets/oap"),
		})
		registry.Register("k8s-csi", k)
		names = append(names, "k8s-csi")
		log.Info("secrets: registered k8s-csi backend", "namespace", ns)
	}

	// Default: env-var backend (development / fallback).
	env := secrets.NewEnvBackend("OAP_SECRET_")
	registry.Register("env", env)
	names = append(names, "env")
	log.Info("secrets: registered env backend (default)")

	return registry, names
}

// getEnvDefault returns the value of the environment variable named by
// key, or def if it is empty.
func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// --- Shared adapter types (used by both server.go and main.go) ---

// eventStoreAdapter bridges the events package's narrow HeartbeatStore /
// CheckStore interfaces to the api package's pgAgentStore. We re-use the
// api package's store directly through this thin wrapper to avoid
// duplicating the SQL.
type eventStoreAdapter struct {
	pool *pgxpool.Pool
}

func newAgentStoreAdapter(pool *pgxpool.Pool) *eventStoreAdapter {
	return &eventStoreAdapter{pool: pool}
}

// The methods below intentionally duplicate a small subset of the SQL
// expressed in internal/api/agent_store.go so the events package can stay
// dependency-free. Keep them in sync.
func (a *eventStoreAdapter) UpdateAgentHeartbeat(ctx context.Context, agentID string, status string, lastSeen any, cpu, mem, disk float64) error {
	if a.pool == nil {
		return nil
	}
	const q = `
		UPDATE agents
		SET status = $2,
		    last_seen = $3,
		    last_cpu_percent = $4,
		    last_mem_percent = $5,
		    last_disk_percent = $6,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := a.pool.Exec(ctx, q, agentID, status, lastSeen, cpu, mem, disk)
	return err
}

func (a *eventStoreAdapter) GetAgent(ctx context.Context, _, id string) (*models.Agent, error) {
	if a.pool == nil {
		return nil, nil
	}
	const q = `SELECT id, COALESCE(status, 'offline') FROM agents WHERE id = $1 LIMIT 1`
	ag := &models.Agent{}
	err := a.pool.QueryRow(ctx, q, id).Scan(&ag.ID, &ag.Status)
	return ag, err
}

func (a *eventStoreAdapter) ListSilentAgents(ctx context.Context, orgID, statusFilter string, staleBefore time.Time) ([]models.Agent, error) {
	if a.pool == nil {
		return nil, nil
	}
	args := []any{staleBefore}
	where := []string{"last_seen < $1"}
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if orgID != "" {
		add("org_id = $%d", orgID)
	}
	if statusFilter != "" {
		add("status = $%d", statusFilter)
	}
	q := `
		SELECT id, site_id, COALESCE(org_id,''), hostname, COALESCE(os,''), COALESCE(arch,''),
		       COALESCE(platform,''), COALESCE(cpu_count,0), COALESCE(total_memory_mb,0),
		       COALESCE(total_disk_gb,0), COALESCE(agent_version,''), COALESCE(status,'offline'),
		       COALESCE(last_seen, 'epoch'::timestamptz), tags, created_at, updated_at
		FROM agents
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY last_seen ASC
	`
	rows, err := a.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("agent_store: list silent agents: %w", err)
	}
	defer rows.Close()
	out := make([]models.Agent, 0, 16)
	for rows.Next() {
		var ag models.Agent
		if err := rows.Scan(
			&ag.ID, &ag.SiteID, &ag.OrgID, &ag.Hostname, &ag.OperatingSystem, &ag.Arch, &ag.Platform,
			&ag.CPUCount, &ag.TotalMemoryMB, &ag.TotalDiskGB, &ag.AgentVersion, &ag.Status,
			&ag.LastSeen, &ag.Tags, &ag.CreatedAt, &ag.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("agent_store: scan silent agent: %w", err)
		}
		out = append(out, ag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agent_store: rows err: %w", err)
	}
	return out, nil
}

func (a *eventStoreAdapter) MarkStaleAgentsOffline(ctx context.Context, threshold any) ([]string, error) {
	if a.pool == nil {
		return nil, nil
	}
	const q = `
		UPDATE agents
		SET status = 'offline', updated_at = NOW()
		WHERE status = 'online' AND last_seen < $1
		RETURNING id
	`
	rows, err := a.pool.Query(ctx, q, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (a *eventStoreAdapter) InsertCheckResult(ctx context.Context, r *models.CheckResult) error {
	if a.pool == nil {
		return nil
	}
	const q = `
		INSERT INTO check_results (agent_id, check_id, timestamp, status, value, message, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := a.pool.Exec(ctx, q, r.AgentID, r.CheckID, r.Timestamp, r.Status, r.Value, r.Message, r.Metadata)
	return err
}

// ListRecentResults returns the most recent N check results for the
// given (agent_id, check_id) pair, ordered from oldest to newest. It
// satisfies the checks.ResultStore interface used by the threshold
// evaluator.
func (a *eventStoreAdapter) ListRecentResults(ctx context.Context, agentID, checkID string, limit int) ([]models.CheckResult, error) {
	if a.pool == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	const q = `
		SELECT agent_id, check_id, COALESCE(timestamp, 'epoch'::timestamptz),
		       COALESCE(status,''), COALESCE(value, 0), COALESCE(message,''), metadata
		FROM check_results
		WHERE agent_id = $1 AND check_id = $2
		ORDER BY timestamp DESC
		LIMIT $3
	`
	rows, err := a.pool.Query(ctx, q, agentID, checkID, limit)
	if err != nil {
		return []models.CheckResult{}, nil
	}
	defer rows.Close()
	out := make([]models.CheckResult, 0, limit)
	for rows.Next() {
		var r models.CheckResult
		if err := rows.Scan(
			&r.AgentID, &r.CheckID, &r.Timestamp, &r.Status, &r.Value, &r.Message, &r.Metadata,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	// Reverse so callers see oldest -> newest.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// GetCheck fetches a check definition by id. It satisfies the
// checks.CheckDefinitionLookup interface used by the threshold
// evaluator to compute the flap-detection window.
func (a *eventStoreAdapter) GetCheck(ctx context.Context, id string) (*models.CheckDefinition, error) {
	if a.pool == nil {
		return nil, nil
	}
	const q = `
		SELECT id, COALESCE(org_id,''), name, COALESCE(description,''),
		       check_type, COALESCE(interval_seconds, 60),
		       COALESCE(timeout_seconds, 30), COALESCE(enabled, true)
		FROM check_definitions
		WHERE id = $1
		LIMIT 1
	`
	c := &models.CheckDefinition{}
	err := a.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.OrgID, &c.Name, &c.Description, &c.CheckType,
		&c.IntervalSeconds, &c.TimeoutSeconds, &c.Enabled,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// policyResolver is a thin adapter that backs the oap.* OPA builtins
// from PostgreSQL. Each method looks up a small piece of agent state
// and returns it; errors are returned (not swallowed) so policies can
// use Rego's default rules to handle missing data gracefully.
type policyResolver struct {
	pool *pgxpool.Pool
}

func newPolicyResolver(pool *pgxpool.Pool, _ *eventStoreAdapter) *policyResolver {
	return &policyResolver{pool: pool}
}

func (r *policyResolver) AgentStatus(ctx context.Context, agentID string) (string, error) {
	if r.pool == nil {
		return "", nil
	}
	const q = `SELECT COALESCE(status, 'offline') FROM agents WHERE id = $1 LIMIT 1`
	var s string
	if err := r.pool.QueryRow(ctx, q, agentID).Scan(&s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "offline", nil
		}
		return "", err
	}
	return s, nil
}

func (r *policyResolver) AgentHasCheck(ctx context.Context, agentID, checkType string) (bool, error) {
	if r.pool == nil {
		return false, nil
	}
	const q = `
		SELECT 1
		FROM check_assignments ca
		JOIN check_definitions cd ON cd.id = ca.check_id
		WHERE ca.agent_id = $1 AND cd.check_type = $2
		LIMIT 1
	`
	var n int
	err := r.pool.QueryRow(ctx, q, agentID, checkType).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *policyResolver) CheckLastResult(ctx context.Context, agentID, checkID string) (map[string]any, error) {
	if r.pool == nil {
		return nil, nil
	}
	const q = `
		SELECT COALESCE(status, ''), value, COALESCE(message, ''), metadata
		FROM check_results
		WHERE agent_id = $1 AND check_id = $2
		ORDER BY timestamp DESC
		LIMIT 1
	`
	out := map[string]any{
		"agent_id": agentID,
		"check_id": checkID,
		"status":   "",
		"value":    0.0,
		"message":  "",
	}
	var status, message string
	var value float64
	var meta []byte
	err := r.pool.QueryRow(ctx, q, agentID, checkID).Scan(&status, &value, &message, &meta)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, err
	}
	out["status"] = status
	out["value"] = value
	out["message"] = message
	if len(meta) > 0 {
		var d map[string]any
		if json.Unmarshal(meta, &d) == nil {
			out["details"] = d
		}
	}
	return out, nil
}

func (r *policyResolver) AgentPatchLevel(ctx context.Context, agentID string) (string, error) {
	if r.pool == nil {
		return "", nil
	}
	const q = `SELECT COALESCE(metadata->>'patch_level', '') FROM agents WHERE id = $1 LIMIT 1`
	var s string
	if err := r.pool.QueryRow(ctx, q, agentID).Scan(&s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return s, nil
}

func (r *policyResolver) AgentOSVersion(ctx context.Context, agentID string) (string, error) {
	if r.pool == nil {
		return "", nil
	}
	const q = `SELECT COALESCE(os, '') || ' ' || COALESCE(platform, '') FROM agents WHERE id = $1 LIMIT 1`
	var s string
	if err := r.pool.QueryRow(ctx, q, agentID).Scan(&s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return s, nil
}
