package relay

import (
	"context"
	"testing"
	"time"
)

// fakeStore records every call so tests can assert the §8 write-through and
// flush contract without a database.
type fakeStore struct {
	inserted  map[string]*RelayConnection
	closed    map[string]time.Time
	bytes     map[string]int64            // connKey -> final bytes
	tenantAdd map[string]int64            // tenantID -> accumulated deltas
	tenantConns map[string]int64          // tenantID -> accumulated connection-count deltas
	metrics   map[string]*RelayMetrics    // preloaded for rehydration
	stale     int64                       // rows CloseStaleActive reports closed
	failAdd   bool                        // force AddTenantBytes to fail
	rebuffer  map[string]int64            // deltas rebuffered after a failed flush
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		inserted: make(map[string]*RelayConnection),
		closed:   make(map[string]time.Time),
		bytes:    make(map[string]int64),
		tenantAdd: make(map[string]int64),
		tenantConns: make(map[string]int64),
		rebuffer: make(map[string]int64),
	}
}

func (f *fakeStore) InsertConnection(_ context.Context, c *RelayConnection) error {
	cp := *c
	f.inserted[c.ID] = &cp
	return nil
}

func (f *fakeStore) MarkClosed(_ context.Context, connKey string, closedAt time.Time) error {
	f.closed[connKey] = closedAt
	return nil
}

func (f *fakeStore) UpdateBytes(_ context.Context, connKey string, bytes int64) error {
	f.bytes[connKey] = bytes
	return nil
}

func (f *fakeStore) AddTenantBytes(_ context.Context, tenantID string, delta int64) error {
	if f.failAdd {
		return context.DeadlineExceeded
	}
	f.tenantAdd[tenantID] += delta
	return nil
}

func (f *fakeStore) AddTenantConnections(_ context.Context, tenantID string, delta int64) error {
	f.tenantConns[tenantID] += delta
	return nil
}

func (f *fakeStore) LoadTenantMetrics(_ context.Context) (map[string]*RelayMetrics, error) {
	return f.metrics, nil
}

func (f *fakeStore) CloseStaleActive(_ context.Context, _ time.Time) (int64, error) {
	return f.stale, nil
}

func (f *fakeStore) Close() {}

