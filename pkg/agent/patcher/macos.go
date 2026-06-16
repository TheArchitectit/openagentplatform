package patcher

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// MacOSScanner enumerates available macOS software updates via
// `softwareupdate -l` (Apple's built-in updater) and Homebrew outdated
// packages via `brew outdated --json=v2` (falls back to plain text
// parsing for older brew versions).
type MacOSScanner struct{}

// Name returns "macos".
func (m *MacOSScanner) Name() string { return "macos" }

// Scan runs both detectors and merges the results.
func (m *MacOSScanner) Scan(ctx context.Context) ([]PatchInfo, error) {
	var out []PatchInfo
	var errs []string

	if sys, err := m.scanSoftwareUpdate(ctx); err != nil {
		errs = append(errs, "softwareupdate:"+err.Error())
	} else {
		out = append(out, sys...)
	}

	if brew, err := m.scanBrewOutdated(ctx); err != nil {
		errs = append(errs, "brew:"+err.Error())
	} else {
		out = append(out, brew...)
	}

	if len(out) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("macos scanner: all tools failed: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

// scanSoftwareUpdate runs `softwareupdate -l` and parses the human-
// readable list it emits.
//
// Example output:
//
//	Software Update found the following new or updated software:
//	   * Label: macOS Big Sur 11.6.1-20G224
//	       Title: macOS Big Sur, Version: 11.6.1, Size: 1218340924K
//	       ...
func (m *MacOSScanner) scanSoftwareUpdate(ctx context.Context) ([]PatchInfo, error) {
	if _, err := exec.LookPath("softwareupdate"); err != nil {
		return nil, err
	}
	out, err := runCmd(ctx, "softwareupdate", "-l")
	if err != nil {
		return nil, err
	}
	return parseSoftwareUpdate(out.Stdout), nil
}

var (
	suLabelRegex   = regexp.MustCompile(`Label:\s*(.+)`)
	suTitleRegex   = regexp.MustCompile(`Title:\s*(.+?),\s*Version:\s*(\S+)(?:,\s*Size:\s*(\S+))?`)
	suRecommendRegex = regexp.MustCompile(`Recommended:\s*(\S+)`)
	suRestartRegex = regexp.MustCompile(`Restart Required:\s*(\S+)`)
)

// parseSoftwareUpdate parses `softwareupdate -l` text output. Each
// block starts with "  * Label: ..." and contains a multi-line
// description. We capture the label, version, size, and restart flag.
func parseSoftwareUpdate(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	var current *PatchInfo
	flush := func() {
		if current != nil {
			out = append(out, *current)
			current = nil
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "* Label:") {
			flush()
			m := suLabelRegex.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			current = &PatchInfo{
				Name:           strings.TrimSpace(m[1]),
				Category:       "os_update",
				Severity:       "important",
				PackageManager: "softwareupdate",
				Source:         "softwareupdate -l",
			}
			continue
		}
		if current == nil {
			continue
		}
		if m := suTitleRegex.FindStringSubmatch(line); m != nil {
			current.AvailableVersion = m[2]
			if len(m) >= 4 && m[3] != "" {
				current.SizeBytes = sizeToBytes(strings.TrimSuffix(m[3], "K"))
			}
		}
		if m := suRecommendRegex.FindStringSubmatch(line); m != nil {
			if strings.EqualFold(m[1], "YES") {
				current.Severity = "critical"
			}
		}
		if m := suRestartRegex.FindStringSubmatch(line); m != nil {
			if strings.EqualFold(m[1], "YES") {
				current.RebootRequired = true
			}
		}
	}
	flush()
	return out
}

// scanBrewOutdated runs `brew outdated` and parses the text output.
// We use the text format rather than JSON to avoid the brew-version
// dependent JSON schema; the text output has been stable since 2014.
func (m *MacOSScanner) scanBrewOutdated(ctx context.Context) ([]PatchInfo, error) {
	if _, err := exec.LookPath("brew"); err != nil {
		return nil, err
	}
	out, err := runCmd(ctx, "brew", "outdated")
	if err != nil && out.RC != 1 {
		// brew outdated exits 1 when packages are out of date
		// and 0 when up to date; treat both as informational.
		return nil, err
	}
	return parseBrewOutdated(out.Stdout), nil
}

// brewLineRegex matches the standard `brew outdated` text output:
//
//	package_name (current) < latest
var brewLineRegex = regexp.MustCompile(`^(\S+)\s+\(\S+\)\s+<\s+(\S+)`)

func parseBrewOutdated(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		m := brewLineRegex.FindStringSubmatch(line)
		if m == nil {
			// Fallback: tolerate the simpler `name  current  latest`
			// format some brew casks use.
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			out = append(out, PatchInfo{
				Name:             fields[0],
				InstalledVersion: fields[1],
				AvailableVersion: fields[2],
				Category:         "application",
				Severity:         "moderate",
				PackageManager:   "brew",
				Source:           "brew outdated",
			})
			continue
		}
		out = append(out, PatchInfo{
			Name:             m[1],
			AvailableVersion: m[2],
			Category:         "application",
			Severity:         "moderate",
			PackageManager:   "brew",
			Source:           "brew outdated",
		})
	}
	return out
}
