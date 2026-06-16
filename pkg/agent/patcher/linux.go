package patcher

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// LinuxScanner detects the active package manager and runs the
// appropriate "check for updates" command. The detection order is:
//
//	1. apt (Debian, Ubuntu)
//	2. dnf (Fedora, RHEL 8+)
//	3. yum (RHEL 7, CentOS 7)
//	4. zypper (SUSE / openSUSE)
//	5. apk (Alpine)
//	6. pacman (Arch)
//
// The first manager found in the PATH wins; this is the same approach
// used by most package-management dashboards.
type LinuxScanner struct{}

// Name returns "linux".
func (l *LinuxScanner) Name() string { return "linux" }

// linuxPackageManager is the detected package manager type.
type linuxPackageManager string

const (
	pkgApt    linuxPackageManager = "apt"
	pkgDnf    linuxPackageManager = "dnf"
	pkgYum    linuxPackageManager = "yum"
	pkgZypper linuxPackageManager = "zypper"
	pkgApk    linuxPackageManager = "apk"
	pkgPacman linuxPackageManager = "pacman"
)

// Scan detects the package manager and runs its check-update command.
func (l *LinuxScanner) Scan(ctx context.Context) ([]PatchInfo, error) {
	mgr, binary, err := l.detectPackageManager()
	if err != nil {
		return nil, err
	}
	return l.scanWith(ctx, mgr, binary)
}

// detectPackageManager returns the first package manager binary
// found on PATH.
func (l *LinuxScanner) detectPackageManager() (linuxPackageManager, string, error) {
	candidates := []struct {
		mgr    linuxPackageManager
		binary string
	}{
		{pkgApt, "apt"},
		{pkgDnf, "dnf"},
		{pkgYum, "yum"},
		{pkgZypper, "zypper"},
		{pkgApk, "apk"},
		{pkgPacman, "pacman"},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c.binary); err == nil {
			return c.mgr, c.binary, nil
		}
	}
	return "", "", fmt.Errorf("linux scanner: no supported package manager found (apt/dnf/yum/zypper/apk/pacman)")
}

// scanWith dispatches to the correct parser for the detected manager.
func (l *LinuxScanner) scanWith(ctx context.Context, mgr linuxPackageManager, binary string) ([]PatchInfo, error) {
	switch mgr {
	case pkgApt:
		out, err := runCmd(ctx, binary, "list", "--upgradable")
		if err != nil {
			return nil, fmt.Errorf("apt list: %w", err)
		}
		return parseAptUpgradable(out.Stdout), nil
	case pkgDnf:
		out, err := runCmd(ctx, binary, "check-update", "-q")
		// dnf check-update returns exit code 100 when updates are
		// available, 0 when up-to-date. Both are non-errors here.
		if err != nil && out.RC != 100 {
			return nil, fmt.Errorf("dnf check-update: %w", err)
		}
		return parseDnfCheckUpdate(out.Stdout, string(mgr)), nil
	case pkgYum:
		out, err := runCmd(ctx, binary, "check-update", "-q")
		if err != nil && out.RC != 100 {
			return nil, fmt.Errorf("yum check-update: %w", err)
		}
		return parseDnfCheckUpdate(out.Stdout, string(mgr)), nil
	case pkgZypper:
		out, err := runCmd(ctx, binary, "list-updates", "-t")
		if err != nil {
			return nil, fmt.Errorf("zypper list-updates: %w", err)
		}
		return parseZypperUpdates(out.Stdout), nil
	case pkgApk:
		out, err := runCmd(ctx, binary, "version", "-v", "L", "available")
		// apk returns non-zero when updates exist; ignore the code
		// and parse whatever stdout we got.
		_ = err
		return parseApkUpgrades(out.Stdout), nil
	case pkgPacman:
		out, err := runCmd(ctx, "bash", "-c", "pacman -Sup 2>/dev/null | awk '{print $4}' | sort -u")
		if err != nil {
			return nil, fmt.Errorf("pacman -Sup: %w", err)
		}
		return parsePacmanUpgrades(out.Stdout), nil
	}
	return nil, fmt.Errorf("linux scanner: unknown package manager %q", mgr)
}

