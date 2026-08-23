package gates

import (
	"context"
	"testing"
)

func TestSchemaScanValidatesJSON(t *testing.T) {
	valid := writeTestFile(t, "valid.json", `{"name":"devgate"}`)
	invalid := writeTestFile(t, "invalid.json", "{\n  \"name\": true,\n}\n")
	findings, err := NewSchemaScan().Check(context.Background(), []string{valid, invalid})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 1 || findings[0].Rule != "invalid-json" || findings[0].Line != 3 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestSchemaScanValidatesYAMLStructure(t *testing.T) {
	valid := writeTestFile(t, "valid.yaml", "name: devgate\ngates:\n  enabled: true\n")
	invalid := writeTestFile(t, "invalid.yml", "name: devgate\n\tbad: value\nbroken-value\n")
	findings, err := NewSchemaScan().Check(context.Background(), []string{valid, invalid})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].Rule != "yaml-tab" || findings[1].Rule != "yaml-structure" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestSchemaScanIgnoresUnsupportedFiles(t *testing.T) {
	path := writeTestFile(t, "notes.txt", "not: [validated")
	findings, err := NewSchemaScan().Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}
