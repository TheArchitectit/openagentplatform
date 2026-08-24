package inject

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestEnvInjector(t *testing.T) {
	inj := newEnvInjector()
	rawKey := "test_inject_secret_" + fmt.Sprintf("%d", os.Getpid())
	sanitizedKey := sanitizeKey(rawKey) // uppercased, special chars replaced
	envKey := envPrefix + sanitizedKey
	spec := InjectionSpec{
		Method: MethodEnv,
		Key:    rawKey,
		Value:  []byte("test-value"),
	}

	path, err := inj.inject(spec)
	if err != nil {
		t.Fatalf("env inject: %v", err)
	}
	// envInjector prepends OAP_INJECTED_ and sanitizes the key.
	if path != envKey {
		t.Fatalf("expected path=%s, got %s", envKey, path)
	}
	if os.Getenv(envKey) != "test-value" {
		t.Fatal("env var not set correctly")
	}

	if err := inj.cleanup(spec); err != nil {
		t.Fatalf("env cleanup: %v", err)
	}
	if os.Getenv(envKey) != "" {
		t.Fatal("env var not cleaned up")
	}
}

func TestFileInjector(t *testing.T) {
	inj := newFileInjector()
	spec := InjectionSpec{
		Method:  MethodFile,
		Key:     "test-file",
		Value:   []byte("file-content"),
		Mode:    0o600,
		agentID: "test-agent",
	}

	path, err := inj.inject(spec)
	if err != nil {
		t.Fatalf("file inject: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "file-content" {
		t.Fatalf("expected file-content, got %s", string(data))
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}

	if err := inj.cleanup(spec); err != nil {
		t.Fatalf("file cleanup: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file not cleaned up")
	}
}

func TestStdinInjector(t *testing.T) {
	inj := newStdinInjector()
	spec := InjectionSpec{
		Method:  MethodStdin,
		Key:     "test-stdin",
		Value:   []byte("pipe-data"),
		agentID: "test-agent",
	}

	path, err := inj.inject(spec)
	if err != nil {
		t.Fatalf("stdin inject: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	// Cleanup should not error (even if socket was never connected).
	if err := inj.cleanup(spec); err != nil {
		// Socket may already be gone if Accept failed — that's acceptable.
		t.Logf("stdin cleanup (acceptable): %v", err)
	}
}

func TestInjector_Cleanup(t *testing.T) {
	inj := &Injector{
		env:   newEnvInjector(),
		file:  newFileInjector(),
		stdin: newStdinInjector(),
	}

	spec := InjectionSpec{
		Method:  MethodFile,
		Key:     "cleanup-test",
		Value:   []byte("data"),
		Mode:    0o600,
		agentID: "test-agent",
	}

	path, err := inj.file.inject(spec)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}

	inj.mu.Lock()
	inj.specs = append(inj.specs, spec)
	inj.mu.Unlock()

	inj.Cleanup(context.Background())

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file not cleaned up")
	}
}

func TestInjector_Plan_RequiresResolver(t *testing.T) {
	// Injector with no resolver — Plan will panic on nil deref if r is nil.
	// Verify that creating the injector with nil resolver doesn't panic,
	// but Plan on an empty URI list returns no specs.
	inj := &Injector{
		env:   newEnvInjector(),
		file:  newFileInjector(),
		stdin: newStdinInjector(),
	}

	// Empty URI list should work fine even with nil resolver.
	specs, err := inj.Plan(context.Background(), "agent1", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected 0 specs, got %d", len(specs))
	}
}

func TestInjector_Execute_UnknownMethod(t *testing.T) {
	inj := &Injector{
		env:   newEnvInjector(),
		file:  newFileInjector(),
		stdin: newStdinInjector(),
	}

	results := inj.Execute(context.Background(), []InjectionSpec{
		{Method: "bogus", Key: "test", Value: []byte("v")},
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestInjector_Execute_Env(t *testing.T) {
	inj := &Injector{
		env:   newEnvInjector(),
		file:  newFileInjector(),
		stdin: newStdinInjector(),
	}

	rawKey := "TEST_OAP_INJECT_" + fmt.Sprintf("%d", os.Getpid())
	expectedEnvKey := envPrefix + sanitizeKey(rawKey)
	results := inj.Execute(context.Background(), []InjectionSpec{
		{Method: MethodEnv, Key: rawKey, Value: []byte("hello")},
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("unexpected error: %v", results[0].Err)
	}
	if os.Getenv(expectedEnvKey) != "hello" {
		t.Fatal("env not set")
	}
	os.Unsetenv(expectedEnvKey)
}

func TestInjector_Execute_File(t *testing.T) {
	inj := &Injector{
		env:   newEnvInjector(),
		file:  newFileInjector(),
		stdin: newStdinInjector(),
	}

	results := inj.Execute(context.Background(), []InjectionSpec{
		{Method: MethodFile, Key: "f", Value: []byte("content"), Mode: 0o600, agentID: "a"},
	})
	if results[0].Err != nil {
		t.Fatalf("unexpected error: %v", results[0].Err)
	}
	if results[0].Path == "" {
		t.Fatal("expected non-empty path")
	}
	os.Remove(results[0].Path)
}
