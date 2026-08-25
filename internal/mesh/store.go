// Package mesh implements the control-plane data layer for the RMM-09
// secure tunnel fabric. It owns the org-scoped persistence contract for
// WireGuard mesh peers, operator tunnel sessions, and Ed25519-signed
// agent releases. Every query filters by org_id. Tunnel/transport code
// lives in later sprint steps; this file is the schema contract.
package mesh

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MeshPeer is a single agent's WireGuard identity within an org.
type MeshPeer struct {
	AgentID    string
	OrgID      string
	PublicKey  string
	AllowedIPs string
	LastSeen   time.Time
	Status     string
}

// MeshSession is an operator-initiated tunnel session (audit trail).
type MeshSession struct {
	SessionID  string
	OperatorID string
	AgentID    string
	OrgID      string
	Purpose    string
	StartedAt  time.Time
	EndedAt    *time.Time
	Status     string
}

// AgentRelease is an Ed25519-signed agent binary for self-update.
type AgentRelease struct {
	ID          string
	OrgID       string
	Version     string
	Platform    string
	BinarySHA256 string
	Signature   string
	Pinned      bool
	CreatedAt   time.Time
}

// Store is the persistence contract for the mesh control plane. Every
// method is org-scoped: no query may return rows belonging to another
// org.
type Store interface {
	UpsertMeshPeer(ctx context.Context, orgID, agentID, publicKey, allowedIPs string) error
	GetMeshPeer(ctx context.Context, orgID, agentID string) (*MeshPeer, error)
	ListMeshPeers(ctx context.Context, orgID string) ([]*MeshPeer, error)
	UpdateMeshPeerLastSeen(ctx context.Context, orgID, agentID string, t time.Time) error

	InsertMeshSession(ctx context.Context, orgID, operatorID, agentID, sessionID, purpose string) error
	CloseMeshSession(ctx context.Context, orgID, sessionID string) error
	ListMeshSessions(ctx context.Context, orgID, operatorID string) ([]*MeshSession, error)

	InsertAgentRelease(ctx context.Context, orgID, version, platform, sha256, signature string, pinned bool) (*AgentRelease, error)
	GetAgentRelease(ctx context.Context, orgID, version string) (*AgentRelease, error)
	ListAgentReleases(ctx context.Context, orgID string, onlyPinned bool) ([]*AgentRelease, error)
	PinAgentRelease(ctx context.Context, orgID, version string, pinned bool) error
}

// meshPoolConn is the minimal pgx surface used by pgxStore. It is
// satisfied by *pgxpool.Pool in production and by pgxmock pools in tests.
type meshPoolConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// pgxStore is the production Store backed by a pgx connection pool.
type pgxStore struct {
	pool meshPoolConn
}

// NewStore builds a production store from a pgx pool.
func NewStore(pool meshPoolConn) Store {
	return &pgxStore{pool: pool}
}

// Errors returned by the store.
var (
	ErrMeshPeerNotFound   = errors.New("mesh: peer not found")
	ErrMeshSessionNotFound = errors.New("mesh: session not found")
	ErrReleaseNotFound    = errors.New("mesh: release not found")
	ErrNilPool            = errors.New("mesh: nil pool")
)

func (s *pgxStore) UpsertMeshPeer(ctx context.Context, orgID, agentID, publicKey, allowedIPs string) error {
	if s.pool == nil {
		return ErrNilPool
	}
	if orgID == "" || agentID == "" {
		return errors.New("mesh: org_id and agent_id are required")
	}
	const q = `
		INSERT INTO mesh_peers (agent_id, org_id, public_key, allowed_ips, last_seen, status)
		VALUES ($1, $2, $3, $4, now(), 'active')
		ON CONFLICT (agent_id) DO UPDATE
		SET public_key = EXCLUDED.public_key,
			allowed_ips = EXCLUDED.allowed_ips,
			last_seen = now(),
			status = 'active'
	`
	if _, err := s.pool.Exec(ctx, q, agentID, orgID, publicKey, allowedIPs); err != nil {
		return fmt.Errorf("mesh: upsert peer: %w", err)
	}
	return nil
}

func (s *pgxStore) GetMeshPeer(ctx context.Context, orgID, agentID string) (*MeshPeer, error) {
	if s.pool == nil {
		return nil, ErrNilPool
	}
	if orgID == "" || agentID == "" {
		return nil, errors.New("mesh: org_id and agent_id are required")
	}
	const q = `
		SELECT agent_id, org_id, public_key, allowed_ips, last_seen, status
		FROM mesh_peers
		WHERE org_id = $1 AND agent_id = $2
		LIMIT 1
	`
	row := s.pool.QueryRow(ctx, q, orgID, agentID)
	return scanMeshPeer(row)
}

