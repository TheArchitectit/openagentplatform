package relay

// This file implements Phase 2 of the discovery federation successor sprint
// (RELAY-05 ADR §2–§3, §6 Phase 2): the oap.discovery.v1 gRPC federation
// service. It converts between the wire protobuf messages
// (internal/relay/discoverypb) and the local DiscoveryEnvelope model, applies
// the origin-relay-authoritative conflict rules on ingest, and drives the
// hybrid push+pull synchronization loop against configured peers.
//
// Provenance signatures (ADR §1.4) are signed on local publish/withdraw using
// the relay signing key from the trust config, and verified on ingest against
// the origin relay's configured public key. A peer without a configured public
// key is verified at mTLS transport level only (ADR §2.2) — its signatures are
// carried but not checked.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
	"github.com/openagentplatform/openagentplatform/internal/relay/discoverypb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// federationPinInterval is the per-peer liveness probe cadence (ADR §2.5).
const federationPingInterval = 30 * time.Second

// federationFailureThreshold is how many consecutive pull/ping failures mark
// a peer unhealthy (ADR §2.5).
const federationFailureThreshold = 3

// federationPullTimeout and federationPingTimeout bound a single sync RPC so
// one hung peer cannot stall the loop.
const (
	federationPullTimeout = 30 * time.Second
	federationPingTimeout = 5 * time.Second
)

// ---------------------------------------------------------------------------
// Wire conversion
// ---------------------------------------------------------------------------

// envelopeToProto encodes a DiscoveryEnvelope for the wire. The AgentCard is
// carried as JSON (ADR §1.1: no parallel data model). Times carry nanoseconds.
func envelopeToProto(env *DiscoveryEnvelope) *discoverypb.DiscoveryEnvelope {
	if env == nil {
		return nil
	}
	record, _ := json.Marshal(env.Record)
	return &discoverypb.DiscoveryEnvelope{
		Record:   record,
		Version:  env.Version,
		TtlNanos: int64(env.TTL),
		Provenance: &discoverypb.Provenance{
			OriginRelayId:  env.Provenance.OriginRelayID,
			TenantId:       env.Provenance.TenantID,
			PublishedAt:    env.Provenance.PublishedAt.UnixNano(),
			PublisherAgent: env.Provenance.PublisherAgent,
			Signature:      env.Provenance.Signature,
		},
		Visibility: &discoverypb.Visibility{
			Scope:     string(env.Visibility.Scope),
			Allowlist: env.Visibility.Allowlist,
		},
	}
}

// envelopeFromProto decodes a wire DiscoveryEnvelope into the local model.
// A missing or empty record decodes to the zero AgentCard; a malformed record
// is an error (untrusted payload is never stored).
func envelopeFromProto(pb *discoverypb.DiscoveryEnvelope) (*DiscoveryEnvelope, error) {
	if pb == nil {
		return nil, errors.New("discovery: nil envelope")
	}
	var record models.AgentCard
	if len(pb.GetRecord()) > 0 {
		if err := json.Unmarshal(pb.GetRecord(), &record); err != nil {
			return nil, fmt.Errorf("discovery: decode record: %w", err)
		}
	}
	env := &DiscoveryEnvelope{
		Record:  record,
		TTL:     time.Duration(pb.GetTtlNanos()),
		Version: pb.GetVersion(),
	}
	if p := pb.GetProvenance(); p != nil {
		env.Provenance = Provenance{
			OriginRelayID:  p.GetOriginRelayId(),
			TenantID:       p.GetTenantId(),
			PublishedAt:    time.Unix(0, p.GetPublishedAt()).UTC(),
			PublisherAgent: p.GetPublisherAgent(),
			Signature:      p.GetSignature(),
		}
	}
	if v := pb.GetVisibility(); v != nil {
		env.Visibility = Visibility{Scope: VisibilityScope(v.GetScope())}
		if v.GetAllowlist() != nil {
			env.Visibility.Allowlist = append([]string(nil), v.GetAllowlist()...)
		}
	}
	return env, nil
}

