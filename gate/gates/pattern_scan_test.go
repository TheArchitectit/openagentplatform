package gates

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPatternScanDetectsMarkers(t *testing.T) {
	path := writeTestFile(t, "main.go", "package main\n// TODO: ship this\n// FIXME repair\n// HACK workaround\n")
	findings, err := NewPatternScan().Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[2].Rule != "hack" || findings[2].Line != 4 {
		t.Fatalf("last finding = %#v", findings[2])
	}
}

func TestPatternScanSkipsTestsAndHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("// TODO"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("# FIXME"), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, err := NewPatternScan().Check(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}
