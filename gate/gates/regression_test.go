package gates

import (
	"context"
	"testing"

	"github.com/openagentplatform/openagentplatform/gate"
)

func TestRegressionScanDetectsKnownPattern(t *testing.T) {
	scan, err := NewRegressionScan([]RegressionPattern{{
		ID: "REG-1", Message: "unsafe parser returned", Severity: gate.SeverityCritical, Pattern: `JSON\.parse\([^)]*\)\.[A-Za-z_]`,
	}})
	if err != nil {
		t.Fatalf("NewRegressionScan returned error: %v", err)
	}
	path := writeTestFile(t, "app.js", "const value = JSON.parse(raw).name;\n")
	findings, err := scan.Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 1 || findings[0].Rule != "REG-1" || findings[0].Severity != gate.SeverityCritical {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestRegressionScanRejectsInvalidPattern(t *testing.T) {
	if _, err := NewRegressionScan([]RegressionPattern{{Pattern: "["}}); err == nil {
		t.Fatal("expected invalid regular expression error")
	}
}

func TestRegressionScanUsesDefaultSeverity(t *testing.T) {
	scan, err := NewRegressionScan([]RegressionPattern{{ID: "REG-2", Message: "bad", Pattern: `bad\(`}})
	if err != nil {
		t.Fatal(err)
	}
	path := writeTestFile(t, "app.go", "bad()\n")
	findings, err := scan.Check(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Severity != gate.SeverityError {
		t.Fatalf("findings = %#v", findings)
	}
}