func (s *pgxStore) ListMeshPeers(ctx context.Context, orgID string) ([]*MeshPeer, error) {
	if s.pool == nil {
		return nil, ErrNilPool
	}
	if orgID == "" {
		return nil, errors.New("mesh: org_id is required")
	}
	const q = `
		SELECT agent_id, org_id, public_key, allowed_ips, last_seen, status
		FROM mesh_peers
		WHERE org_id = $1
		ORDER BY agent_id ASC
	`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("mesh: list peers: %w", err)
	}
	defer rows.Close()
	return scanMeshPeers(rows)
}

func (s *pgxStore) UpdateMeshPeerLastSeen(ctx context.Context, orgID, agentID string, t time.Time) error {
	if s.pool == nil {
		return ErrNilPool
	}
	if orgID == "" || agentID == "" {
		return errors.New("mesh: org_id and agent_id are required")
	}
	const q = `UPDATE mesh_peers SET last_seen = $3 WHERE org_id = $1 AND agent_id = $2`
	if _, err := s.pool.Exec(ctx, q, orgID, agentID, t); err != nil {
		return fmt.Errorf("mesh: update peer last_seen: %w", err)
	}
	return nil
}

func (s *pgxStore) InsertMeshSession(ctx context.Context, orgID, operatorID, agentID, sessionID, purpose string) error {
	if s.pool == nil {
		return ErrNilPool
	}
	if orgID == "" || operatorID == "" || agentID == "" || sessionID == "" {
		return errors.New("mesh: org_id, operator_id, agent_id, session_id are required")
	}
	const q = `
		INSERT INTO mesh_sessions
			(session_id, operator_id, agent_id, org_id, purpose, started_at, status)
		VALUES ($1, $2, $3, $4, $5, now(), 'active')
	`
	if _, err := s.pool.Exec(ctx, q, sessionID, operatorID, agentID, orgID, purpose); err != nil {
		return fmt.Errorf("mesh: insert session: %w", err)
	}
	return nil
}

