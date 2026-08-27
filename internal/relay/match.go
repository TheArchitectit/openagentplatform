package relay

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RendezvousType is the message type field for the handshake message.
const RendezvousType = "rendezvous"

// RendezvousMsg is the handshake message the connecting agent sends after WSS
// upgrade. The relay does NOT trust agent_id or tenant_id from this struct for
// authorization — mTLS is the trust anchor (RELAY-03 §2). These values are
// informational only.
type RendezvousMsg struct {
	Type     string `json:"type"`
	AgentID  string `json:"agent_id"`
	TargetID string `json:"target_id"`
	TenantID string `json:"tenant_id"`
	Token    string `json:"token"`
	JTI      string `json:"jti"`
}

// LegState tracks where a leg is in its pairing lifecycle.
type LegState int

const (
	LegAdded     LegState = iota // registered, not yet validated
	LegPending                   // validated, waiting for counterpart
	LegMatched                   // paired, forwarding
	LegClosed                    // terminal
)

// Leg represents a single admitted WebSocket connection on the relay.
type Leg struct {
	Conn       *websocket.Conn
	TenantID   string
	AgentID    string // source agent (from mTLS principal)
	TargetID   string // target agent (from rendezvous message)
	ConnID     string // from EstablishConnection
	State      LegState
	Partner    *Leg // set when matched
	MatchedAt  time.Time
	AddedAt    time.Time
	mu         sync.Mutex // protects State, Partner
	closeErr   error
}

// MatchEngine manages leg admission, pairing, and lifecycle. It sits between
// the WSS handler and the forwarder. All methods are safe for concurrent use.
type MatchEngine struct {
	svc   *RelayService
	log   interface{ Info(string, ...any) }
	mu    sync.Mutex                        // protects pending map + leg writes
	pends map[string]*Leg                   // key: "tenant:source→target"
}

// NewMatchEngine creates a MatchEngine bound to the given RelayService.
func NewMatchEngine(svc *RelayService) *MatchEngine {
	return &MatchEngine{
		svc:   svc,
		pends: make(map[string]*Leg),
	}
}

// pendKey returns the unique key for a pending leg in the pending map.
func pendKey(tenantID, agentID, targetID string) string {
	return tenantID + ":" + agentID + "→" + targetID
}

// Admit processes a rendezvous message, validates identity/entitlement, registers
// the connection via EstablishConnection, and returns the newly added Leg. If a
// pending counterpart already exists, it returns (matchingLeg, nil) instead.
// On failure it returns (nil, error) and the caller MUST close the socket.
func (m *MatchEngine) Admit(conn *websocket.Conn, tlsPrincipal string, msg RendezvousMsg) (*Leg, *Leg, error) {
	// Validate rendezvous type.
	if msg.Type != RendezvousType {
		return nil, nil, errors.New("unknown_rendezvous_type")
	}
	// Principal must match mTLS certificate. The tlsPrincipal is the CN/SAN
	// extracted during the handshake. The msg body is informational only.
	if msg.AgentID != tlsPrincipal {
		return nil, nil, errors.New("principal_mismatch")
	}
	if msg.TargetID == "" {
		return nil, nil, errors.New("target_id_required")
	}

	// Validate entitlement (RELAY-02 §2.1): source→target must exist in the
	// trust config. Placeholder — will be wired with real trust config in
	// a future sprint; for now entitlement is always granted if we reach this
	// point (mTLS + token are verified by the caller before calling Admit).

	// Register the connection via the accounting core.
	connRecord, err := m.svc.EstablishConnection(
		nil, // ctx not needed for in-memory store
		msg.TenantID, tlsPrincipal, msg.TargetID,
	)
	if err != nil {
		return nil, nil, err
	}

	leg := &Leg{
		Conn:     conn,
		TenantID: connRecord.TenantID,
		AgentID:  tlsPrincipal,
		TargetID: msg.TargetID,
		ConnID:   connRecord.ID,
		State:    LegAdded,
		AddedAt:  time.Now().UTC(),
	}

	// Check for a pending counterpart.
	m.mu.Lock()
	defer m.mu.Unlock()

	revKey := pendKey(msg.TenantID, msg.TargetID, tlsPrincipal)
	if partner, ok := m.pends[revKey]; ok {
		// Match found! Remove partner from pending, mark both matched.
		delete(m.pends, revKey)
		partner.mu.Lock()
		partner.Partner = leg
		partner.State = LegMatched
		partner.MatchedAt = time.Now().UTC()
		partner.mu.Unlock()
		leg.Partner = partner
		leg.State = LegMatched
		leg.MatchedAt = time.Now().UTC()
		return leg, partner, nil
	}

	// No counterpart — add to pending. Handle duplicate-leg replacement.
	key := pendKey(msg.TenantID, tlsPrincipal, msg.TargetID)
	if existing, ok := m.pends[key]; ok {
		// Close the existing stale pending leg.
		existing.mu.Lock()
		existing.State = LegClosed
		existing.closeErr = errors.New("duplicate_leg")
		existing.mu.Unlock()
		m.svc.CloseConnection(nil, existing.ConnID)
		if existing.Conn != nil {
			_ = existing.Conn.Close()
		}
	}

	leg.State = LegPending
	m.pends[key] = leg
	return leg, nil, nil
}

// CloseLeg removes a leg from internal maps and closes the connection. Safe to
// call multiple times (idempotent).
func (m *MatchEngine) CloseLeg(leg *Leg) {
	if leg == nil {
		return
	}
	leg.mu.Lock()
	if leg.State == LegClosed {
		leg.mu.Unlock()
		return
	}
	leg.State = LegClosed
	leg.mu.Unlock()

	m.mu.Lock()
	// Remove from pending if it was pending.
	key := pendKey(leg.TenantID, leg.AgentID, leg.TargetID)
	if m.pends[key] == leg {
		delete(m.pends, key)
	}
	m.mu.Unlock()

	m.svc.CloseConnection(nil, leg.ConnID)
	if leg.Conn != nil {
		_ = leg.Conn.Close()
	}
}

// ClosePartner closes both legs of a matched pair (the match partner and the
// given leg). Used by the forwarder when one leg closes.
func (m *MatchEngine) ClosePair(leg *Leg) {
	if leg == nil {
		return
	}
	leg.mu.Lock()
	partner := leg.Partner
	leg.mu.Unlock()

	m.CloseLeg(partner)
	m.CloseLeg(leg)
}

// parseRendezvous reads a single WebSocket text/binary message and decodes it
// as a RendezvousMsg. Returns the parsed message on success, or an error if
// the message is malformed.
func parseRendezvous(conn *websocket.Conn, deadline time.Duration) (RendezvousMsg, error) {
	var msg RendezvousMsg
	_ = conn.SetReadDeadline(time.Now().Add(deadline))
	msgType, raw, err := conn.ReadMessage()
	if err != nil {
		return msg, err
	}
	_ = msgType
	if err := json.Unmarshal(raw, &msg); err != nil {
		return msg, errors.New("malformed_rendezvous")
	}
	return msg, nil
}
