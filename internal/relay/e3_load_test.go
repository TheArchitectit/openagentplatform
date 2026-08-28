package relay

// E.3 acceptance (RELAY-06 §7.4 E.3): per-tenant limits and metering hold under
// concurrency. This is the "load" half — it hammers the in-memory accounting
// core, which is the exact store the WSS admission path reuses (R.3). Approved
// thresholds 2026-08-27 ("Tiered"): C=1000 concurrent connections per tenant
// (matches the --max-connections default), G=64 concurrent workers.
//
// The soak half (live mTLS WSS forwarding) lives in e3_soak_test.go.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	e3LoadCap        = 1000 // C — per-tenant concurrent connection cap
	e3LoadWorkers    = 64   // G — concurrent workers in each hammer phase
	e3BytesPerRecord = 128
)

// e3DiscardService builds a relay for the load tests with a silent logger (the
// fill phase alone emits thousands of Info lines) and an idle timeout long
// enough that nothing is reaped mid-test.
func e3DiscardService() *RelayService {
	return NewRelayService(
		RelayConfig{MaxConnections: e3LoadCap, IdleTimeout: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func isLimitErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "connection limit reached")
}

// TestE3_Load_PerTenantCapUnderConcurrentEstablish drives 2C establish attempts
// per tenant from G=64 goroutines and asserts exactly C succeed and exactly C
// are rejected with a limit error — proving the per-tenant active count is
// enforced (and race-free) under concurrency. It then proves the cap is
// per-tenant (not global) and that ConnectionCount tracks close/refill exactly.
func TestE3_Load_PerTenantCapUnderConcurrentEstablish(t *testing.T) {
	svc := e3DiscardService()
	ctx := context.Background()
	const t0, t1 = "load-a", "load-b"

	for _, tenant := range []string{t0, t1} {
		var successes, limits, others atomic.Int64
		per := 2 * e3LoadCap / e3LoadWorkers // 31
		remainder := 2*e3LoadCap - per*e3LoadWorkers

		var wg sync.WaitGroup
		wg.Add(e3LoadWorkers)
		for w := 0; w < e3LoadWorkers; w++ {
			go func(w int) {
				defer wg.Done()
				n := per
				if w < remainder {
					n++
				}
				for i := 0; i < n; i++ {
					_, err := svc.EstablishConnection(ctx, tenant, "src", "tgt")
					switch {
					case err == nil:
						successes.Add(1)
					case isLimitErr(err):
						limits.Add(1)
					default:
						others.Add(1)
					}
				}
			}(w)
		}
		wg.Wait()

		if got := successes.Load(); got != e3LoadCap {
			t.Fatalf("tenant %s: %d establishes succeeded, want exactly cap %d", tenant, got, e3LoadCap)
		}
		if got := limits.Load(); got != e3LoadCap {
			t.Fatalf("tenant %s: %d limit rejections, want exactly cap %d (concurrent overshoot)", tenant, got, e3LoadCap)
		}
		if got := others.Load(); got != 0 {
			t.Fatalf("tenant %s: %d unexpected non-limit errors", tenant, got)
		}
		if m := svc.GetMetrics(ctx, tenant); m.ConnectionCount != e3LoadCap {
			t.Fatalf("tenant %s: ConnectionCount=%d, want %d", tenant, m.ConnectionCount, e3LoadCap)
		}
	}

	// Per-tenant isolation: freeing a slot on t1 must not free one on t0.
	idle := svc.ListConnections(ctx, t1)[0]
	if err := svc.CloseConnection(ctx, idle.ID); err != nil {
		t.Fatalf("close t1 slot: %v", err)
	}
	if m := svc.GetMetrics(ctx, t1); m.ConnectionCount != e3LoadCap-1 {
		t.Fatalf("t1 after close: ConnectionCount=%d, want %d", m.ConnectionCount, e3LoadCap-1)
	}
	if _, err := svc.EstablishConnection(ctx, t1, "src", "tgt"); err != nil {
		t.Fatalf("t1 should accept after freeing a slot: %v", err)
	}
	if _, err := svc.EstablishConnection(ctx, t0, "src", "tgt"); err == nil {
		t.Fatal("t0 stayed at cap but accepted a new connection (limit is not per-tenant)")
	}

	// Close under concurrency: retire and refill a batch on each tenant.
	const batch = 100
	for _, tenant := range []string{t0, t1} {
		victims := svc.ListConnections(ctx, tenant)[:batch]
		var wg sync.WaitGroup
		wg.Add(e3LoadWorkers)
		for w := 0; w < e3LoadWorkers; w++ {
			go func(w int) {
				defer wg.Done()
				for i := w; i < len(victims); i += e3LoadWorkers {
					if err := svc.CloseConnection(ctx, victims[i].ID); err != nil {
						t.Errorf("close %s: %v", victims[i].ID, err)
					}
				}
			}(w)
		}
		wg.Wait()
		if m := svc.GetMetrics(ctx, tenant); m.ConnectionCount != e3LoadCap-batch {
			t.Fatalf("tenant %s: after batch close ConnectionCount=%d, want %d", tenant, m.ConnectionCount, e3LoadCap-batch)
		}
		for i := 0; i < batch; i++ {
			if _, err := svc.EstablishConnection(ctx, tenant, "src", "tgt"); err != nil {
				t.Fatalf("tenant %s: refill %d: %v", tenant, i, err)
			}
		}
		if m := svc.GetMetrics(ctx, tenant); m.ConnectionCount != e3LoadCap {
			t.Fatalf("tenant %s: after refill ConnectionCount=%d, want %d", tenant, m.ConnectionCount, e3LoadCap)
		}
	}
}

// TestE3_Load_MeteringExactUnderConcurrency fills two tenants to the cap, then
// records a fixed byte count once on every connection from G=64 workers. The
// per-tenant aggregate and the independent per-connection sum must both be
// exact — no lost or double counts under concurrent RecordBytes.
func TestE3_Load_MeteringExactUnderConcurrency(t *testing.T) {
	svc := e3DiscardService()
	ctx := context.Background()
	const t0, t1 = "load-a", "load-b"

	all := make([]*RelayConnection, 0, 2*e3LoadCap)
	for _, tenant := range []string{t0, t1} {
		for i := 0; i < e3LoadCap; i++ {
			c, err := svc.EstablishConnection(ctx, tenant, "src", "tgt")
			if err != nil {
				t.Fatalf("fill %s conn %d: %v", tenant, i, err)
			}
			all = append(all, c)
		}
	}

	var recorded atomic.Int64
	var wg sync.WaitGroup
	wg.Add(e3LoadWorkers)
	for w := 0; w < e3LoadWorkers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := w; i < len(all); i += e3LoadWorkers {
				if err := svc.RecordBytes(ctx, all[i].ID, e3BytesPerRecord); err != nil {
					t.Errorf("record %s: %v", all[i].ID, err)
					return
				}
				recorded.Add(e3BytesPerRecord)
			}
		}(w)
	}
	wg.Wait()

	if want := int64(len(all)) * e3BytesPerRecord; recorded.Load() != want {
		t.Fatalf("test bookkeeping: recorded=%d, want %d", recorded.Load(), want)
	}
	for _, tenant := range []string{t0, t1} {
		m := svc.GetMetrics(ctx, tenant)
		if want := int64(e3LoadCap) * e3BytesPerRecord; m.TotalBytesRelayed != want {
			t.Fatalf("tenant %s: TotalBytesRelayed=%d, want %d", tenant, m.TotalBytesRelayed, want)
		}
		var sum int64
		for _, c := range svc.ListConnections(ctx, tenant) {
			sum += c.BytesRelayed
		}
		if sum != m.TotalBytesRelayed {
			t.Fatalf("tenant %s: per-connection sum=%d disagrees with metric %d", tenant, sum, m.TotalBytesRelayed)
		}
	}
}