// ---------------------------------------------------------------------------
// Federated ingest (ADR §3: origin-relay authoritative)
// ---------------------------------------------------------------------------

// IngestRemote applies a federated record received from a peer. The origin
// relay is authoritative: a record whose origin matches the local relay ID is
// rejected (the local copy wins); a record from a different origin than the
// stored copy is rejected (ADR §1.3: no two relays originate one agent);
// otherwise the incoming record is accepted only when strictly newer.
func (d *DiscoveryRegistry) IngestRemote(env *DiscoveryEnvelope) error {
	if env == nil {
		return errors.New("discovery: nil envelope")
	}
	if env.Record.ID == "" {
		return errors.New("discovery: agent id required")
	}
	if env.Provenance.OriginRelayID == "" || env.Provenance.TenantID == "" {
		return errors.New("discovery: provenance required")
	}
	if env.Version == 0 {
		return errors.New("discovery: version must be positive")
	}
	if env.Provenance.OriginRelayID == d.relayID {
		return errors.New("discovery: local_authoritative")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// ADR §1.4: verify the provenance signature when the origin relay's public
	// key is configured. A missing or invalid signature is rejected — an
	// untrusted record is never stored (ADR §3 rule 4).
	if err := d.verifyProvenanceLocked(env); err != nil {
		return err
	}

	if withdrawn := d.withdrawn[env.Record.ID]; env.Version <= withdrawn {
		return fmt.Errorf("discovery: version %d is not above withdrawn version %d", env.Version, withdrawn)
	}
	if existing, ok := d.records[env.Record.ID]; ok {
		switch {
		case existing.Provenance.OriginRelayID != env.Provenance.OriginRelayID:
			return errors.New("discovery: conflicting_origin")
		case env.Version <= existing.Version:
			return fmt.Errorf("discovery: version %d is not above stored version %d", env.Version, existing.Version)
		}
	}

	cp := *env
	d.records[env.Record.ID] = &cp
	d.log.Info("discovery: federated record ingested",
		"agent_id", cp.Record.ID, "origin_relay", cp.Provenance.OriginRelayID,
		"version", cp.Version)
	return nil
}

// IngestRemoteWithdraw applies a federated withdraw tombstone (ADR §1.5, §3).
// The withdraw carries the same provenance as the record, so its signature is
// verified the same way (ADR §1.4). The tombstone is keyed by the origin
// relay's monotonic version and suppresses stale re-publishes. A withdraw that
// targets a still-present record from a different origin is rejected as a
// conflict.
func (d *DiscoveryRegistry) IngestRemoteWithdraw(env *DiscoveryEnvelope) error {
	if env == nil {
		return errors.New("discovery: nil envelope")
	}
	agentID := env.Record.ID
	originRelayID := env.Provenance.OriginRelayID
	version := env.Version
	if agentID == "" {
		return errors.New("discovery: agent id required")
	}
	if originRelayID == "" {
		return errors.New("discovery: provenance required")
	}
	if version == 0 {
		return errors.New("discovery: version must be positive")
	}
	if originRelayID == d.relayID {
		return errors.New("discovery: local_authoritative")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.verifyProvenanceLocked(env); err != nil {
		return err
	}

	if existing, ok := d.records[agentID]; ok && existing.Provenance.OriginRelayID != originRelayID {
		return errors.New("discovery: conflicting_origin")
	}
	if withdrawn := d.withdrawn[agentID]; version <= withdrawn {
		return fmt.Errorf("discovery: version %d is not above withdrawn version %d", version, withdrawn)
	}
	delete(d.records, agentID)
	d.withdrawn[agentID] = version
	d.log.Info("discovery: federated withdraw applied",
		"agent_id", agentID, "origin_relay", originRelayID, "version", version)
	return nil
}

// ---------------------------------------------------------------------------
// gRPC server
// ---------------------------------------------------------------------------

// discoveryFederationServer serves PushRecord/PullRecords/Ping against the
// local registry. The transport (mTLS peer authentication) is the caller's
// responsibility; this type only applies the conflict rules and returns
// rejection reasons, never errors for a rejected record (ADR §2.1).
type discoveryFederationServer struct {
	discoverypb.UnimplementedDiscoveryFederationServer
	relayID  string
	registry *DiscoveryRegistry
	log      *slog.Logger
}

// NewFederationServer builds the wire handler for the discovery service.
func NewFederationServer(relayID string, registry *DiscoveryRegistry, log *slog.Logger) discoverypb.DiscoveryFederationServer {
	if log == nil {
		log = slog.Default()
	}
	return &discoveryFederationServer{relayID: relayID, registry: registry, log: log}
}

func (s *discoveryFederationServer) PushRecord(ctx context.Context, req *discoverypb.PushRequest) (*discoverypb.PushResponse, error) {
	env, err := envelopeFromProto(req.GetEnvelope())
	if err != nil {
		return &discoverypb.PushResponse{Accepted: false, RejectionReason: "invalid_envelope"}, nil
	}
	if req.GetWithdraw() {
		err = s.registry.IngestRemoteWithdraw(env)
	} else {
		err = s.registry.IngestRemote(env)
	}
	if err != nil {
		s.log.Info("discovery: PushRecord rejected", "err", err)
		return &discoverypb.PushResponse{Accepted: false, RejectionReason: err.Error()}, nil
	}
	return &discoverypb.PushResponse{Accepted: true}, nil
}

func (s *discoveryFederationServer) PullRecords(req *discoverypb.PullRequest, stream grpc.ServerStreamingServer[discoverypb.DiscoveryEnvelope]) error {
	for _, env := range s.registry.Snapshot() {
		if env.Version <= req.GetSinceVersion() {
			continue
		}
		if pb := envelopeToProto(env); pb != nil {
			if err := stream.Send(pb); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *discoveryFederationServer) Ping(context.Context, *discoverypb.PingRequest) (*discoverypb.PingResponse, error) {
	return &discoverypb.PingResponse{}, nil
}

// ---------------------------------------------------------------------------
// Outbound federation (hybrid push+pull, ADR §2.3–§2.5)
// ---------------------------------------------------------------------------

// peerDialer creates a gRPC client for one peer. It is a variable so tests
// can substitute an in-memory client without standing up mTLS.
type peerDialer func(cfg FederationPeerConfig) (discoverypb.DiscoveryFederationClient, error)

// mTLSDiscoveryDialer returns the production dialer: mTLS with the platform CA
// pool (ADR §2.2).
func mTLSDiscoveryDialer(tlsCfg *tls.Config) peerDialer {
	return func(cfg FederationPeerConfig) (discoverypb.DiscoveryFederationClient, error) {
		if tlsCfg == nil {
			return nil, errors.New("discovery: TLS config required for peer dialing")
		}
		conn, err := grpc.NewClient(cfg.Endpoint, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
		if err != nil {
			return nil, err
		}
		return discoverypb.NewDiscoveryFederationClient(conn), nil
	}
}

// federationPeer tracks one configured peer and its client/liveness state.
type federationPeer struct {
	cfg          FederationPeerConfig
	mu           sync.Mutex
	client       discoverypb.DiscoveryFederationClient
	dialErr      error
	pullFailures int
	pingFailures int
	healthy      bool
}

// client returns the lazily-dialed client, dialing on first use.
func (p *federationPeer) getClient(dial peerDialer) (discoverypb.DiscoveryFederationClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		return p.client, nil
	}
	if p.dialErr != nil {
		return nil, p.dialErr
	}
	c, err := dial(p.cfg)
	if err != nil {
		p.dialErr = err
		return nil, err
	}
	p.client = c
	return c, nil
}

// Federation drives outbound synchronization to peers. It wires the registry's
// observer so local publishes/withdraws are pushed immediately (ADR §2.3) and
// runs the periodic pull + ping loops (the self-healing backstop).
type Federation struct {
	relayID          string
	registry         *DiscoveryRegistry
	log              *slog.Logger
	pullInterval     time.Duration
	startupReconcile bool
	dial             peerDialer

	mu    sync.Mutex
	peers map[string]*federationPeer
}

// NewFederation builds a Federation from the trust config's federation section.
// A nil section (or no peers) yields a federation that no-ops: Broadcast and
// Start return immediately. The production mTLS dialer is used unless the
// caller overrides it for tests via SetDialer.
func NewFederation(relayID string, registry *DiscoveryRegistry, tlsCfg *tls.Config, section *FederationSection, log *slog.Logger) *Federation {
	if log == nil {
		log = slog.Default()
	}
	f := &Federation{
		relayID:          relayID,
		registry:         registry,
		log:              log,
		pullInterval:     section.PullIntervalDuration(),
		startupReconcile: section.StartupReconcile,
		dial:             mTLSDiscoveryDialer(tlsCfg),
		peers:            make(map[string]*federationPeer),
	}
	if section != nil {
		for _, pcfg := range section.Peers {
			f.peers[pcfg.RelayID] = &federationPeer{cfg: pcfg}
		}
	}
	return f
}

// SetDialer overrides the peer client factory (test hook).
func (f *Federation) SetDialer(dial peerDialer) {
	f.dial = dial
}

// PeerCount reports the number of configured peers.
func (f *Federation) PeerCount() int {
	return len(f.peers)
}

// Broadcast fans a local change out to every peer (ADR §2.3 push path). It is
// non-blocking: each peer push runs in its own goroutine and failures are
// logged, to be healed by the next pull cycle (ADR §2.5).
func (f *Federation) Broadcast(env *DiscoveryEnvelope, withdraw bool) {
	if len(f.peers) == 0 {
		return
	}
	f.mu.Lock()
	peers := make([]*federationPeer, 0, len(f.peers))
	for _, p := range f.peers {
		peers = append(peers, p)
	}
	f.mu.Unlock()

	req := &discoverypb.PushRequest{Envelope: envelopeToProto(env), Withdraw: withdraw}
	for _, p := range peers {
		go func() {
			client, err := p.getClient(f.dial)
			if err != nil {
				f.log.Warn("discovery: push dropped (no client)", "peer", p.cfg.RelayID, "err", err)
				return
			}
			resp, err := client.PushRecord(context.Background(), req)
			if err != nil {
				f.log.Warn("discovery: push failed", "peer", p.cfg.RelayID, "err", err)
				return
			}
			if !resp.GetAccepted() {
				f.log.Warn("discovery: push rejected", "peer", p.cfg.RelayID, "reason", resp.GetRejectionReason())
			}
		}()
	}
}

// Start begins the synchronization loops: optional startup reconciliation, then
// the periodic pull and ping loops. It returns after the loops are launched;
// cancellation stops them.
func (f *Federation) Start(ctx context.Context) {
	if f.startupReconcile {
		f.reconcileAll(ctx)
	}
	go f.pullLoop(ctx)
	go f.pingLoop(ctx)
}

// reconcileAll performs a full pull from each peer (ADR §2.3 startup
// reconciliation): since_version=0 retrieves the peer's complete set.
func (f *Federation) reconcileAll(ctx context.Context) {
	f.mu.Lock()
	peers := make([]*federationPeer, 0, len(f.peers))
	for _, p := range f.peers {
		peers = append(peers, p)
	}
	f.mu.Unlock()
	for _, p := range peers {
		f.pullFrom(ctx, p, 0)
	}
}

// pullLoop periodically pulls from every healthy peer (ADR §2.3). The pull
// path is the reliability backstop for missed pushes.
func (f *Federation) pullLoop(ctx context.Context) {
	t := time.NewTicker(f.pullInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.mu.Lock()
			peers := make([]*federationPeer, 0, len(f.peers))
			for _, p := range f.peers {
				p.mu.Lock()
				if p.healthy {
					peers = append(peers, p)
				}
				p.mu.Unlock()
			}
			f.mu.Unlock()
			for _, p := range peers {
				// Full pull (since_version=0): per-agent versions are not
				// globally comparable, so a nonzero floor could drop a
				// peer's first-record-for-an-agent (version 1). The RPC field
				// remains supported for callers; the loop stays correct by
				// always pulling the complete set.
				f.pullFrom(ctx, p, 0)
			}
		}
	}
}

// pingLoop probes each peer every federationPingInterval (ADR §2.5). Three
// consecutive failures mark the peer unhealthy; a success clears the marker.
func (f *Federation) pingLoop(ctx context.Context) {
	t := time.NewTicker(federationPingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.mu.Lock()
			peers := make([]*federationPeer, 0, len(f.peers))
			for _, p := range f.peers {
				peers = append(peers, p)
			}
			f.mu.Unlock()
			for _, p := range peers {
				f.ping(ctx, p)
			}
		}
	}
}

func (f *Federation) ping(ctx context.Context, p *federationPeer) {
	client, err := p.getClient(f.dial)
	if err != nil {
		f.notePingFailure(p, err)
		return
	}
	pctx, cancel := context.WithTimeout(ctx, federationPingTimeout)
	defer cancel()
	if _, err := client.Ping(pctx, &discoverypb.PingRequest{}); err != nil {
		f.notePingFailure(p, err)
		return
	}
	p.mu.Lock()
	p.pingFailures = 0
	p.healthy = true
	p.mu.Unlock()
}

func (f *Federation) notePingFailure(p *federationPeer, err error) {
	p.mu.Lock()
	p.pingFailures++
	p.mu.Unlock()
	f.log.Warn("discovery: ping failed", "peer", p.cfg.RelayID, "err", err)
	if p.pingFailures >= federationFailureThreshold {
		p.mu.Lock()
		p.healthy = false
		p.mu.Unlock()
	}
}

// pullFrom streams a peer's records since the given version and ingests each
// via the conflict rules. It trims the peer's consecutive-failure counters.
func (f *Federation) pullFrom(ctx context.Context, p *federationPeer, since uint64) {
	client, err := p.getClient(f.dial)
	if err != nil {
		f.notePullFailure(p, err)
		return
	}
	pctx, cancel := context.WithTimeout(ctx, federationPullTimeout)
	defer cancel()

	stream, err := client.PullRecords(pctx, &discoverypb.PullRequest{RequestingRelayId: f.relayID, SinceVersion: since})
	if err != nil {
		f.notePullFailure(p, err)
		return
	}
	for {
		pb, err := stream.Recv()
		if err != nil {
			f.log.Warn("discovery: pull stream ended", "peer", p.cfg.RelayID, "err", err)
			return
		}
		env, err := envelopeFromProto(pb)
		if err != nil {
			f.log.Warn("discovery: pull received bad record", "peer", p.cfg.RelayID, "err", err)
			continue
		}
		if err := f.registry.IngestRemote(env); err != nil {
			f.log.Debug("discovery: pulled record rejected", "peer", p.cfg.RelayID, "agent_id", env.Record.ID, "err", err)
			continue
		}
	}
}

func (f *Federation) notePullFailure(p *federationPeer, err error) {
	p.mu.Lock()
	p.pullFailures++
	p.mu.Unlock()
	f.log.Warn("discovery: pull failed", "peer", p.cfg.RelayID, "err", err)
	if p.pullFailures >= federationFailureThreshold {
		p.mu.Lock()
		p.healthy = false
		p.mu.Unlock()
	}
}
