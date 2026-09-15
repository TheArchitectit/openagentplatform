package gates

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Rule is one entry from a DevGate prevention-rules registry file. Unknown
// fields are ignored; the struct mirrors the subset the gates act on.
//
// Resolution is per pattern-rules.schema.json: an entry with enabled=false is
// inert, and severity decides whether the entry blocks.
type Rule struct {
	RuleID           string   `json:"rule_id"`
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	Pattern          string   `json:"pattern"`
	ForbiddenContext *string  `json:"forbidden_context"`
	Message          string   `json:"message"`
	Severity         string   `json:"severity"`
	FileGlob         []string `json:"file_glob"`
	ExcludeGlob      []string `json:"exclude_glob"`
	ScanComments     bool     `json:"scan_comments"`
	Suggestion       string   `json:"suggestion"`
}

// Blocking reports whether a finding from this rule should fail a gate.
// warning and info are advisory; critical and error block.
func (r Rule) Blocking() bool {
	return r.Severity == "critical" || r.Severity == "error"
}

type ruleFile struct {
	Version string `json:"version"`
	Rules   []Rule `json:"rules"`
}

// RuleSet is a resolved, ordered registry: DevGate's bundled baseline with the
// project's overlay merged on top.
type RuleSet struct {
	Rules      []Rule
	DevGateDir string
	ProjectDir string
}

// DefaultRulePaths returns the bundled-baseline and project-overlay rule
// directories for a project laid out as <project>/.devgate/. projectRoot is
// the directory that contains .devgate.
func DefaultRulePaths(projectRoot string) (devgateDir, projectDir string) {
	return filepath.Join(projectRoot, ".devgate", ".guardrails", "prevention-rules"),
		filepath.Join(projectRoot, ".guardrails", "prevention-rules")
}

// LoadRuleSet reads pattern-rules.json from both directories and merges them by
// rule_id: an overlay entry replaces the same-id baseline entry in place (so a
// project can retune a baseline rule's severity or pattern without editing the
// submodule), and new overlay ids append.
//
// This mirrors .devgate/scripts/guardrails-scan.mjs and gate_overlay.py. A
// missing overlay file is not an error — the baseline alone is a valid
// registry. A missing baseline IS an error: silently scanning with no rules
// would report a clean run that checked nothing.
func LoadRuleSet(devgateDir, projectDir string) (*RuleSet, error) {
	base, err := loadRuleFile(filepath.Join(devgateDir, "pattern-rules.json"))
	if err != nil {
		return nil, fmt.Errorf("load bundled rules: %w", err)
	}
	if len(base) == 0 {
		return nil, fmt.Errorf("no bundled rules in %s", devgateDir)
	}

	var overlay []Rule
	if _, statErr := os.Stat(filepath.Join(projectDir, "pattern-rules.json")); statErr == nil {
		overlay, err = loadRuleFile(filepath.Join(projectDir, "pattern-rules.json"))
		if err != nil {
			return nil, fmt.Errorf("load overlay rules: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat overlay rules: %w", statErr)
	}

	return &RuleSet{
		Rules:      mergeRules(base, overlay),
		DevGateDir: devgateDir,
		ProjectDir: projectDir,
	}, nil
}

func loadRuleFile(path string) ([]Rule, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed ruleFile
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parsed.Rules, nil
}

// mergeRules keeps baseline order, replacing in place on an id collision and
// appending ids the baseline does not have.
func mergeRules(baseline, overlay []Rule) []Rule {
	merged := make([]Rule, len(baseline))
	copy(merged, baseline)

	index := make(map[string]int, len(baseline))
	for i, rule := range merged {
		if rule.RuleID != "" {
			index[rule.RuleID] = i
		}
	}
	for _, rule := range overlay {
		if at, ok := index[rule.RuleID]; ok && rule.RuleID != "" {
			merged[at] = rule
			continue
		}
		if rule.RuleID != "" {
			index[rule.RuleID] = len(merged)
		}
		merged = append(merged, rule)
	}
	return merged
}

// Active returns the enabled rules that fire at the requested severities.
func (s *RuleSet) Active(severities ...string) []Rule {
	wanted := make(map[string]bool, len(severities))
	for _, severity := range severities {
		wanted[severity] = true
	}
	var active []Rule
	for _, rule := range s.Rules {
		if !rule.Enabled || !wanted[rule.Severity] {
			continue
		}
		active = append(active, rule)
	}
	return active
}