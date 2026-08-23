package gates

import (
	"context"
	"testing"
)

func TestSecretScanDetectsSecrets(t *testing.T) {
	path := writeTestFile(t, "config.go", "package config\nvar key = \"AKIAIOSFODNN7EXAMPLQ\"\npassword = \"correct-horse-battery\"\n")
	findings, err := NewSecretScan().Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].Line != 2 || findings[0].Rule != "aws-access-key" {
		t.Fatalf("first finding = %#v", findings[0])
	}
}

func TestSecretScanIgnoresPlaceholders(t *testing.T) {
	path := writeTestFile(t, "config.py", "api_key = \"your_api_key_here\"\ntoken = os.getenv(\"AUTH_TOKEN\")\n")
	findings, err := NewSecretScan().Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestSecretScanReportsMissingFile(t *testing.T) {
	if _, err := NewSecretScan().Check(context.Background(), []string{"missing.txt"}); err == nil {
		t.Fatal("expected missing file error")
	}
}
