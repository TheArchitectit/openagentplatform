package relay

// E.3 acceptance (RELAY-06 §7.4 E.3): the "soak" half — sustained forwarding
// over the full authenticated stack (mTLS-issued identity + entitlement + bearer
// token + WSS rendezvous + matching + metered forwarding). Approved thresholds
// 2026-08-27 ("Tiered"): P=50 matched pairs, 1 MiB (just under MaxFrameSize)
// frames, D=5 minutes. The invariant under soak is structural, not
// byte-numerical: every forwarder goroutine (Run + 2 pipes per pair) must exit
// once the legs close — no goroutine leak — and metering must remain internally
// consistent.
//
// Exact per-frame metering is proven elsewhere (E.4 short-run, E.3 load); a
// soak's teardown cuts frames mid-write, so an exact byte oracle is not
// meaningful here.

import (
	"context"
	"crypto/rand"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	soakPairs         = 50               // P — matched pairs (= 100 legs)
	soakFrameSize     = 1 << 20          // 1 MiB — just under MaxFrameSize
	soakSmallFrame    = 64               // keepalive-sized frame every 10th write
	soakFrameInterval = time.Second      // per-leg write cadence (~100 MiB/s aggregate)
	soakDuration      = 5 * time.Minute  // D
	soakTeardownGrace = 15 * time.Second // window for forwarder goroutines to exit
)

// newE2eRelayWithCap builds the full WSS fixture with a specific connection cap
// (the base fixture hardcodes 100, which would be exactly the soak's leg count —
// riding the cap during admission is fragile and not what this test exercises).
func newE2eRelayWithCap(t *testing.T, maxConns int, agentIDs ...string) *e2eRelay {
	t.Helper()
	r := newE2eRelayWithEntitlements(t, fullMeshEntitlements(agentIDs), agentIDs...)
	r.svc.config.MaxConnections = maxConns
	return r
}

// TestE3_Soak_SustainedForwardingNoGoroutineLeak runs P pairs of authenticated
// legs for D minutes while every leg pumps frames through the relay. After
// closing every leg it asserts all forwarder goroutines exit — the structural
// no-leak property a soak exists to catch.
func TestE3_Soak_SustainedForwardingNoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("soak: 5-minute sustained run (use -timeout 10m)")
	}

	// 50 disjoint pairs = 100 agent IDs; the full-mesh fixture entitles each pair.
	agents := make([]string, 0, 2*soakPairs)
	for i := 0; i < soakPairs; i++ {
		agents = append(agents, fmt.Sprintf("soak-a-%d", i), fmt.Sprintf("soak-b-%d", i))
	}
	r := newE2eRelayWithCap(t, 0, agents...) // per-tenant cap already proven in e3_load_test.go

	// Idle baseline: goroutine count with the listener up and zero legs. This
	// is the number the relay must return to once every leg tears down.
	idle := runtime.NumGoroutine()

	// Dial and admit all 100 legs (pair i: soak-a-i ↔ soak-b-i).
	type leg struct {
		conn *websocket.Conn
		stop chan struct{}
	}
	legs := make([]*leg, 0, 2*soakPairs)
	for i := 0; i < soakPairs; i++ {
		a, b := fmt.Sprintf("soak-a-%d", i), fmt.Sprintf("soak-b-%d", i)
		legs = append(legs,
			&leg{conn: r.dialAndAdmit(t, a, b), stop: make(chan struct{})},
			&leg{conn: r.dialAndAdmit(t, b, a), stop: make(chan struct{})},
		)
	}
	r.waitMatched(t, 2*soakPairs)

	maxPayload := make([]byte, soakFrameSize)
	smallPayload := make([]byte, soakSmallFrame)
	if _, err := rand.Read(maxPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(smallPayload); err != nil {
		t.Fatal(err)
	}

	// Per leg: a writer that pumps frames and a reader that drains the partner's
	// forwarded traffic (a non-reading partner would stall the forwarder on
	// backpressure).
	var wg sync.WaitGroup
	for _, l := range legs {
		wg.Add(2)
		go func(l *leg) {
			defer wg.Done()
			tick := time.NewTicker(soakFrameInterval)
			defer tick.Stop()
			for i := 0; ; i++ {
				select {
				case <-l.stop:
					return
				case <-tick.C:
					// Mix frame sizes: 1 MiB most of the time, a 64-byte
					// keepalive-sized frame every 10th write.
					payload := maxPayload
					if i%10 == 0 {
						payload = smallPayload
					}
					if err := l.conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
						return
					}
				}
			}
		}(l)
		go func(l *leg) {
			defer wg.Done()
			for {
				if _, _, err := l.conn.ReadMessage(); err != nil {
					return
				}
			}
		}(l)
	}

	time.Sleep(soakDuration)

	// Teardown: stop writers, close every leg (both sides of each pair so the
	// forwarder's blocked pipe observes the close), then join all client
	// goroutines.
	for _, l := range legs {
		close(l.stop)
		_ = l.conn.Close()
	}
	wg.Wait()

	// Structural no-leak: every Run + 2 pipes goroutine must exit. Poll until the
	// count returns to the idle baseline (plus tolerance for transient
	// listener/scheduler goroutines).
	deadline := time.Now().Add(soakTeardownGrace)
	for runtime.NumGoroutine() > idle+10 {
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: idle baseline=%d, still %d after teardown",
				idle, runtime.NumGoroutine())
		}
		time.Sleep(time.Second)
	}

	// Metering stayed consistent: every tenant byte appears on a connection and
	// vice versa; bytes demonstrably flowed during the soak.
	ctx := context.Background()
	m := r.svc.GetMetrics(ctx, r.tenant)
	if m.TotalBytesRelayed <= 0 {
		t.Fatal("soak relayed no bytes; forwarding did not run")
	}
	var sum int64
	for _, c := range r.svc.ListConnections(ctx, r.tenant) {
		sum += c.BytesRelayed
	}
	if sum != m.TotalBytesRelayed {
		t.Fatalf("tenant metric %d disagrees with connection sum %d", m.TotalBytesRelayed, sum)
	}
}
