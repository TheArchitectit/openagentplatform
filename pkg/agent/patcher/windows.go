package patcher

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// WindowsScanner enumerates available Windows patches and updates via
// three sources, in order of preference:
//
//  1. `wmic qfe list` — installed hotfixes (the historical source)
//  2. PowerShell `Get-HotFix` — modern replacement for wmic qfe
//  3. `winget list --upgradable` — modern application/package updates
//
// The output of these tools is parsed with regex; tools that are
// missing (winget on older Windows) are silently skipped. Scan
// returns whatever data is available; an error is only returned if
// every available tool fails.
type WindowsScanner struct{}

// Name returns "windows".
func (w *WindowsScanner) Name() string { return "windows" }

// Scan runs each detection tool and merges the results.
func (w *WindowsScanner) Scan(ctx context.Context) ([]PatchInfo, error) {
	var out []PatchInfo
	var errs []string

	if hotfixes, err := w.scanWMIC(ctx); err != nil {
		errs = append(errs, "wmic:"+err.Error())
	} else {
		out = append(out, hotfixes...)
	}

	if ps, err := w.scanGetHotFix(ctx); err != nil {
		errs = append(errs, "powershell:"+err.Error())
	} else {
		// Merge: only add PS entries that aren't already present
		// (wmic and Get-HotFix cover the same installed hotfix set).
		existing := make(map[string]bool, len(out))
		for _, p := range out {
			existing[p.KBID+p.Name] = true
		}
		for _, p := range ps {
			if !existing[p.KBID+p.Name] {
				out = append(out, p)
			}
		}
	}

	if upgradable, err := w.scanWinget(ctx); err != nil {
		errs = append(errs, "winget:"+err.Error())
	} else {
		out = append(out, upgradable...)
	}

	if len(out) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("windows scanner: all tools failed: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

// scanWMIC runs `wmic qfe list brief` and parses installed hotfixes.
// wmic is deprecated on Windows 11+ but still present on most agents.
func (w *WindowsScanner) scanWMIC(ctx context.Context) ([]PatchInfo, error) {
	if _, err := exec.LookPath("wmic"); err != nil {
		return nil, err
	}
	out, err := runCmd(ctx, "wmic", "qfe", "list", "brief")
	if err != nil {
		return nil, err
	}
	return parseWMICQFE(out.Stdout), nil
}

// scanGetHotFix runs PowerShell's Get-HotFix cmdlet. This is the
// recommended path on Windows 11 and Server 2022+ where wmic is
// removed.
func (w *WindowsScanner) scanGetHotFix(ctx context.Context) ([]PatchInfo, error) {
	if _, err := exec.LookPath("powershell"); err != nil {
		// Try pwsh (PowerShell 7+).
		if _, err2 := exec.LookPath("pwsh"); err2 != nil {
			return nil, err
		}
	}
	ps := "powershell"
	if _, err := exec.LookPath("pwsh"); err == nil {
		ps = "pwsh"
	}
	out, err := runCmd(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", "Get-HotFix | Select-Object -Property HotFixID,Description,InstalledOn,InstalledBy | Format-Table -AutoSize")
	if err != nil {
		return nil, err
	}
	return parseGetHotFix(out.Stdout), nil
}

// scanWinget runs `winget list --upgradable` to discover available
// application/package updates. winget is the modern Windows package
// manager and is present on Windows 10 1809+ with App Installer.
func (w *WindowsScanner) scanWinget(ctx context.Context) ([]PatchInfo, error) {
	if _, err := exec.LookPath("winget"); err != nil {
		return nil, err
	}
	out, err := runCmd(ctx, "winget", "list", "--upgradable", "--accept-source-agreements")
	if err != nil {
		return nil, err
	}
	return parseWingetUpgrade(out.Stdout), nil
}

var (
	// wmic qfe columns: HotFixID, InstallDate, Name, Caption, ...
	wmicHFRowRegex = regexp.MustCompile(`\|\s*(KB\d+)\s*\|\s*(\S*)\s*\|`)
)

// parseWMICQFE parses the output of `wmic qfe list brief`. wmic prints
// a header followed by rows of pipe-separated fields. Hotfix IDs are
// matched against KB[0-9]+.
func parseWMICQFE(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "KB") {
			continue
		}
		m := wmicHFRowRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		kb := m[1]
		out = append(out, PatchInfo{
			Name:           kb,
			KBID:           kb,
			Category:       "os_update",
			Severity:       "important",
			PackageManager: "wmic",
			Source:         "wmic qfe",
		})
	}
	return out
}

// parseGetHotFix parses PowerShell's Format-Table output for
// Get-HotFix. Each non-header line that contains a KB id becomes a
// PatchInfo entry.
func parseGetHotFix(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	seenHeader := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !seenHeader {
			// Skip the two header lines (column names + dashes).
			if strings.Contains(line, "HotFixID") || strings.HasPrefix(line, "---") {
				seenHeader = true
				continue
			}
			seenHeader = true
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		kb := ""
		for _, f := range fields {
			if strings.HasPrefix(f, "KB") {
				kb = f
				break
			}
		}
		if kb == "" {
			continue
		}
		desc := ""
		if len(fields) > 1 {
			desc = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), kb))
		}
		out = append(out, PatchInfo{
			Name:           kb,
			KBID:           kb,
			Category:       "os_update",
			Severity:       "important",
			PackageManager: "wmic",
			Source:         "Get-HotFix",
		})
		_ = desc // description not currently surfaced in the struct
	}
	return out
}

// parseWingetUpgrade parses the output of `winget list --upgradable`.
// The default output is a multi-column table; we use a tolerant line-
// by-line match against "Id" and "Version" patterns.
func parseWingetUpgrade(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Skip the header.
		if strings.HasPrefix(strings.ToLower(line), "name") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			continue
		}
		// Each line has at least Name, Id, Version, Available, Source.
		fields := strings.Split(line, " ")
		if len(fields) < 3 {
			continue
		}
		name := fields[0]
		version := fields[len(fields)-1]
		available := fields[len(fields)-1]
		_ = version
		out = append(out, PatchInfo{
			Name:             name,
			InstalledVersion: fields[max(0, len(fields)-2)],
			AvailableVersion: available,
			Category:         "application",
			Severity:         "moderate",
			PackageManager:   "winget",
			Source:           "winget",
		})
	}
	return out
}

// sizeToBytes is shared by all platforms to parse human-readable
// sizes (1.2M, 500K, etc.) into bytes. Not currently used by the
// Windows scanner but kept here for symmetry.
func sizeToBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := int64(1)
	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		mult = 1024
		s = s[:len(s)-1]
	case 'M', 'm':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case 'G', 'g':
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(f * float64(mult))
}

// runCmd is a small helper that runs a command with a context-bound
// timeout, captures stdout+stderr, and never returns a non-nil error
// for non-zero exit codes (we want the parser to see partial output).
// It is defined here (not in the main file) to keep the exec import
// out of the platform-agnostic patcher.go.
func runCmd(ctx context.Context, name string, args ...string) (RunOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return RunOutput{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		RC:     exitCode(err),
	}, err
}

// exitCode returns the process exit code, or -1 if err is nil or
// the process was not started.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
