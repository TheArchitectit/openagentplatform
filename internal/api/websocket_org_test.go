package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// newBareWsClient builds a hub client without a live connection: the
// broadcast path only touches the send channel, so delivery can be
// observed directly.
func newBareWsClient(hub *wsHub, orgID string, ch wsChannel) *wsClient {
	c := &wsClient{
		send:  make(chan []byte, 8),
		subs:  make(map[wsChannel]struct{}),
		orgID: orgID,
		hub:   hub,
	}
	hub.add(c)
	hub.subscribe(c, ch)
	return c
}

// TestBroadcastOrgScopesByTenant verifies that org-scoped broadcasts are
// delivered only to clients of the matching tenant, that an empty org
// fails closed, and that unsubscribed channels receive nothing.
func TestBroadcastOrgScopesByTenant(t *testing.T) {
	hub := newWsHub(newDiscardLogger())
	clientA := newBareWsClient(hub, "org-A", wsChannelChecks)
	clientB := newBareWsClient(hub, "org-B", wsChannelChecks)
	clientAOther := newBareWsClient(hub, "org-A", wsChannelAlerts) // different channel

	hub.BroadcastOrg(wsChannelChecks, "result", "org-A", map[string]string{"v": "1"})

	select {
	case frame := <-clientA.send:
		var msg wsMessage
		if err := json.Unmarshal(frame, &msg); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if msg.Event != "result" || msg.Channel != wsChannelChecks {
			t.Errorf("unexpected frame: channel=%s event=%s", msg.Channel, msg.Event)
		}
		if !strings.Contains(string(msg.Data), `"v":"1"`) {
			t.Errorf("payload = %s, want embedded data", msg.Data)
		}
	default:
		t.Fatal("org-A client did not receive the scoped broadcast")
	}

	select {
	case frame := <-clientB.send:
		t.Fatalf("org-B client received org-A event: %s", frame)
	default:
	}

	// Cross-channel isolation: subscribed to alerts, not checks.
	select {
	case frame := <-clientAOther.send:
		t.Fatalf("alerts subscriber received checks event: %s", frame)
	default:
	}

	// Fail closed: empty org must deliver nothing to anyone.
	hub.BroadcastOrg(wsChannelChecks, "result", "", map[string]string{"v": "2"})
	for name, c := range map[string]*wsClient{"org-A": clientA, "org-B": clientB} {
		select {
		case frame := <-c.send:
			t.Fatalf("%s client received broadcast with empty org scope: %s", name, frame)
		default:
		}
	}

	// BroadcastOrg to an org with no subscribers is a no-op (no panic).
	hub.BroadcastOrg(wsChannelChecks, "result", "org-Z", map[string]string{"v": "3"})
}
