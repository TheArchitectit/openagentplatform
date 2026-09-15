package gates

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openagentplatform/openagentplatform/gate"
)

type compiledRule struct {
	rule     Rule
	pattern  *regexp.Regexp
	context  *regexp.Regexp // forbidden_context, nil when the rule has none
	allow    *regexp.Regexp // inline `guardrails-allow <id>: <reason>` escape
	blocking bool
}

// RuleScan checks source files against a DevGate rule registry. Rules come
// from .devgate/.guardrails/prevention-rules/pattern-rules.json merged with
// the project overlay, so the same retunes that apply in CI apply here.
//
// Unlike SecretScan, which this replaces, the rule set is data: adding a
// pattern does not require touching Go. The one thing the scanner cannot
// express declaratively — the git-index committed-artifact check in
// guardrails-scan.mjs — is not reproduced here.
type RuleScan struct {
	rules        []compiledRule
	skipped      []SkippedRule
	projectRoot  string
	ignore       []string
	testFileSkip bool
}

// SkippedRule records an enabled registry rule this gate could not run,
// together with why. It exists because the registry regexes are authored for
// the JavaScript and Python scanners, whose dialects are supersets of Go's
// RE2: lookahead, lookbehind, and backreferences compile there but not here.
type SkippedRule struct {
	RuleID string
	Reason string
}

// Skipped returns the enabled rules that could not be compiled, in registry
// order. Callers should surface a non-empty result rather than treating the
// gate's silence as coverage — those rules are enforced by the JavaScript
// scanner in CI, not by this gate.
func (s *RuleScan) Skipped() []SkippedRule {
	return append([]SkippedRule(nil), s.skipped...)
}

// NewRuleScan compiles every enabled rule in the registry. projectRoot is the
// directory that contains .devgate and .guardrailsignore.
//
// Rules whose patterns Go's RE2 cannot express are collected in Skipped rather
// than failing the whole gate: the registry is shared with the CI scanners, and
// one dialect-specific pattern must not disable every other rule. A rule that
// compiles is never silently dropped — a nil pattern cannot reach scan.rules.
func NewRuleScan(set *RuleSet, projectRoot string) (*RuleScan, error) {
	ignore, err := LoadIgnorePatterns(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load ignore patterns: %w", err)
	}

	scan := &RuleScan{projectRoot: projectRoot, ignore: ignore, testFileSkip: true}
	for _, rule := range set.Active("critical", "error", "warning", "info") {
		pattern, err := regexp.Compile(rule.Pattern)
		if err != nil {
			scan.skipped = append(scan.skipped, SkippedRule{RuleID: rule.RuleID, Reason: err.Error()})
			continue
		}
		compiled := compiledRule{
			rule:     rule,
			pattern:  pattern,
			allow:    regexp.MustCompile(`guardrails-allow\s+` + regexp.QuoteMeta(rule.RuleID) + `\s*:\s*\S`),
			blocking: rule.Blocking(),
		}
		if rule.ForbiddenContext != nil && *rule.ForbiddenContext != "" {
			context, err := regexp.Compile(*rule.ForbiddenContext)
			if err != nil {
				scan.skipped = append(scan.skipped, SkippedRule{RuleID: rule.RuleID, Reason: "forbidden_context: " + err.Error()})
				continue
			}
			compiled.context = context
		}
		scan.rules = append(scan.rules, compiled)
	}
	if len(scan.rules) == 0 {
		return nil, fmt.Errorf("no enabled rules compiled from %s", set.ProjectDir)
	}
	return scan, nil
}

func (s *RuleScan) Name() string { return "rule-scan" }

// Check reports every rule violation in the supplied paths.
func (s *RuleScan) Check(ctx context.Context, paths []string) ([]gate.Finding, error) {
	files, err := expandPaths(paths)
	if err != nil {
		return nil, err
	}
	var findings []gate.Finding
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		rel := s.relPath(path)
		if IgnoredPath(s.ignore, rel) {
			continue
		}
		testFile := isTestRelPath(rel)
		if err := s.checkFile(path, rel, testFile, &findings); err != nil {
			return findings, err
		}
	}
	return findings, nil
}

func (s *RuleScan) checkFile(path, rel string, testFile bool, findings *[]gate.Finding) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(path))
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		for _, compiled := range s.rules {
			if !ruleAppliesTo(compiled.rule, rel) {
				continue
			}
			// Blocking rules do not apply to test code. A scanner that fires
			// production rules on test fixtures gets waived into
			// meaninglessness, so test files are skipped outright for
			// error/critical rules; style rules still scan them.
			if compiled.blocking && testFile && s.testFileSkip {
				continue
			}
			if !compiled.rule.ScanComments && isCommentLine(line, ext) {
				continue
			}
			if compiled.allow.MatchString(line) {
				continue
			}
			location := compiled.pattern.FindStringIndex(line)
			if location == nil {
				continue
			}
			// forbidden_context suppresses a hit when the same line carries its
			// documented safe usage — line-scoped, not path-scoped.
			if compiled.context != nil && compiled.context.MatchString(line) {
				continue
			}
			*findings = append(*findings, gate.Finding{
				Gate:     s.Name(),
				Path:     path,
				Line:     i + 1,
				Column:   location[0] + 1,
				Severity: gate.Severity(compiled.rule.Severity),
				Message:  compiled.rule.Message,
				Rule:     compiled.rule.RuleID,
			})
		}
	}
	return nil
}

func (s *RuleScan) relPath(path string) string {
	if strings.HasPrefix(path, s.projectRoot+string(filepath.Separator)) {
		return filepath.ToSlash(path[len(s.projectRoot)+1:])
	}
	return filepath.ToSlash(path)
}

// ruleAppliesTo applies file_glob then exclude_glob. Globs are matched against
// both the project-relative path and the basename, so a bare-extension glob
// like "*.go" reaches nested files.
func ruleAppliesTo(rule Rule, rel string) bool {
	if len(rule.FileGlob) > 0 && !globsMatchAny(rule.FileGlob, rel) {
		return false
	}
	if len(rule.ExcludeGlob) > 0 && globsMatchAny(rule.ExcludeGlob, rel) {
		return false
	}
	return true
}

func globsMatchAny(globs []string, rel string) bool {
	base := filepath.Base(rel)
	for _, glob := range globs {
		if globMatch(glob, rel) || globMatch(glob, base) {
			return true
		}
	}
	return false
}

// isTestRelPath mirrors guardrails-scan.mjs isTestFile: test directories,
// _test suffixed source files, and the conftest/test_/.test./.spec. name
// conventions.
func isTestRelPath(rel string) bool {
	base := filepath.Base(rel)
	if strings.HasPrefix(rel, "tests/") || strings.HasPrefix(rel, "test/") ||
		strings.Contains(rel, "/tests/") || strings.Contains(rel, "/test/") {
		return true
	}
	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.rs") || strings.HasSuffix(base, "_test.py") {
		return true
	}
	if strings.HasPrefix(base, "conftest") || strings.HasPrefix(base, "test_") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	return false
}

// isCommentLine reports whether a line is comment-only for the given file
// extension. Full-line comments are skipped by rules that do not opt in via
// scan_comments; trailing comments on a code line are still scanned.
func isCommentLine(line, ext string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	switch ext {
	case ".py", ".rb", ".sh":
		return strings.HasPrefix(trimmed, "#")
	case ".ts", ".tsx", ".js", ".jsx", ".rs", ".go", ".java", ".kt", ".gd", ".php":
		return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*")
	}
	return false
}