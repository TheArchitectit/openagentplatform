package gates

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/openagentplatform/openagentplatform/gate"
)

// TestGateRunnerIntegration uses real Gate implementations (SecretScan,
// SchemaScan) against temp files to verify the runner correctly collects
// findings in both Sequential and Parallel modes.
func TestGateRunnerIntegration(t *testing.T) {
	dir := t.TempDir()

	// Write a file with an AWS access key pattern (AKIA + 16 uppercase alphanumeric chars).
	secretFile := filepath.Join(dir, "deploy.sh")
	secretContent := `#!/bin/bash
export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EFGH123
echo "deploying"
`
	if err := os.WriteFile(secretFile, []byte(secretContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a file with a GitHub token.
	tokenFile := filepath.Join(dir, "config.env")
	tokenContent := `DATABASE_URL=postgres://localhost/mydb
GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234
`
	if err := os.WriteFile(tokenFile, []byte(tokenContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write valid JSON.
	validJSON := filepath.Join(dir, "good.json")
	if err := os.WriteFile(validJSON, []byte(`{"name": "test", "value": 42}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write invalid JSON.
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte(`{"name": "test", value: }`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write valid YAML.
	validYAML := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(validYAML, []byte("name: test\nvalue: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write YAML with tab indentation (invalid).
	badYAML := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badYAML, []byte("name:\n\tvalue: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	secretGate := NewSecretScan()
	schemaGate := NewSchemaScan()

	allPaths := []string{secretFile, tokenFile, validJSON, badJSON, validYAML, badYAML}
	secretPaths := []string{secretFile, tokenFile}
	schemaPaths := []string{validJSON, badJSON, validYAML, badYAML}

	t.Run("sequential", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, secretGate, schemaGate)
		results, err := runner.Run(context.Background(), allPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		// SecretScan results.
		sec := results[0]
		if sec.Gate != "secret-scan" {
			t.Errorf("result[0].Gate = %q, want %q", sec.Gate, "secret-scan")
		}
		if sec.Err != nil {
			t.Errorf("secret-scan error: %v", sec.Err)
		}
		// deploy.sh has AWS key, config.env has GitHub token.
		if len(sec.Findings) < 2 {
			t.Errorf("secret-scan: expected >= 2 findings, got %d: %+v", len(sec.Findings), sec.Findings)
		}

		// SchemaScan results.
		sch := results[1]
		if sch.Gate != "schema" {
			t.Errorf("result[1].Gate = %q, want %q", sch.Gate, "schema")
		}
		if sch.Err != nil {
			t.Errorf("schema error: %v", sch.Err)
		}
		// bad.json has invalid JSON, bad.yaml has tab indentation.
		rules := extractRules(sch.Findings)
		if !containsStr(rules, "invalid-json") {
			t.Errorf("schema: expected invalid-json finding, got rules: %v", rules)
		}
		if !containsStr(rules, "yaml-tab") {
			t.Errorf("schema: expected yaml-tab finding, got rules: %v", rules)
		}
	})

	t.Run("parallel", func(t *testing.T) {
		runner := gate.NewRunner(gate.Parallel, secretGate, schemaGate)
		results, err := runner.Run(context.Background(), allPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		// Gate order in results is preserved even in parallel mode.
		if results[0].Gate != "secret-scan" || results[1].Gate != "schema" {
			t.Errorf("unexpected result order: %v", []string{results[0].Gate, results[1].Gate})
		}
		if len(results[0].Findings) < 2 {
			t.Errorf("parallel secret-scan: expected >= 2 findings, got %d", len(results[0].Findings))
		}
	})

	t.Run("secret-only", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, secretGate)
		results, err := runner.Run(context.Background(), secretPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		rules := extractRules(results[0].Findings)
		if !containsStr(rules, "aws-access-key") {
			t.Errorf("expected aws-access-key finding, got rules: %v", rules)
		}
		if !containsStr(rules, "github-token") {
			t.Errorf("expected github-token finding, got rules: %v", rules)
		}
	})

	t.Run("schema-only", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, schemaGate)
		results, err := runner.Run(context.Background(), schemaPaths)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		rules := extractRules(results[0].Findings)
		if !containsStr(rules, "invalid-json") {
			t.Errorf("expected invalid-json finding, got rules: %v", rules)
		}
		// valid.json and valid.yaml should produce no findings.
		if len(results[0].Findings) != 2 {
			t.Errorf("schema: expected 2 findings (bad.json + bad.yaml), got %d: %+v",
				len(results[0].Findings), results[0].Findings)
		}
	})

	t.Run("empty-paths", func(t *testing.T) {
		runner := gate.NewRunner(gate.Sequential, secretGate, schemaGate)
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
