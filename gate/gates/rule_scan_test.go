package gates

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newFixtureScan builds a RuleScan over a synthetic registry so the scan
// semantics can be asserted without depending on which rules the real
// registry happens to carry.
func newFixtureScan(t *testing.T, projectRoot, rulesJSON string) *RuleScan {
	t.Helper()
	devgate := filepath.Join(projectRoot, ".devgate", "prevention-rules")
	writeRulesFile(t, devgate, rulesJSON)
	set, err := LoadRuleSet(devgate, filepath.Join(projectRoot, ".guardrails", "prevention-rules"))
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}
	scan, err := NewRuleScan(set, projectRoot)
	if err != nil {
		t.Fatalf("NewRuleScan: %v", err)
	}
	return scan
}

func TestRuleScanAppliesFileAndExcludeGlobs(t *testing.T) {
	root := t.TempDir()
	scan := newFixtureScan(t, root, `{"version":"1.0.0","rules":[
		{"rule_id":"R-GO","enabled":true,"pattern":"BADTHING","severity":"error","message":"go only",
		 "file_glob":["*.go"],"exclude_glob":["*_test.go"]}
	]}`)

	goFile := filepath.Join(root, "app.go")
	if err := os.WriteFile(goFile, []byte("BADTHING\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tsFile := filepath.Join(root, "app.ts")
	if err := os.WriteFile(tsFile, []byte("BADTHING\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(root, "app_test.go")
	if err := os.WriteFile(testFile, []byte("BADTHING\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.Check(context.Background(), []string{goFile, tsFile, testFile})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// Only app.go qualifies: the *.go glob excludes app.ts, and exclude_glob
	// (plus the test-file skip) drops app_test.go.
	if len(findings) != 1 || filepath.Base(findings[0].Path) != "app.go" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestRuleScanForbiddenContextSuppresses(t *testing.T) {
	root := t.TempDir()
	scan := newFixtureScan(t, root, `{"version":"1.0.0","rules":[
		{"rule_id":"R-CTX","enabled":true,"pattern":"SECRETWORD","severity":"critical","message":"ctx",
		 "forbidden_context":"allow-me"}
	]}`)

	hit := filepath.Join(root, "hit.go")
	if err := os.WriteFile(hit, []byte("var x = SECRETWORD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Same pattern, but the line carries the documented safe context.
	safe := filepath.Join(root, "safe.go")
	if err := os.WriteFile(safe, []byte("var y = SECRETWORD // allow-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.Check(context.Background(), []string{hit, safe})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(findings) != 1 || filepath.Base(findings[0].Path) != "hit.go" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestRuleScanHonorsInlineAllowAnnotation(t *testing.T) {
	root := t.TempDir()
	scan := newFixtureScan(t, root, `{"version":"1.0.0","rules":[
		{"rule_id":"R-ALLOW","enabled":true,"pattern":"NAUGHTY","severity":"error","message":"allow"}
	]}`)

	// The annotation is matched by rule id, so a different id must not suppress.
	allowed := filepath.Join(root, "allowed.go")
	if err := os.WriteFile(allowed, []byte("NAUGHTY // guardrails-allow R-ALLOW: deliberate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrongID := filepath.Join(root, "wrong.go")
	if err := os.WriteFile(wrongID, []byte("NAUGHTY // guardrails-allow R-OTHER: not this rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.Check(context.Background(), []string{allowed, wrongID})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(findings) != 1 || filepath.Base(findings[0].Path) != "wrong.go" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestRuleScanSkipsCommentsUnlessOptedIn(t *testing.T) {
	root := t.TempDir()
	scan := newFixtureScan(t, root, `{"version":"1.0.0","rules":[
		{"rule_id":"R-QUIET","enabled":true,"pattern":"MARKER","severity":"warning","message":"no comments"},
		{"rule_id":"R-LOUD","enabled":true,"pattern":"TICKETLESS","severity":"warning","message":"comments ok","scan_comments":true}
	]}`)

	path := filepath.Join(root, "notes.go")
	if err := os.WriteFile(path, []byte("// MARKER TICKETLESS\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// R-QUIET skips the comment-only line; R-LOUD opted in and fires.
	counts := countByRule(findings)
	if counts["R-QUIET"] != 0 || counts["R-LOUD"] != 1 {
		t.Fatalf("counts = %#v, findings = %#v", counts, findings)
	}
}

func TestRuleScanReportsUncompilableRules(t *testing.T) {
	root := t.TempDir()
	// RE2 has no lookahead; this is the shape of the registry's PREVENT-003 and
	// PREVENT-009 retunes, which the JS scanner supports and this gate cannot.
	scan := newFixtureScan(t, root, `{"version":"1.0.0","rules":[
		{"rule_id":"R-OK","enabled":true,"pattern":"FINE","severity":"error","message":"ok"},
		{"rule_id":"R-LOOKAHEAD","enabled":true,"pattern":"a(?!b)","severity":"error","message":"needs lookahead"}
	]}`)

	skipped := scan.Skipped()
	if len(skipped) != 1 || skipped[0].RuleID != "R-LOOKAHEAD" {
		t.Fatalf("skipped = %#v", skipped)
	}
	// The compilable rule still runs; one bad pattern must not disable the gate.
	path := filepath.Join(root, "app.go")
	if err := os.WriteFile(path, []byte("FINE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, err := scan.Check(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(findings) != 1 || findings[0].Rule != "R-OK" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestRuleScanExcludesIgnoredPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".guardrailsignore"), []byte("generated/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scan := newFixtureScan(t, root, `{"version":"1.0.0","rules":[
		{"rule_id":"R-ANY","enabled":true,"pattern":"ANYTHING","severity":"error","message":"any"}
	]}`)

	generated := filepath.Join(root, "generated", "out.go")
	if err := os.MkdirAll(filepath.Dir(generated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, []byte("ANYTHING\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(root, "src", "real.go")
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("ANYTHING\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := scan.Check(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(findings) != 1 || filepath.Base(findings[0].Path) != "real.go" {
		t.Fatalf("findings = %#v", findings)
	}
}