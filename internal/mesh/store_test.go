package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
)

// newTestStore builds a pgxStore backed by a pgxmock pool.
func newTestStore(t *testing.T) (Store, pgxmock.PgxPoolIface, func()) {
	t.Helper()
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	s := NewStore(pool)
	return s, pool, pool.Close
}

func TestUpsertMeshPeer_Insert(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()
	pool.ExpectExec(`INSERT INTO mesh_peers`).
		WithArgs(agentID, orgID, "abc123pubkey", "10.0.0.2/32").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.UpsertMeshPeer(context.Background(), orgID, agentID, "abc123pubkey", "10.0.0.2/32"); err != nil {
		t.Fatalf("UpsertMeshPeer: %v", err)
	}
}

func TestGetMeshPeer_OrgScoped(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()
	now := time.Now()
	rows := pgxmock.NewRows([]string{"agent_id", "org_id", "public_key", "allowed_ips", "last_seen", "status"}).
		AddRow(agentID, orgID, "abc123pubkey", "10.0.0.2/32", now, "active")
	pool.ExpectQuery(`SELECT .* FROM mesh_peers WHERE org_id = \$1 AND agent_id = \$2`).
		WithArgs(orgID, agentID).
		WillReturnRows(rows)

	got, err := s.GetMeshPeer(context.Background(), orgID, agentID)
	if err != nil {
		t.Fatalf("GetMeshPeer: %v", err)
	}
	if got.AgentID != agentID || got.OrgID != orgID {
		t.Errorf("got agent=%s org=%s, want agent=%s org=%s", got.AgentID, got.OrgID, agentID, orgID)
	}
	if got.PublicKey != "abc123pubkey" {
		t.Errorf("got public_key=%q", got.PublicKey)
	}
}

func TestListMeshSessions_OrgScoped(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	operatorID := uuid.NewString()
	agentID := uuid.NewString()
	sessionID := uuid.NewString()
	now := time.Now()
	rows := pgxmock.NewRows([]string{"session_id", "operator_id", "agent_id", "org_id", "purpose", "started_at", "ended_at", "status"}).
		AddRow(sessionID, operatorID, agentID, orgID, "vnc", now, nil, "active")
	pool.ExpectQuery(`SELECT .* FROM mesh_sessions WHERE org_id = \$1 AND operator_id = \$2`).
		WithArgs(orgID, operatorID).
		WillReturnRows(rows)

	got, err := s.ListMeshSessions(context.Background(), orgID, operatorID)
	if err != nil {
		t.Fatalf("ListMeshSessions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	if got[0].SessionID != sessionID || got[0].Purpose != "vnc" {
		t.Errorf("unexpected session %+v", got[0])
	}
	if got[0].EndedAt != nil {
		t.Errorf("active session should have nil ended_at, got %v", got[0].EndedAt)
	}
}

func TestListMeshSessions_AllOrgSessions(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	// Empty operatorID means admin view — all sessions for the org.
	rows := pgxmock.NewRows([]string{"session_id", "operator_id", "agent_id", "org_id", "purpose", "started_at", "ended_at", "status"})
	pool.ExpectQuery(`SELECT .* FROM mesh_sessions WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(rows)

	got, err := s.ListMeshSessions(context.Background(), orgID, "")
	if err != nil {
		t.Fatalf("ListMeshSessions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d sessions, want 0", len(got))
	}
}

func TestInsertAgentRelease(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	releaseID := uuid.NewString()
	now := time.Now()
	rows := pgxmock.NewRows([]string{"id", "org_id", "version", "platform", "binary_sha256", "signature", "pinned", "created_at"}).
		AddRow(releaseID, orgID, "1.2.3", "linux/amd64", "sha256abc", "ed25519sig", true, now)
	pool.ExpectQuery(`INSERT INTO agent_releases`).
		WithArgs(orgID, "1.2.3", "linux/amd64", "sha256abc", "ed25519sig", true).
		WillReturnRows(rows)

	got, err := s.InsertAgentRelease(context.Background(), orgID, "1.2.3", "linux/amd64", "sha256abc", "ed25519sig", true)
	if err != nil {
		t.Fatalf("InsertAgentRelease: %v", err)
	}
	if got.Version != "1.2.3" || got.Pinned != true {
		t.Errorf("unexpected release %+v", got)
	}
}

func TestPinAgentRelease(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	pool.ExpectExec(`UPDATE agent_releases SET pinned = \$3 WHERE org_id = \$1 AND version = \$2`).
		WithArgs(orgID, "1.2.3", true).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := s.PinAgentRelease(context.Background(), orgID, "1.2.3", true); err != nil {
		t.Fatalf("PinAgentRelease: %v", err)
	}
}

func TestPinAgentRelease_NotFound(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	pool.ExpectExec(`UPDATE agent_releases SET pinned = \$3 WHERE org_id = \$1 AND version = \$2`).
		WithArgs(orgID, "9.9.9", true).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := s.PinAgentRelease(context.Background(), orgID, "9.9.9", true)
	if err != ErrReleaseNotFound {
		t.Errorf("got err=%v, want ErrReleaseNotFound", err)
	}
}

func TestUpsertMeshPeer_MissingOrgID(t *testing.T) {
	s, _, closeFn := newTestStore(t)
	defer closeFn()

	err := s.UpsertMeshPeer(context.Background(), "", "agent1", "key", "10.0.0.2/32")
	if err == nil {
		t.Fatal("expected error for empty org_id")
	}
}

func TestCloseMeshSession_NotFound(t *testing.T) {
	s, pool, closeFn := newTestStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	sessionID := uuid.NewString()
	pool.ExpectExec(`UPDATE mesh_sessions SET status = 'closed', ended_at = now\(\) WHERE org_id = \$1 AND session_id = \$2 AND status = 'active'`).
		WithArgs(orgID, sessionID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := s.CloseMeshSession(context.Background(), orgID, sessionID)
	if err != ErrMeshSessionNotFound {
		t.Errorf("got err=%v, want ErrMeshSessionNotFound", err)
	}
}
