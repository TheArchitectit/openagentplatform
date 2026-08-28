package relay

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
	"github.com/openagentplatform/openagentplatform/internal/relay/discoverypb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// startFederationServer spins up an in-memory gRPC DiscoveryFederation server
// on a bufconn listener and returns a peer dialer targeting it plus a stop
// func. This exercises the real wire path without mTLS.
func startFederationServer(t *testing.T, relayID string, registry *DiscoveryRegistry) (peerDialer, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	discoverypb.RegisterDiscoveryFederationServer(srv, NewFederationServer(relayID, registry, nil))
	go func() { _ = srv.Serve(lis) }()

	dial := func(cfg FederationPeerConfig) (discoverypb.DiscoveryFederationClient, error) {
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		return discoverypb.NewDiscoveryFederationClient(conn), nil
	}
	return dial, srv.Stop
}

func TestDiscoveryEnvelopeProtoRoundTrip(t *testing.T) {
	env := testEnvelope("agent-a", "t1", 7, VisibilityTenantAllowlisted,
		[]models.Skill{{ID: "patch", Name: "Patch", Description: "applies patches"}},
		time.Hour)
	env.Provenance.OriginRelayID = "relay-west"
	env.Provenance.PublisherAgent = "oap:agent-a"
	env.Provenance.Signature = []byte{1, 2, 3}
	env.Visibility.Allowlist = []string{"t2", "t3"}

	pb := envelopeToProto(env)
	back, err := envelopeFromProto(pb)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if back.Record.ID != env.Record.ID || back.Record.Name != env.Record.Name {
		t.Fatalf("record mismatch: %+v vs %+v", back.Record, env.Record)
	}
	if len(back.Record.Skills) != 1 || back.Record.Skills[0].ID != "patch" {
		t.Fatalf("skills lost in round trip: %+v", back.Record.Skills)
	}
	if back.Version != env.Version || back.TTL != env.TTL {
		t.Fatalf("meta mismatch: version %d ttl %v", back.Version, back.TTL)
	}
	if back.Provenance.OriginRelayID != "relay-west" ||
		back.Provenance.TenantID != "t1" ||
		back.Provenance.PublisherAgent != "oap:agent-a" {
		t.Fatalf("provenance mismatch: %+v", back.Provenance)
	}
	if !back.Provenance.PublishedAt.Equal(env.Provenance.PublishedAt) {
		t.Fatalf("published_at drift: %v vs %v", back.Provenance.PublishedAt, env.Provenance.PublishedAt)
	}
	if back.Visibility.Scope != VisibilityTenantAllowlisted || len(back.Visibility.Allowlist) != 2 {
		t.Fatalf("visibility mismatch: %+v", back.Visibility)
	}
	if string(back.Provenance.Signature) != string([]byte{1, 2, 3}) {
		t.Fatalf("signature lost: %v", back.Provenance.Signature)
	}

	// Nil and empty-record edge cases.
	if envelopeToProto(nil) != nil {
		t.Fatal("nil envelope should encode to nil")
	}
	empty, err := envelopeFromProto(&discoverypb.DiscoveryEnvelope{})
	if err != nil || empty.Record.ID != "" || empty.Version != 0 {
		t.Fatalf("empty proto should decode to zero envelope, got %+v err %v", empty, err)
	}
}

func TestIngestRemoteOriginAuthoritative(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)

	// Ingest a peer record: accepted.
	env := testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	env.Provenance.OriginRelayID = "relay-2"
	if err := d.IngestRemote(env); err != nil {
		t.Fatalf("ingest peer record: %v", err)
	}
	if len(d.Snapshot()) != 1 {
		t.Fatalf("expected 1 record, got %d", len(d.Snapshot()))
	}

	// Replay same version: rejected.
	if err := d.IngestRemote(testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)); err == nil {
		t.Fatal("expected version replay rejection")
	}

	// Newer version from the same origin: accepted.
	v2 := testEnvelope("agent-a", "t1", 2, VisibilityTenantPrivate, nil, time.Hour)
	v2.Provenance.OriginRelayID = "relay-2"
	if err := d.IngestRemote(v2); err != nil {
		t.Fatalf("ingest newer version: %v", err)
	}

	// Our own records echoed back: rejected (local authoritative).
	local := testEnvelope("agent-a", "t1", 3, VisibilityTenantPrivate, nil, time.Hour)
	local.Provenance.OriginRelayID = "relay-1"
	if err := d.IngestRemote(local); err == nil {
		t.Fatal("expected local_authoritative rejection")
	}

	// A different origin for the same agent: conflicting_origin.
	other := testEnvelope("agent-a", "t1", 3, VisibilityTenantPrivate, nil, time.Hour)
	other.Provenance.OriginRelayID = "relay-3"
	if err := d.IngestRemote(other); err == nil {
		t.Fatal("expected conflicting_origin rejection")
	}

	// Missing provenance rejected.
	if err := d.IngestRemote(testEnvelope("agent-b", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)); err == nil {
		t.Fatal("expected provenance-required rejection")
	}
}

