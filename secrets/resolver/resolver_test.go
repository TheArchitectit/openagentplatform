package resolver

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/secrets"
)

func TestParseURI_Basic(t *testing.T) {
	ref, err := ParseURI("ref:oap://vault/myworkspace/secret/data/api-key")
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if ref.BackendType != "vault" {
		t.Fatalf("expected vault, got %s", ref.BackendType)
	}
	if ref.WorkspaceID != "myworkspace" {
		t.Fatalf("expected myworkspace, got %s", ref.WorkspaceID)
	}
	if ref.Path != "secret/data/api-key" {
		t.Fatalf("expected secret/data/api-key, got %s", ref.Path)
	}
	if ref.Version != nil {
		t.Fatalf("expected nil version, got %v", ref.Version)
	}
	if ref.Key != "" {
		t.Fatalf("expected empty key, got %s", ref.Key)
	}
}

func TestParseURI_WithQueryParams(t *testing.T) {
	ref, err := ParseURI("ref:oap://vault/ws1/secret/data/pass?key=password")
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if ref.Key != "password" {
		t.Fatalf("expected key=password, got %s", ref.Key)
	}
}

func TestParseURI_InvalidScheme(t *testing.T) {
	_, err := ParseURI("https://vault/ws/path")
	if err == nil {
		t.Fatal("expected error for invalid scheme")
	}
}

func TestParseURI_MissingPath(t *testing.T) {
	_, err := ParseURI("ref:oap://vault")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestParseURI_MinimalValid(t *testing.T) {
	ref, err := ParseURI("ref:oap://mem/workspace1/secret-path")
	if err != nil {
		t.Fatalf("ParseURI minimal: %v", err)
	}
	if ref.BackendType != "mem" || ref.WorkspaceID != "workspace1" || ref.Path != "secret-path" {
		t.Fatalf("unexpected parsed ref: %+v", ref)
	}
}

func TestCacheKey(t *testing.T) {
	v := 2
	ref := ParsedRef{BackendType: "mem", WorkspaceID: "ws", Path: "p", Version: &v}
	key := CacheKey(ref)
	if key != "mem:ws:p:2" {
		t.Fatalf("expected mem:ws:p:2, got %s", key)
	}
}

func TestCacheKey_NilVersion(t *testing.T) {
	ref := ParsedRef{BackendType: "mem", WorkspaceID: "ws", Path: "p"}
	key := CacheKey(ref)
	if key != "mem:ws:p:-1" {
		t.Fatalf("expected mem:ws:p:-1, got %s", key)
	}
}

func TestLRUCache_GetPut(t *testing.T) {
	c := NewLRUCache(10)

	val := &secrets.SecretValue{Path: "p", Data: map[string]any{"k": "v"}}
	c.Put("key1", val)

	got, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Data["k"] != "v" {
		t.Fatalf("unexpected data: %v", got.Data)
	}

	_, ok = c.Get("missing")
	if ok {
		t.Fatal("expected miss")
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	c := NewLRUCache(2)

	c.Put("a", &secrets.SecretValue{Path: "a"})
	c.Put("b", &secrets.SecretValue{Path: "b"})
	c.Put("c", &secrets.SecretValue{Path: "c"}) // evicts "a"

	_, ok := c.Get("a")
	if ok {
		t.Fatal("expected 'a' to be evicted")
	}

	_, ok = c.Get("b")
	if !ok {
		t.Fatal("expected 'b' to still exist")
	}

	_, ok = c.Get("c")
	if !ok {
		t.Fatal("expected 'c' to still exist")
	}
}

func TestLRUCache_Update(t *testing.T) {
	c := NewLRUCache(2)

	c.Put("a", &secrets.SecretValue{Path: "a", Data: map[string]any{"v": 1}})
	c.Put("a", &secrets.SecretValue{Path: "a", Data: map[string]any{"v": 2}})
	c.Put("b", &secrets.SecretValue{Path: "b"})

	got, _ := c.Get("a")
	if got.Data["v"] != 2 {
		t.Fatalf("expected updated value 2, got %v", got.Data["v"])
	}
}

func TestLRUCache_Invalidate(t *testing.T) {
	c := NewLRUCache(10)

	c.Put("mem:ws:secret/a:1", &secrets.SecretValue{Path: "secret/a"})
	c.Put("mem:ws:secret/b:1", &secrets.SecretValue{Path: "secret/b"})
	c.Put("mem:ws:other/x:1", &secrets.SecretValue{Path: "other/x"})

	c.Invalidate("secret/a")

	_, ok := c.Get("mem:ws:secret/a:1")
	if ok {
		t.Fatal("expected secret/a to be invalidated")
	}

	_, ok = c.Get("mem:ws:secret/b:1")
	if !ok {
		t.Fatal("expected secret/b to still exist")
	}

	_, ok = c.Get("mem:ws:other/x:1")
	if !ok {
		t.Fatal("expected other/x to still exist")
	}
}

func TestLRUCache_ZeroMax(t *testing.T) {
	c := NewLRUCache(0)
	if c.max != DefaultCacheMaxEntries {
		t.Fatalf("expected default max, got %d", c.max)
	}
}

func TestInjectWorkspaceVariables(t *testing.T) {
	uri := "ref:oap://vault/{{workspace}}/secret?key={{name}}"
	got := InjectWorkspaceVariables(uri, map[string]string{
		"workspace": "ws1",
		"name":      "api-key",
	})
	expected := "ref:oap://vault/ws1/secret?key=api-key"
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestAuthorizer_CanAccess(t *testing.T) {
	a := NewAuthorizer()

	// Client-level path, client matches.
	if !a.CanAccess("clients/acme/creds", &AuthContext{ClientID: "acme"}) {
		t.Fatal("expected client-level access")
	}

	// Client-level path, client mismatch.
	if a.CanAccess("clients/acme/creds", &AuthContext{ClientID: "bob"}) {
		t.Fatal("expected denial for wrong client")
	}

	// Site-level path, site matches.
	if !a.CanAccess("clients/acme/sites/site1/creds", &AuthContext{ClientID: "acme", SiteID: "site1"}) {
		t.Fatal("expected site-level access")
	}

	// Site-level path, site mismatch.
	if a.CanAccess("clients/acme/sites/site1/creds", &AuthContext{ClientID: "acme", SiteID: "other"}) {
		t.Fatal("expected denial for wrong site")
	}

	// Agent-level path, agent matches.
	if !a.CanAccess("clients/acme/sites/site1/agents/agent1/creds", &AuthContext{ClientID: "acme", SiteID: "site1", AgentID: "agent1"}) {
		t.Fatal("expected agent-level access")
	}

	// Agent-level path, agent mismatch.
	if a.CanAccess("clients/acme/sites/site1/agents/agent1/creds", &AuthContext{ClientID: "acme", SiteID: "site1", AgentID: "other"}) {
		t.Fatal("expected denial for wrong agent")
	}

	// Nil context.
	if a.CanAccess("clients/acme/creds", nil) {
		t.Fatal("expected denial for nil context")
	}
}

func TestNewResolver_NilDeps(t *testing.T) {
	// New with nil registry should not panic.
	r := New(nil, nil, nil)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
}

func TestResolver_Backends_Empty(t *testing.T) {
	r := New(nil, nil, nil)
	names := r.Backends()
	if len(names) != 0 {
		t.Fatalf("expected empty, got %v", names)
	}
}

func TestResolver_BackendFor_Nil(t *testing.T) {
	r := New(nil, nil, nil)
	_, ok := r.BackendFor("anything")
	if ok {
		t.Fatal("expected false for nil registry")
	}
}
