package secrets

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
)

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
