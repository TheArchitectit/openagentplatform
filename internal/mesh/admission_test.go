package mesh

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeStore implements Store with in-memory maps for testing Admission.
type fakeStore struct {
	mu       sync.RWMutex
	peers    map[string]*MeshPeer    // key = orgID+"/"+agentID
	sessions map[string]*MeshSession // key = orgID+"/"+sessionID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		peers:    make(map[string]*MeshPeer),
		sessions: make(map[string]*MeshSession),
	}
}

func (f *fakeStore) UpsertMeshPeer(_ context.Context, orgID, agentID, pubKey, allowedIPs string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.peers[orgID+"/"+agentID] = &MeshPeer{
		AgentID:    agentID,
		OrgID:      orgID,
		PublicKey:  pubKey,
		AllowedIPs: allowedIPs,
		LastSeen:   time.Now(),
		Status:    "active",
	}
	return nil
}

func (f *fakeStore) GetMeshPeer(_ context.Context, orgID, agentID string) (*MeshPeer, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.peers[orgID+"/"+agentID]
	if !ok {
		return nil, ErrMeshPeerNotFound
	}
	return p, nil
}

func (f *fakeStore) ListMeshPeers(_ context.Context, orgID string) ([]*MeshPeer, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*MeshPeer
	for _, p := range f.peers {
		if p.OrgID == orgID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeStore) UpdateMeshPeerLastSeen(_ context.Context, orgID, agentID string, t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.peers[orgID+"/"+agentID]; ok {
		p.LastSeen = t
	}
	return nil
}

func (f *fakeStore) InsertMeshSession(_ context.Context, orgID, operatorID, agentID, sessionID, purpose string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[orgID+"/"+sessionID] = &MeshSession{
		SessionID:  sessionID,
		OperatorID: operatorID,
		AgentID:    agentID,
		OrgID:      orgID,
		Purpose:    purpose,
		StartedAt:  time.Now(),
		Status:    "active",
	}
	return nil
}

func (f *fakeStore) CloseMeshSession(_ context.Context, orgID, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[orgID+"/"+sessionID]
	if !ok || s.Status != "active" {
		return ErrMeshSessionNotFound
	}
	s.Status = "closed"
	now := time.Now()
	s.EndedAt = &now
	return nil
}

func (f *fakeStore) ListMeshSessions(_ context.Context, orgID, operatorID string) ([]*MeshSession, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*MeshSession
	for _, s := range f.sessions {
		if s.OrgID != orgID {
			continue
		}
		if operatorID != "" && s.OperatorID != operatorID {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStore) InsertAgentRelease(_ context.Context, orgID, version, platform, sha256, signature string, pinned bool) (*AgentRelease, error) {
	return &AgentRelease{OrgID: orgID, Version: version, Platform: platform, Pinned: pinned}, nil
}
func (f *fakeStore) GetAgentRelease(_ context.Context, orgID, version string) (*AgentRelease, error) {
	return nil, ErrReleaseNotFound
}
func (f *fakeStore) ListAgentReleases(_ context.Context, orgID string, onlyPinned bool) ([]*AgentRelease, error) {
	return nil, nil
}
func (f *fakeStore) PinAgentRelease(_ context.Context, orgID, version string, pinned bool) error {
	return ErrReleaseNotFound
}

func TestRequestSession_Success(t *testing.T) {
	st := newFakeStore()
	km, _ := NewKeyManager(nil)
	adm, err := NewAdmission(km, st, nil)
	if err != nil {
		t.Fatalf("NewAdmission: %v", err)
	}
	// Enroll an agent first.
	st.UpsertMeshPeer(context.Background(), "org1", "agent1", "cHVia2V5", "10.0.0.3/32")

	grant, err := adm.RequestSession(context.Background(), "org1", "op1", "agent1", PurposeVNC)
	if err != nil {
		t.Fatalf("RequestSession: %v", err)
	}
	if grant.SessionID == "" {
		t.Error("expected non-empty session_id")
	}
	if grant.AgentID != "agent1" {
		t.Errorf("AgentID = %q, want agent1", grant.AgentID)
	}
	if grant.AgentPublicKeyWGB64 != "cHVia2V5" {
		t.Errorf("AgentPublicKeyWGB64 = %q, want cHVia2V5", grant.AgentPublicKeyWGB64)
	}
	if grant.OperatorMeshIP == "" {
		t.Error("expected non-empty operator mesh IP")
	}
	if grant.OperatorPrivateKeyWGB64 == "" {
		t.Error("expected non-empty operator WG private key")
	}
	if grant.SSHCertPEM == "" {
		t.Error("expected non-empty SSH cert")
	}
	if grant.SSHCAPublicKeyPEM == "" {
		t.Error("expected non-empty CA public key PEM")
	}
	if grant.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at")
	}
}

func TestRequestSession_AgentNotInMesh(t *testing.T) {
	st := newFakeStore()
	km, _ := NewKeyManager(nil)
	adm, _ := NewAdmission(km, st, nil)

	_, err := adm.RequestSession(context.Background(), "org1", "op1", "unknown-agent", PurposeVNC)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !IsErrAgentNotInMesh(err) {
		t.Errorf("err = %v, want ErrAgentNotInMesh", err)
	}
}

func TestRequestSession_InvalidPurpose(t *testing.T) {
	st := newFakeStore()
	km, _ := NewKeyManager(nil)
	adm, _ := NewAdmission(km, st, nil)
	st.UpsertMeshPeer(context.Background(), "org1", "agent1", "cHVia2V5", "10.0.0.3/32")

	_, err := adm.RequestSession(context.Background(), "org1", "op1", "agent1", "root-shell")
	if err == nil {
		t.Fatal("expected error for invalid purpose")
	}
	if !IsErrInvalidPurpose(err) {
		t.Errorf("err = %v, want ErrInvalidPurpose", err)
	}
}

func TestCloseSession(t *testing.T) {
	st := newFakeStore()
	km, _ := NewKeyManager(nil)
	adm, _ := NewAdmission(km, st, nil)
	st.UpsertMeshPeer(context.Background(), "org1", "agent1", "cHVia2V5", "10.0.0.3/32")

	grant, _ := adm.RequestSession(context.Background(), "org1", "op1", "agent1", PurposeShell)
	if err := adm.CloseSession(context.Background(), "org1", grant.SessionID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	// Double close should fail.
	if err := adm.CloseSession(context.Background(), "org1", grant.SessionID); err == nil {
		t.Fatal("expected error on double close")
	}
}

func TestListSessions(t *testing.T) {
	st := newFakeStore()
	km, _ := NewKeyManager(nil)
	adm, _ := NewAdmission(km, st, nil)
	st.UpsertMeshPeer(context.Background(), "org1", "agent1", "cHVia2V5", "10.0.0.3/32")

	adm.RequestSession(context.Background(), "org1", "op1", "agent1", PurposeVNC)
	adm.RequestSession(context.Background(), "org1", "op1", "agent1", PurposeRDP)

	sessions, err := adm.ListSessions(context.Background(), "org1", "op1")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(sessions))
	}

	// Empty operatorID returns all org sessions.
	all, err := adm.ListSessions(context.Background(), "org1", "")
	if err != nil {
		t.Fatalf("ListSessions all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("got %d sessions for all, want 2", len(all))
	}
}

// IsErrAgentNotInMesh checks the error type without importing errors
// from outside the package (the var is already in this package).
func IsErrAgentNotInMesh(err error) bool { return err == ErrAgentNotInMesh }
func IsErrInvalidPurpose(err error) bool  { return err == ErrInvalidPurpose }
