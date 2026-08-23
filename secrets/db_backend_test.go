package secrets

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Mock DBQuerier ---

type mockQuerier struct {
	mu      sync.Mutex
	data    map[string][]mockSecretRow // path -> rows ordered by version
	pingErr error
}

type mockSecretRow struct {
	version   int
	data      []byte
	labels    []byte
	createdAt time.Time
}

func NewMockQuerier() *mockQuerier {
	return &mockQuerier{data: make(map[string][]mockSecretRow)}
}

func (m *mockQuerier) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.HasPrefix(query, "CREATE") {
		return &mockResult{}, nil
	}

	if strings.HasPrefix(query, "INSERT") {
		if len(args) < 3 {
			return nil, fmt.Errorf("INSERT requires 3+ args")
		}
		path, _ := args[0].(string)
		version, _ := args[1].(int)
		data, _ := args[2].([]byte)
		var labels []byte
		if len(args) > 3 {
			labels, _ = args[3].([]byte)
		}
		m.data[path] = append(m.data[path], mockSecretRow{
			version: version, data: data, labels: labels, createdAt: time.Now(),
		})
		return &mockResult{rowsAffected: 1}, nil
	}

	if strings.HasPrefix(query, "DELETE") {
		path, _ := args[0].(string)
		rows := m.data[path]
		if len(args) > 1 {
			// Specific versions — args[1:] are version ints.
			versionSet := make(map[int]bool)
			for _, a := range args[1:] {
				if v, ok := a.(int); ok {
					versionSet[v] = true
				}
			}
			filtered := rows[:0]
			for _, r := range rows {
				if !versionSet[r.version] {
					filtered = append(filtered, r)
				}
			}
			m.data[path] = filtered
		} else {
			delete(m.data, path)
		}
		return &mockResult{rowsAffected: 1}, nil
	}

	return &mockResult{}, nil
}

func (m *mockQuerier) QueryRowContext(_ context.Context, query string, args ...any) *sql.Row {
	// We can't construct a real *sql.Row without a driver.
	// Instead, use QueryContext for our interface.
	// This is a limitation — for proper testing, use a real sqlite DB.
	return nil
}

func (m *mockQuerier) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, fmt.Errorf("QueryContext not implemented in mock")
}

func (m *mockQuerier) PingContext(_ context.Context) error {
	return m.pingErr
}

type mockResult struct {
	lastInsertID int64
	rowsAffected int64
}

func (r *mockResult) LastInsertId() (int64, error)  { return r.lastInsertID, nil }
func (r *mockResult) RowsAffected() (int64, error)  { return r.rowsAffected, nil }

// --- Tests that don't require sql.Row ---

