package secrets

import (
	"context"
	"database/sql"
	"fmt"
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

func (r *mockResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }
func (r *mockResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

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
