package gates

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRulesFile writes a pattern-rules.json into dir (creating it).
func writeRulesFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pattern-rules.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ruleIDs(rules []Rule) []string {
	ids := make([]string, len(rules))
	for i, r := range rules {
		ids[i] = r.RuleID
	}
	return ids
}

func TestLoadRuleSetOverlayReplacesSameID(t *testing.T) {
	root := t.TempDir()
	devgate := filepath.Join(root, ".devgate", "prevention-rules")
	project := filepath.Join(root, ".guardrails", "prevention-rules")

	// Baseline: a retunable rule plus one the overlay does not touch.
	writeRulesFile(t, devgate, `{"version":"1.0.0","rules":[
		{"rule_id":"PREVENT-001","name":"baseline","enabled":true,"pattern":"AAAA","severity":"error","message":"baseline"},
		{"rule_id":"PREVENT-002","name":"untouched","enabled":true,"pattern":"BBBB","severity":"warning","message":"untouched"}
	]}`)
	// Overlay retunes PREVENT-001's severity and adds a new OAP id.
	writeRulesFile(t, project, `{"version":"1.0.0","rules":[
		{"rule_id":"OAP-001","name":"project","enabled":true,"pattern":"CCCC","severity":"critical","message":"project"},
		{"rule_id":"PREVENT-001","name":"retuned","enabled":true,"pattern":"DDDD","severity":"warning","message":"retuned"}
	]}`)

	set, err := LoadRuleSet(devgate, project)
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}

	// Baseline order is preserved: PREVENT-001 keeps its slot, PREVENT-002
	// follows, and the overlay-only id appends.
	want := []string{"PREVENT-001", "PREVENT-002", "OAP-001"}
	if got := ruleIDs(set.Rules); !equalStrings(got, want) {
		t.Fatalf("rule ids = %v, want %v", got, want)
	}
	// The replaced entry is the overlay's, not the baseline's.
	if set.Rules[0].Name != "retuned" || set.Rules[0].Pattern != "DDDD" {
		t.Fatalf("PREVENT-001 = %#v, want the overlay entry", set.Rules[0])
	}
	// The untouched baseline entry survives intact.
	if set.Rules[1].Name != "untouched" {
		t.Fatalf("PREVENT-002 = %#v, want the baseline entry", set.Rules[1])
	}
}

func TestLoadRuleSetWithoutOverlayUsesBaseline(t *testing.T) {
	root := t.TempDir()
	devgate := filepath.Join(root, ".devgate", "prevention-rules")
	writeRulesFile(t, devgate, `{"version":"1.0.0","rules":[
		{"rule_id":"PREVENT-001","enabled":true,"pattern":"A","severity":"error","message":"m"}
	]}`)

	set, err := LoadRuleSet(devgate, filepath.Join(root, ".guardrails", "prevention-rules"))
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}
	if len(set.Rules) != 1 || set.Rules[0].RuleID != "PREVENT-001" {
		t.Fatalf("rules = %#v", set.Rules)
	}
}

func TestLoadRuleSetMissingBaselineIsError(t *testing.T) {
	root := t.TempDir()
	_, err := LoadRuleSet(
		filepath.Join(root, ".devgate", "prevention-rules"),
		filepath.Join(root, ".guardrails", "prevention-rules"),
	)
	if err == nil {
		t.Fatal("expected an error when the bundled baseline is absent")
	}
}

func TestActiveFiltersDisabledAndUnwantedSeverities(t *testing.T) {
	set := &RuleSet{Rules: []Rule{
		{RuleID: "A", Enabled: true, Severity: "error"},
		{RuleID: "B", Enabled: false, Severity: "error"},
		{RuleID: "C", Enabled: true, Severity: "info"},
		{RuleID: "D", Enabled: true, Severity: "critical"},
	}}
	// An info rule is dropped here even though it is enabled: the caller asked
	// only for blocking severities.
	if got := ruleIDs(set.Active("error", "critical")); !equalStrings(got, []string{"A", "D"}) {
		t.Fatalf("Active = %v, want [A D]", got)
	}
}

func TestRuleBlockingSeverity(t *testing.T) {
	cases := map[string]bool{
		"critical": true, "error": true, "warning": false, "info": false,
	}
	for severity, want := range cases {
		if got := (Rule{Severity: severity}).Blocking(); got != want {
			t.Errorf("Blocking(%q) = %v, want %v", severity, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}