// aptLineRegex matches lines from `apt list --upgradable` of the form:
//
//	firefox/trusty-updates 70.0.1-1~ubuntu0.16.04.1 all [upgradable from: 70.0-1]
var aptLineRegex = regexp.MustCompile(`^([^/\s]+)/([^\s]+)\s+(\S+)\s+(\S+)\s+(\S+)\s+\[upgradable from:\s+(\S+)\]`)

// parseAptUpgradable parses `apt list --upgradable` output.
func parseAptUpgradable(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Listing") {
			continue
		}
		m := aptLineRegex.FindStringSubmatch(line)
		if m == nil {
			// Fallback: a simpler two-field format for older apt
			// versions.
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				name := fields[0]
				ver := fields[1]
				out = append(out, PatchInfo{
					Name:             strings.SplitN(name, "/", 2)[0],
					AvailableVersion: ver,
					Category:         "security",
					Severity:         "moderate",
					PackageManager:   "apt",
					Source:           "apt list --upgradable",
				})
			}
			continue
		}
		name := m[1]
		repo := m[2]
		avail := m[3]
		installed := m[6]
		severity := "moderate"
		if strings.Contains(repo, "security") || strings.Contains(repo, "-security") {
			severity = "critical"
		}
		out = append(out, PatchInfo{
			Name:             name,
			InstalledVersion: installed,
			AvailableVersion: avail,
			Category:         "security",
			Severity:         severity,
			PackageManager:   "apt",
			Source:           "apt list --upgradable",
		})
	}
	return out
}

// dnfRowRegex matches tab-separated dnf/yum check-update rows of the
// form:  name.arch   version   repo
var dnfRowRegex = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(\S+)\s*$`)

// parseDnfCheckUpdate parses `dnf check-update` / `yum check-update`
// output. Both tools emit a header line and then rows like:
//
//	kernel.x86_64    4.18.0-305.7.1.el8_4    BaseOS
func parseDnfCheckUpdate(s, mgr string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "loaded plugins") || strings.HasPrefix(lower, "last metadata") {
			continue
		}
		// Skip the column header.
		if strings.HasPrefix(lower, "package") || strings.HasPrefix(lower, "name") {
			continue
		}
		// Skip separator lines.
		if strings.HasPrefix(line, "=") || strings.HasPrefix(line, "-") {
			continue
		}
		m := dnfRowRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		ver := m[2]
		repo := m[3]
		severity := "moderate"
		if strings.Contains(strings.ToLower(repo), "security") {
			severity = "critical"
		}
		out = append(out, PatchInfo{
			Name:             name,
			AvailableVersion: ver,
			Category:         "security",
			Severity:         severity,
			PackageManager:   mgr,
			Source:           mgr + " check-update",
		})
	}
	return out
}

// parseZypperUpdates parses `zypper list-updates -t` output. The
// table format includes columns: Repository | Name | Version | Arch
// | Status. The "i" or "v" flags before the version indicate the
// installed vs available version.
func parseZypperUpdates(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "S ") ||
			strings.HasPrefix(strings.TrimSpace(line), "---") ||
			strings.HasPrefix(strings.TrimSpace(line), "Repository") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[1]
		ver := fields[2]
		out = append(out, PatchInfo{
			Name:             name,
			AvailableVersion: ver,
			Category:         "security",
			Severity:         "moderate",
			PackageManager:   "zypper",
			Source:           "zypper list-updates",
		})
	}
	return out
}

// parseApkUpgrades parses `apk version -v L available` output. Lines
// look like:
//
//	firefox-68.10.0-r0 -> 78.4.0-r0
var apkLineRegex = regexp.MustCompile(`^(\S+)-(\S+)\s+->\s+(\S+)$`)

func parseApkUpgrades(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		m := apkLineRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, PatchInfo{
			Name:             m[1],
			InstalledVersion: m[2],
			AvailableVersion: m[3],
			Category:         "security",
			Severity:         "moderate",
			PackageManager:   "apk",
			Source:           "apk version",
		})
	}
	return out
}

// parsePacmanUpgrades parses a newline-separated list of package
// names from `pacman -Sup` (after the bash-awk filter).
func parsePacmanUpgrades(s string) []PatchInfo {
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []PatchInfo
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" {
			continue
		}
		out = append(out, PatchInfo{
			Name:           name,
			Category:       "security",
			Severity:       "moderate",
			PackageManager: "pacman",
			Source:         "pacman -Sup",
		})
	}
	return out
}
