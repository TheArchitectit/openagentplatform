package gates

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

// testProjectRoot returns the repository root — the directory containing
// .devgate and .guardrails. The RuleScan tests need the real registry rather
// than a fixture, because the point of the gate is that it consumes the same
// rule files CI does; a fixture would let the two drift apart silently.
func testProjectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devgate", ".guardrails", "prevention-rules")); err != nil {
		t.Skipf("DevGate submodule not checked out at %s: %v", root, err)
	}
	return root
}

// testScanners builds a RuleScan over the real registry plus a SchemaScan.
func testScanners(t *testing.T) (*RuleScan, *SchemaScan) {
	t.Helper()
	projectRoot := testProjectRoot(t)
	ruleSet, err := LoadRuleSet(DefaultRulePaths(projectRoot))
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}
	ruleScan, err := NewRuleScan(ruleSet, projectRoot)
	if err != nil {
		t.Fatalf("NewRuleScan: %v", err)
	}
	return ruleScan, NewSchemaScan()
}