func TestDBBackend_NilDB(t *testing.T) {
	_, err := NewDBBackend(nil, DBBackendConfig{})
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestDBBackend_SupportsDynamic(t *testing.T) {
	b := &DBBackend{}
	if b.SupportsDynamic() {
		t.Fatal("expected false for static backend")
	}
}

func TestDBBackend_Close_NoOp(t *testing.T) {
	b := &DBBackend{}
	if err := b.Close(context.Background()); err != nil {
		t.Fatalf("Close should be no-op: %v", err)
	}
}

func TestDBBackend_RevokeLease(t *testing.T) {
	b := &DBBackend{}
	err := b.RevokeLease(context.Background(), "lease-123")
	if err != ErrNotSupported {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
}

func TestDBBackend_Delete_Paths(t *testing.T) {
	m := NewMockQuerier()
	b, err := NewDBBackendFromQuerier(m, DBBackendConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := context.Background()

	// Delete all — no versions arg.
	b.Delete(ctx, "del/test", DeleteOptions{})
	m.mu.Lock()
	if _, ok := m.data["del/test"]; ok {
		t.Fatalf("expected path to be deleted")
	}
	m.mu.Unlock()

	// Delete specific versions.
	m.data["del/test"] = []mockSecretRow{
		{version: 1, data: []byte(`{"k":"v1"}`)},
		{version: 2, data: []byte(`{"k":"v2"}`)},
	}
	b.Delete(ctx, "del/test", DeleteOptions{Versions: []int{1}})
	m.mu.Lock()
	if len(m.data["del/test"]) != 1 {
		t.Fatalf("expected 1 row after version delete, got %d", len(m.data["del/test"]))
	}
	if m.data["del/test"][0].version != 2 {
		t.Fatalf("expected version 2 to remain, got %d", m.data["del/test"][0].version)
	}
	m.mu.Unlock()
}

func TestDBBackend_Healthcheck(t *testing.T) {
	m := &mockQuerier{pingErr: fmt.Errorf("db down")}
	b, err := NewDBBackendFromQuerier(m, DBBackendConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = b.Healthcheck(context.Background())
	if err == nil || err.Error() != "db down" {
		t.Fatalf("expected 'db down', got %v", err)
	}

	m.pingErr = nil
	err = b.Healthcheck(context.Background())
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// --- Tests using MemoryBackend (integration-style) ---

func TestSecretBackend_GetSet_Delete(t *testing.T) {
	b := NewMemoryBackend()
	ctx := context.Background()

	_, err := b.Get(ctx, "missing", nil)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}

	ver, err := b.Set(ctx, "s/k", map[string]any{"password": "p"}, SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ver.Version != 1 {
		t.Fatalf("expected version 1, got %d", ver.Version)
	}

	val, _ := b.Get(ctx, "s/k", nil)
	if val.Data["password"] != "p" {
		t.Fatalf("unexpected data: %v", val.Data)
	}
	if val.Version != 1 {
		t.Fatalf("expected version 1, got %d", val.Version)
	}

	ver2, _ := b.Set(ctx, "s/k", map[string]any{"password": "p2"}, SetOptions{})
	if ver2.Version != 2 {
		t.Fatalf("expected version 2, got %d", ver2.Version)
	}

	val2, _ := b.Get(ctx, "s/k", nil)
	if val2.Data["password"] != "p2" {
		t.Fatalf("expected p2, got %v", val2.Data["password"])
	}

	// Delete all.
	b.Delete(ctx, "s/k", DeleteOptions{})
	_, err = b.Get(ctx, "s/k", nil)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSecretBackend_CAS(t *testing.T) {
	b := NewMemoryBackend()
	ctx := context.Background()

	b.Set(ctx, "cas/k", map[string]any{"k": "v1"}, SetOptions{})

	// CAS with wrong version.
	_, err := b.Set(ctx, "cas/k", map[string]any{"k": "v2"}, SetOptions{CAS: 99})
	if err == nil {
		t.Fatal("expected CAS mismatch")
	}

	// CAS with correct version.
	ver, err := b.Set(ctx, "cas/k", map[string]any{"k": "v2"}, SetOptions{CAS: 1})
	if err != nil {
		t.Fatalf("CAS set: %v", err)
	}
	if ver.Version != 2 {
		t.Fatalf("expected version 2, got %d", ver.Version)
	}
}

func TestSecretBackend_List(t *testing.T) {
	b := NewMemoryBackend()
	ctx := context.Background()

	b.Set(ctx, "c/a/k1", map[string]any{"k": "1"}, SetOptions{})
	b.Set(ctx, "c/a/k2", map[string]any{"k": "2"}, SetOptions{})
	b.Set(ctx, "c/b/k1", map[string]any{"k": "3"}, SetOptions{})

	paths, _ := b.List(ctx, ListOptions{})
	if len(paths) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(paths), paths)
	}

	paths, _ = b.List(ctx, ListOptions{Prefix: "c/a"})
	if len(paths) != 2 {
		t.Fatalf("expected 2, got %d", len(paths))
	}

	paths, _ = b.List(ctx, ListOptions{Limit: 1})
	if len(paths) != 1 {
		t.Fatalf("expected 1, got %d", len(paths))
	}
}

func TestSecretBackend_Rotate(t *testing.T) {
	b := NewMemoryBackend()
	ctx := context.Background()

	b.Set(ctx, "r/k", map[string]any{"k": "v1"}, SetOptions{})

	ver, err := b.Rotate(ctx, "r/k", RotateOptions{NewData: map[string]any{"k": "v2"}})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if ver.Version != 2 {
		t.Fatalf("expected version 2, got %d", ver.Version)
	}

	ver, err = b.Rotate(ctx, "r/k", RotateOptions{})
	if err != nil {
		t.Fatalf("Rotate re-issue: %v", err)
	}
	if ver.Version != 3 {
		t.Fatalf("expected version 3, got %d", ver.Version)
	}
}

func TestSecretBackend_Metadata(t *testing.T) {
	b := NewMemoryBackend()
	ctx := context.Background()

	b.Set(ctx, "m/k", map[string]any{"k": "v"}, SetOptions{})

	meta, err := b.Metadata(ctx, "m/k")
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta.Version != 1 {
		t.Fatalf("expected version 1, got %d", meta.Version)
	}

	_, err = b.Metadata(ctx, "m/missing")
	if err == nil {
		t.Fatal("expected error for missing")
	}
}

func TestSecretBackend_Concurrent(t *testing.T) {
	b := NewMemoryBackend()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := fmt.Sprintf("c/%d", idx%5)
			b.Set(ctx, p, map[string]any{"i": idx}, SetOptions{})
			b.Get(ctx, p, nil)
			b.List(ctx, ListOptions{Prefix: "c"})
		}(i)
	}
	wg.Wait()
}

func TestBackendRegistry(t *testing.T) {
	r := NewBackendRegistry()
	mem := NewMemoryBackend()
	r.Register("mem", mem)

	b, ok := r.Get("mem")
	if !ok || b == nil {
		t.Fatal("expected to find mem")
	}

	_, ok = r.Get("nope")
	if ok {
		t.Fatal("expected not found")
	}

	names := r.List()
	sort.Strings(names)
	if len(names) != 1 || names[0] != "mem" {
		t.Fatalf("expected [mem], got %v", names)
	}

	// Overwrite existing registration (no error, just replaces).
	r.Register("mem", mem)

	r.Unregister("mem")
	_, ok = r.Get("mem")
	if ok {
		t.Fatal("expected not found after unregister")
	}
}

func TestBackendRegistry_CloseAll(t *testing.T) {
	r := NewBackendRegistry()
	r.Register("a", NewMemoryBackend())
	r.Register("b", NewMemoryBackend())
	// CloseAll not on BackendRegistry — close each manually.
	for _, name := range r.List() {
		b, ok := r.Get(name)
		if ok {
			b.Close(context.Background())
		}
	}
}

func TestBackendRegistry_ListEmpty(t *testing.T) {
	r := NewBackendRegistry()
	names := r.List()
	if len(names) != 0 {
		t.Fatalf("expected empty list, got %v", names)
	}
}
