package telemetry

import (
	"sync"
	"sync/atomic"
)

// ---- Snapshot support for /api/v1/metrics/summary ------------------------
//
// We keep lightweight atomic snapshots alongside the Prometheus collectors
// so the JSON summary endpoint can return roll-up values without scraping
// the full registry.  These are best-effort roll-ups; the full series are
// always available at /metrics.

var (
	counterSnap sync.Map // name -> *int64
	gaugeSnap   sync.Map // name -> *float64
)

func bumpCounter(name string, delta int64) {
	v, _ := counterSnap.LoadOrStore(name, new(int64))
	atomic.AddInt64(v.(*int64), delta)
}

func setGauge(name string, val float64) {
	v, _ := gaugeSnap.LoadOrStore(name, &float64Atomic{})
	v.(*float64Atomic).Store(val)
}

// RecordCounterRollup adds delta to the named counter in the JSON-summary
// snapshot. Call sites that already record into Prometheus (e.g.
// metricsMiddleware) call this alongside so GET /api/v1/metrics/summary
// reports live roll-ups instead of an empty document.
func RecordCounterRollup(name string, delta int64) {
	bumpCounter(name, delta)
}

// SetGaugeRollup sets the named gauge in the JSON-summary snapshot.
func SetGaugeRollup(name string, val float64) {
	setGauge(name, val)
}

// SnapshotCounters returns a copy of the current counter roll-ups keyed by
// the registered metric name.  Returns nil if nothing has been recorded.
func SnapshotCounters() map[string]int64 {
	out := map[string]int64{}
	counterSnap.Range(func(k, v any) bool {
		out[k.(string)] = atomic.LoadInt64(v.(*int64))
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// SnapshotGauges returns a copy of the current gauge roll-ups keyed by the
// registered metric name.  Returns nil if nothing has been recorded.
func SnapshotGauges() map[string]float64 {
	out := map[string]float64{}
	gaugeSnap.Range(func(k, v any) bool {
		out[k.(string)] = v.(*float64Atomic).Load()
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