func (s *pgxStore) CloseMeshSession(ctx context.Context, orgID, sessionID string) error {
	if s.pool == nil {
		return ErrNilPool
	}
	if orgID == "" || sessionID == "" {
		return errors.New("mesh: org_id and session_id are required")
	}
	const q = `
		UPDATE mesh_sessions
		SET status = 'closed', ended_at = now()
		WHERE org_id = $1 AND session_id = $2 AND status = 'active'
	`
	res, err := s.pool.Exec(ctx, q, orgID, sessionID)
	if err != nil {
		return fmt.Errorf("mesh: close session: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrMeshSessionNotFound
	}
	return nil
}

func (s *pgxStore) ListMeshSessions(ctx context.Context, orgID, operatorID string) ([]*MeshSession, error) {
	if s.pool == nil {
		return nil, ErrNilPool
	}
	if orgID == "" {
		return nil, errors.New("mesh: org_id is required")
	}
	var q string
	var args []any
	if operatorID != "" {
		q = `
			SELECT session_id, operator_id, agent_id, org_id, purpose, started_at, ended_at, status
			FROM mesh_sessions
			WHERE org_id = $1 AND operator_id = $2
			ORDER BY started_at DESC
		`
		args = []any{orgID, operatorID}
	} else {
		q = `
			SELECT session_id, operator_id, agent_id, org_id, purpose, started_at, ended_at, status
			FROM mesh_sessions
			WHERE org_id = $1
			ORDER BY started_at DESC
		`
		args = []any{orgID}
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mesh: list sessions: %w", err)
	}
	defer rows.Close()
	return scanMeshSessions(rows)
}

func (s *pgxStore) InsertAgentRelease(ctx context.Context, orgID, version, platform, sha256, signature string, pinned bool) (*AgentRelease, error) {
	if s.pool == nil {
		return nil, ErrNilPool
	}
	if orgID == "" || version == "" || platform == "" {
		return nil, errors.New("mesh: org_id, version, platform are required")
	}
	const q = `
		INSERT INTO agent_releases
			(id, org_id, version, platform, binary_sha256, signature, pinned, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, now())
		RETURNING id, org_id, version, platform, binary_sha256, signature, pinned, created_at
	`
	row := s.pool.QueryRow(ctx, q, orgID, version, platform, sha256, signature, pinned)
	return scanAgentRelease(row)
}

func (s *pgxStore) GetAgentRelease(ctx context.Context, orgID, version string) (*AgentRelease, error) {
	if s.pool == nil {
		return nil, ErrNilPool
	}
	if orgID == "" || version == "" {
		return nil, errors.New("mesh: org_id and version are required")
	}
	const q = `
		SELECT id, org_id, version, platform, binary_sha256, signature, pinned, created_at
		FROM agent_releases
		WHERE org_id = $1 AND version = $2
		LIMIT 1
	`
	row := s.pool.QueryRow(ctx, q, orgID, version)
	return scanAgentRelease(row)
}

func (s *pgxStore) ListAgentReleases(ctx context.Context, orgID string, onlyPinned bool) ([]*AgentRelease, error) {
	if s.pool == nil {
		return nil, ErrNilPool
	}
	if orgID == "" {
		return nil, errors.New("mesh: org_id is required")
	}
	var q string
	var args []any
	if onlyPinned {
		q = `
			SELECT id, org_id, version, platform, binary_sha256, signature, pinned, created_at
			FROM agent_releases
			WHERE org_id = $1 AND pinned = true
			ORDER BY created_at DESC
		`
		args = []any{orgID}
	} else {
		q = `
			SELECT id, org_id, version, platform, binary_sha256, signature, pinned, created_at
			FROM agent_releases
			WHERE org_id = $1
			ORDER BY created_at DESC
		`
		args = []any{orgID}
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mesh: list releases: %w", err)
	}
	defer rows.Close()
	return scanAgentReleases(rows)
}

func (s *pgxStore) PinAgentRelease(ctx context.Context, orgID, version string, pinned bool) error {
	if s.pool == nil {
		return ErrNilPool
	}
	if orgID == "" || version == "" {
		return errors.New("mesh: org_id and version are required")
	}
	const q = `
		UPDATE agent_releases SET pinned = $3 WHERE org_id = $1 AND version = $2
	`
	res, err := s.pool.Exec(ctx, q, orgID, version, pinned)
	if err != nil {
		return fmt.Errorf("mesh: pin release: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrReleaseNotFound
	}
	return nil
}

// ── scanners ──

func scanMeshPeer(row pgx.Row) (*MeshPeer, error) {
	var p MeshPeer
	if err := row.Scan(&p.AgentID, &p.OrgID, &p.PublicKey, &p.AllowedIPs, &p.LastSeen, &p.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMeshPeerNotFound
		}
		return nil, fmt.Errorf("mesh: scan peer: %w", err)
	}
	return &p, nil
}

func scanMeshPeers(rows pgx.Rows) ([]*MeshPeer, error) {
	out := make([]*MeshPeer, 0, 8)
	for rows.Next() {
		var p MeshPeer
		if err := rows.Scan(&p.AgentID, &p.OrgID, &p.PublicKey, &p.AllowedIPs, &p.LastSeen, &p.Status); err != nil {
			return nil, fmt.Errorf("mesh: scan peer row: %w", err)
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func scanMeshSession(row pgx.Row) (*MeshSession, error) {
	var s MeshSession
	var endedAt *time.Time
	if err := row.Scan(&s.SessionID, &s.OperatorID, &s.AgentID, &s.OrgID, &s.Purpose, &s.StartedAt, &endedAt, &s.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMeshSessionNotFound
		}
		return nil, fmt.Errorf("mesh: scan session: %w", err)
	}
	s.EndedAt = endedAt
	return &s, nil
}

func scanMeshSessions(rows pgx.Rows) ([]*MeshSession, error) {
	out := make([]*MeshSession, 0, 8)
	for rows.Next() {
		var s MeshSession
		var endedAt *time.Time
		if err := rows.Scan(&s.SessionID, &s.OperatorID, &s.AgentID, &s.OrgID, &s.Purpose, &s.StartedAt, &endedAt, &s.Status); err != nil {
			return nil, fmt.Errorf("mesh: scan session row: %w", err)
		}
		s.EndedAt = endedAt
		out = append(out, &s)
	}
	return out, rows.Err()
}

func scanAgentRelease(row pgx.Row) (*AgentRelease, error) {
	var r AgentRelease
	if err := row.Scan(&r.ID, &r.OrgID, &r.Version, &r.Platform, &r.BinarySHA256, &r.Signature, &r.Pinned, &r.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("mesh: scan release: %w", err)
	}
	return &r, nil
}

func scanAgentReleases(rows pgx.Rows) ([]*AgentRelease, error) {
	out := make([]*AgentRelease, 0, 8)
	for rows.Next() {
		var r AgentRelease
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Version, &r.Platform, &r.BinarySHA256, &r.Signature, &r.Pinned, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("mesh: scan release row: %w", err)
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
