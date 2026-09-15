package gates

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/openagentplatform/openagentplatform/gate"
)

// countByRule tallies findings per rule id so assertions read as "this rule
// fired N times" rather than depending on finding order.
func countByRule(findings []gate.Finding) map[string]int {
	counts := make(map[string]int, len(findings))
	for _, f := range findings {
		counts[f.Rule]++
	}
	return counts
}

// TestGateRunnerIntegration runs real Gate implementations against a temp tree
// to verify the runner collects findings in both Sequential and Parallel modes.
func TestGateRunnerIntegration(t *testing.T) {
	dir := t.TempDir()

	// A filesystem fixture with one registry violation: an HTTP handler
	// calling fmt.Println is OAP-002, an error-severity rule.
	handler := filepath.Join(dir, "handler.go")
	handlerSrc := `package handler

import "fmt"

func Handle() {
	fmt.Println("debug line")
}
`
	if err := os.WriteFile(handler, []byte(handlerSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Valid and invalid JSON, plus YAML with a tab-indent error, for SchemaScan.
	validJSON := filepath.Join(dir, "good.json")
	if err := os.WriteFile(validJSON, []byte(`{"name": "test", "value": 42}`), 0o644); err != nil {
		t.Fatal(err)
	}
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte(`{"name": "test", value: }`), 0o644); err != nil {
		t.Fatal(err)
	}
	validYAML := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(validYAML, []byte("name: test\nvalue: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badYAML := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badYAML, []byte("name:\n\tvalue: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ruleScan, schemaScan := testScanners(t)
	// The registry is authored for the JS/Python scanners, whose regex dialect
	// is a superset of Go's RE2. Whatever this gate could not compile must be
	// visible, not silently missing from the scan.
	for _, skipped := range ruleScan.Skipped() {
		t.Logf("rule %s not enforced by this gate: %s", skipped.RuleID, skipped.Reason)
	}
	allPaths := []string{handler, validJSON, badJSON, validYAML, badYAML}
	schemaPaths := []string{validJSON, badJSON, validYAML, badYAML}

	t.Run("sequential", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, ruleScan, schemaScan)
		results, err := runner.Run(context.Background(), allPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		rules := results[0]
		if rules.Gate != "rule-scan" {
			t.Errorf("result[0].Gate = %q, want %q", rules.Gate, "rule-scan")
		}
		if rules.Err != nil {
			t.Errorf("rule-scan error: %v", rules.Err)
		}
		if count := countByRule(rules.Findings)["OAP-002"]; count == 0 {
			t.Errorf("rule-scan: expected an OAP-002 finding, got %+v", rules.Findings)
		}

		schema := results[1]
		if schema.Gate != "schema" {
			t.Errorf("result[1].Gate = %q, want %q", schema.Gate, "schema")
		}
		if schema.Err != nil {
			t.Errorf("schema error: %v", schema.Err)
		}
		ruleIDs := extractRules(schema.Findings)
		if !containsStr(ruleIDs, "invalid-json") {
			t.Errorf("schema: expected invalid-json finding, got rules: %v", ruleIDs)
		}
		if !containsStr(ruleIDs, "yaml-tab") {
			t.Errorf("schema: expected yaml-tab finding, got rules: %v", ruleIDs)
		}
	})

	t.Run("parallel", func(t *testing.T) {
		runner := gate.NewRunner(gate.Parallel, ruleScan, schemaScan)
		results, err := runner.Run(context.Background(), allPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		// Gate order in results is preserved even in parallel mode.
		if results[0].Gate != "rule-scan" || results[1].Gate != "schema" {
			t.Errorf("unexpected result order: %v", []string{results[0].Gate, results[1].Gate})
		}
		if count := countByRule(results[0].Findings)["OAP-002"]; count == 0 {
			t.Errorf("parallel rule-scan: expected an OAP-002 finding, got %+v", results[0].Findings)
		}
	})

	t.Run("schema-only", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, schemaScan)
		results, err := runner.Run(context.Background(), schemaPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		ruleIDs := extractRules(results[0].Findings)
		if !containsStr(ruleIDs, "invalid-json") {
			t.Errorf("expected invalid-json finding, got rules: %v", ruleIDs)
		}
		// good.json and good.yaml contribute nothing; bad.json and bad.yaml
		// contribute exactly one finding each.
		if len(results[0].Findings) != 2 {
			t.Errorf("schema: expected 2 findings (bad.json + bad.yaml), got %d: %+v",
				len(results[0].Findings), results[0].Findings)
		}
	})

	t.Run("empty-paths", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, ruleScan, schemaScan)
		results, err := runner.Run(context.Background(), nil)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		for _, r := range results {
			if len(r.Findings) != 0 {
				t.Errorf("gate %s: expected 0 findings on empty paths, got %d", r.Gate, len(r.Findings))
			}
		}
	})
}

func extractRules(findings []gate.Finding) []string {
	rules := make([]string, len(findings))
	for i, f := range findings {
		rules[i] = f.Rule
	}
	sort.Strings(rules)
	return rules
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}