// §8.1: no store installed = zero behavior change (no panics on the
// write-through paths, no panics from RecordBytes/flush).
func TestNoStore_InMemoryOnly(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	conn, err := svc.EstablishConnection(context.Background(), "t1", "a", "b")
	if err != nil {
		t.Fatalf("establish: %v", err)
	}
	if err := svc.RecordBytes(context.Background(), conn.ID, 100); err != nil {
		t.Fatalf("record bytes: %v", err)
	}
	svc.FlushPendingBytes(context.Background()) // must be a no-op, not a panic
	if err := svc.CloseConnection(context.Background(), conn.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if svc.Store() != nil {
		t.Fatal("expected nil store")
	}
}

// §8.3: establish/close write through synchronously.
func TestStore_WriteThrough(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	fs := newFakeStore()
	if err := svc.SetStore(context.Background(), fs); err != nil {
		t.Fatalf("SetStore: %v", err)
	}
	if len(fs.metrics) != 0 {
		t.Fatalf("no metrics expected preloaded")
	}

	conn, err := svc.EstablishConnection(context.Background(), "t1", "a", "b")
	if err != nil {
		t.Fatalf("establish: %v", err)
	}
	if _, ok := fs.inserted[conn.ID]; !ok {
		t.Fatal("establish did not persist the connection")
	}
	if err := svc.RecordBytes(context.Background(), conn.ID, 42); err != nil {
		t.Fatalf("record bytes: %v", err)
	}
	if err := svc.CloseConnection(context.Background(), conn.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := fs.closed[conn.ID]; !ok {
		t.Fatal("close was not persisted")
	}
	if got := fs.bytes[conn.ID]; got != 42 {
		t.Errorf("final bytes not persisted: got %d, want 42", got)
	}
	if fs.tenantAdd["t1"] != 0 {
		t.Errorf("no flush yet; tenantAdd should be 0, got %d", fs.tenantAdd["t1"])
	}
}

// §8.4: RecordBytes buffers; FlushPendingBytes moves the delta to the store;
// a failed flush re-buffers so no metering is lost.
func TestStore_FlushDeltas(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	fs := newFakeStore()
	if err := svc.SetStore(context.Background(), fs); err != nil {
		t.Fatalf("SetStore: %v", err)
	}
	conn, _ := svc.EstablishConnection(context.Background(), "t1", "a", "b")
	_ = svc.RecordBytes(context.Background(), conn.ID, 10)
	_ = svc.RecordBytes(context.Background(), conn.ID, 32)

	svc.FlushPendingBytes(context.Background())
	if got := fs.tenantAdd["t1"]; got != 42 {
		t.Fatalf("flushed delta = %d, want 42", got)
	}
	// Buffered deltas consumed.
	svc.mu.Lock()
	n := len(svc.pendingBytes)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("pendingBytes not drained: %d left", n)
	}

	// Failed flush re-buffers.
	fs.failAdd = true
	_ = svc.RecordBytes(context.Background(), conn.ID, 7)
	svc.FlushPendingBytes(context.Background())
	svc.mu.Lock()
	pending := svc.pendingBytes["t1"]
	svc.mu.Unlock()
	if pending != 7 {
		t.Fatalf("delta not rebuffered after failure: %d", pending)
	}
	fs.failAdd = false
	svc.FlushPendingBytes(context.Background())
	if got := fs.tenantAdd["t1"]; got != 49 {
		t.Fatalf("recovered flush = %d, want 49", got)
	}
}

// §8.5: boot rehydrates persisted aggregates and closes stale active rows.
func TestStore_Rehydrate(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	fs := newFakeStore()
	fs.metrics = map[string]*RelayMetrics{
		"t9": {TenantID: "t9", ConnectionCount: 0, TotalConnections: 12, TotalBytesRelayed: 3456},
	}
	fs.stale = 3
	if err := svc.SetStore(context.Background(), fs); err != nil {
		t.Fatalf("SetStore: %v", err)
	}
	m := svc.GetMetrics(context.Background(), "t9")
	if m.TotalConnections != 12 || m.TotalBytesRelayed != 3456 {
		t.Fatalf("rehydrated metrics wrong: %+v", m)
	}

	// RecordBytes on top of rehydrated metrics accumulates (no overwrite).
	conn, _ := svc.EstablishConnection(context.Background(), "t9", "a", "b")
	_ = svc.RecordBytes(context.Background(), conn.ID, 54)
	if got := svc.GetMetrics(context.Background(), "t9").TotalBytesRelayed; got != 3510 {
		t.Fatalf("post-rehydrate accumulation = %d, want 3510", got)
	}
}

// §8.3: store failures never abort the in-memory data plane.
func TestStore_WriteFailure_NonFatal(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	fs := newFakeStore()
	if err := svc.SetStore(context.Background(), fs); err != nil {
		t.Fatalf("SetStore: %v", err)
	}
	// A store that fails on AddTenantBytes must not break RecordBytes.
	fs.failAdd = true
	conn, err := svc.EstablishConnection(context.Background(), "t1", "a", "b")
	if err != nil {
		t.Fatalf("establish: %v", err)
	}
	if err := svc.RecordBytes(context.Background(), conn.ID, 10); err != nil {
		t.Fatalf("record bytes with failing flush: %v", err)
	}
	if err := svc.CloseConnection(context.Background(), conn.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// §8.5a: the lifetime connection counter must be buffered on establish and
// flushed with the deltas (audit F1: it was never persisted, so billing's
// TotalConnections reset on restart).
func TestStore_FlushConnectionCount(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	fs := newFakeStore()
	if err := svc.SetStore(context.Background(), fs); err != nil {
		t.Fatalf("SetStore: %v", err)
	}
	if _, err := svc.EstablishConnection(context.Background(), "t1", "a", "b"); err != nil {
		t.Fatalf("establish: %v", err)
	}
	if _, err := svc.EstablishConnection(context.Background(), "t1", "a", "c"); err != nil {
		t.Fatalf("establish: %v", err)
	}
	if fs.tenantConns["t1"] != 0 {
		t.Fatalf("no flush yet; tenantConns should be 0, got %d", fs.tenantConns["t1"])
	}
	svc.FlushPendingBytes(context.Background())
	if got := fs.tenantConns["t1"]; got != 2 {
		t.Fatalf("flushed connection delta = %d, want 2", got)
	}
}

// §8.4: the periodic flush also updates in-flight connections' BytesRelayed
// (audit F2: only close persisted bytes, so a crash left stale ledger rows).
func TestStore_FlushInFlightBytes(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)
	fs := newFakeStore()
	if err := svc.SetStore(context.Background(), fs); err != nil {
		t.Fatalf("SetStore: %v", err)
	}
	conn, _ := svc.EstablishConnection(context.Background(), "t1", "a", "b")
	_ = svc.RecordBytes(context.Background(), conn.ID, 77)
	svc.FlushPendingBytes(context.Background())
	if got := fs.bytes[conn.ID]; got != 77 {
		t.Fatalf("in-flight bytes = %d, want 77", got)
	}
}