func TestIngestRemoteWithdrawTombstone(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)

	// Ingest a peer record, then withdraw it.
	env := testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	env.Provenance.OriginRelayID = "relay-2"
	if err := d.IngestRemote(env); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := d.IngestRemoteWithdraw("agent-a", "relay-2", 2); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if len(d.Snapshot()) != 0 {
		t.Fatal("record should be gone after withdraw")
	}

	// Stale re-publish at or below tombstone: rejected.
	replay := testEnvelope("agent-a", "t1", 2, VisibilityTenantPrivate, nil, time.Hour)
	replay.Provenance.OriginRelayID = "relay-2"
	if err := d.IngestRemote(replay); err == nil {
		t.Fatal("expected tombstone suppression")
	}
	// Publish above tombstone: accepted.
	revive := testEnvelope("agent-a", "t1", 3, VisibilityTenantPrivate, nil, time.Hour)
	revive.Provenance.OriginRelayID = "relay-2"
	if err := d.IngestRemote(revive); err != nil {
		t.Fatalf("publish above tombstone should succeed: %v", err)
	}

	// Local-authoritative withdraw rejected.
	if err := d.IngestRemoteWithdraw("agent-a", "relay-1", 4); err == nil {
		t.Fatal("expected local_authoritative withdrawal rejection")
	}
}

func TestFederationServerPushPullPing(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	dial, stop := startFederationServer(t, "relay-1", d)
	defer stop()

	client, err := dial(FederationPeerConfig{RelayID: "relay-peer", Endpoint: "bufnet"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Push a valid peer record.
	env := testEnvelope("agent-a", "t1", 5, VisibilityTenantPrivate, nil, time.Hour)
	env.Provenance.OriginRelayID = "relay-2"
	env.Record.Name = "Agent A"
	resp, err := client.PushRecord(context.Background(), &discoverypb.PushRequest{Envelope: envelopeToProto(env)})
	if err != nil {
		t.Fatalf("PushRecord: %v", err)
	}
	if !resp.GetAccepted() {
		t.Fatalf("expected accepted, got %s", resp.GetRejectionReason())
	}
	if len(d.Snapshot()) != 1 {
		t.Fatalf("expected 1 ingested record, got %d", len(d.Snapshot()))
	}

	// Replay at lower version: rejected with a reason, not an error.
	resp, err = client.PushRecord(context.Background(), &discoverypb.PushRequest{Envelope: envelopeToProto(env)})
	if err != nil {
		t.Fatalf("PushRecord replay: %v", err)
	}
	if resp.GetAccepted() || resp.GetRejectionReason() == "" {
		t.Fatalf("expected replay rejection with reason, got %+v", resp)
	}

	// Pull returns the ingested record.
	stream, err := client.PullRecords(context.Background(), &discoverypb.PullRequest{RequestingRelayId: "relay-peer", SinceVersion: 0})
	if err != nil {
		t.Fatalf("PullRecords: %v", err)
	}
	count := 0
	for {
		pb, err := stream.Recv()
		if err != nil {
			break
		}
		if pb.GetVersion() != 5 {
			t.Fatalf("unexpected pulled version %d", pb.GetVersion())
		}
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 pulled record, got %d", count)
	}

	// since_version above the record returns nothing.
	stream, err = client.PullRecords(context.Background(), &discoverypb.PullRequest{SinceVersion: 5})
	if err != nil {
		t.Fatalf("PullRecords: %v", err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("expected no records above since_version=5")
	}

	// Ping round-trips.
	if _, err := client.Ping(context.Background(), &discoverypb.PingRequest{}); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestFederationBroadcastAndReconcile(t *testing.T) {
	// registry B plays the peer; registry A fans out to it.
	regB := NewDiscoveryRegistry("relay-b", nil)
	dialB, stopB := startFederationServer(t, "relay-b", regB)
	defer stopB()

	regA := NewDiscoveryRegistry("relay-a", nil)
	fedA := NewFederation("relay-a", regA, nil,
		&FederationSection{
			Peers:            []FederationPeerConfig{{RelayID: "relay-b", Endpoint: "bufnet"}},
			PullInterval:     "1h",
			StartupReconcile: true,
		}, nil)
	fedA.SetDialer(dialB)
	regA.SetObserver(fedA.Broadcast)

	// A local publish is pushed to B (ADR §2.3 push path).
	env := testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	env.Provenance.OriginRelayID = "relay-a"
	env.Record.Name = "Pushed Agent"
	if err := regA.Publish(env); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return len(regB.Snapshot()) == 1 })
	got := regB.Snapshot()["agent-a"]
	if got == nil || got.Record.Name != "Pushed Agent" {
		t.Fatalf("peer did not receive push: %+v", regB.Snapshot())
	}

	// Withdraw is fanned out as a tombstone push.
	if err := regA.Withdraw("agent-a", "t1", 2); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return len(regB.Snapshot()) == 0 })

	// Startup reconciliation: A pulls B's full set synchronously.
	other := testEnvelope("agent-b", "t2", 1, VisibilityTenantPrivate, nil, time.Hour)
	other.Provenance.OriginRelayID = "relay-b"
	if err := regB.Publish(other); err != nil {
		t.Fatalf("publish on peer: %v", err)
	}
	fedA.reconcileAll(context.Background())
	if regA.Snapshot()["agent-b"] == nil {
		t.Fatal("startup reconciliation did not ingest peer record")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
