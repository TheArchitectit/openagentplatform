package gates

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LoadIgnorePatterns reads <projectRoot>/.guardrailsignore: one glob per line,
// blank lines and '#' comments skipped. A trailing '/' marks a directory
// prefix rather than a filename pattern.
//
// A missing file yields no patterns, which is the correct default — there is
// nothing to exclude.
func LoadIgnorePatterns(projectRoot string) ([]string, error) {
	file, err := os.Open(filepath.Join(projectRoot, ".guardrailsignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// IgnoredPath reports whether rel (a project-relative slash path) or its
// basename matches any pattern. A trailing-slash pattern matches the directory
// itself and everything beneath it; every other pattern is matched with
// globMatch against both the relative path and the basename, so a bare
// "*.go" reaches nested files the way the DevGate scanners define it.
func IgnoredPath(patterns []string, rel string) bool {
	base := filepath.Base(rel)
	for _, pattern := range patterns {
		if prefix, ok := strings.CutSuffix(pattern, "/"); ok {
			if rel == prefix || strings.HasPrefix(rel, pattern) {
				return true
			}
			continue
		}
		if globMatch(pattern, rel) || globMatch(pattern, base) {
			return true
		}
	}
	return false
}

// globMatch translates a DevGate glob to a regexp anchored at both ends:
// '*' spans path separators (so "*.go" matches nested files), "**" is a
// globstar, and "**/" additionally matches zero directories. This is the same
// translation guardrails-scan.mjs and regression_diff.py use, kept in sync
// deliberately — a divergence would make the Go gate and the CI scanner
// disagree about which files a rule applies to.
//
// Placeholders are substituted before quoting so the glob metacharacters
// survive QuoteMeta as literal marker text, then expanded back into regexp
// fragments after.
func globMatch(glob, path string) bool {
	const marker = "\x00GS\x00"
	work := glob
	work = strings.ReplaceAll(work, "**/", marker+"DSLASH"+marker)
	work = strings.ReplaceAll(work, "**", marker+"GLOBSTAR"+marker)
	work = strings.ReplaceAll(work, "*", marker+"STAR"+marker)
	work = strings.ReplaceAll(work, "?", marker+"QMARK"+marker)
	work = regexp.QuoteMeta(work)
	work = strings.ReplaceAll(work, marker+"DSLASH"+marker, "(?:.*/)?")
	work = strings.ReplaceAll(work, marker+"GLOBSTAR"+marker, ".*")
	work = strings.ReplaceAll(work, marker+"STAR"+marker, ".*")
	work = strings.ReplaceAll(work, marker+"QMARK"+marker, ".")
	matched, err := regexp.MatchString(`^`+work+`$`, path)
	return err == nil && matched
}