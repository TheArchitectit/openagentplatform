package gates

import (
	"context"
	"testing"
)

func TestSemanticScanDetectsEmptyAndUnreachableCode(t *testing.T) {
	path := writeTestFile(t, "sample.go", `package sample
func Empty() {}
func Broken() int {
	return 1
	println("never")
}
`)
	findings, err := NewSemanticScan().Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v", findings)
	}
	rules := map[string]bool{}
	for _, finding := range findings {
		rules[finding.Rule] = true
	}
	if !rules["empty-block"] || !rules["unreachable-code"] {
		t.Fatalf("rules = %v", rules)
	}
}

func TestSemanticScanAcceptsValidSource(t *testing.T) {
	path := writeTestFile(t, "sample.go", "package sample\nfunc Value() int { return 1 }\n")
	findings, err := NewSemanticScan().Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestSemanticScanReportsSyntaxErrors(t *testing.T) {
	path := writeTestFile(t, "sample.go", "package sample\nfunc Broken( {\n")
	if _, err := NewSemanticScan().Check(context.Background(), []string{path}); err == nil {
		t.Fatal("expected parse error")
	}
}
