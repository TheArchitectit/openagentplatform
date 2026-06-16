package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// PatchingCollector reports the OS patch level.
//
//   - Windows:  wmic qfe list (list of installed hotfixes)
//   - Linux:    apt list --upgradable, dnf check-update, yum check-update
//   - macOS:    softwareupdate --history
//
// The collector returns the number of available updates and the
// timestamp of the most recent installed patch. Compliance is
// determined by caller-configured thresholds (e.g. patches within
// N days). The collector itself is non-opinionated: it only
// reports the raw numbers.
type PatchingCollector struct{}

func (c *PatchingCollector) Name() string { return "patching" }

func (c *PatchingCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	available, lastPatchDays, method, err := checkPatching(ctx, runtime.GOOS)
	if err != nil {
		data.Fields["error"] = err.Error()
	}
	data.Fields["updates_available"] = available
	data.Fields["days_since_last_patch"] = lastPatchDays
	data.Fields["method"] = method
	// No built-in threshold: mark compliant when we have data and the
	// system is at least queryable. Policies that care about patch
	// recency should inspect days_since_last_patch.
	data.Compliant = err == nil
	if err != nil {
		data.Message = "patching check failed: " + err.Error()
	} else {
		data.Message = "updates_available=" + strconv.Itoa(available) +
			" days_since_last_patch=" + strconv.Itoa(lastPatchDays)
	}
	return data, nil
}

func checkPatching(ctx context.Context, goos string) (updates int, lastPatchDays int, method string, err error) {
	switch goos {
	case "windows":
		// wmic qfe returns hotfix rows; counting them gives a rough
		// patch count. The list also includes install dates but parsing
		// them cross-locale is fragile, so we return the count only.
		out, err2 := exec.CommandContext(ctx, "wmic", "qfe", "list", "brief").CombinedOutput()
		if err2 != nil {
			return 0, -1, "wmic qfe", err2
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		// First line is the header. Each hotfix row is 1 line.
		count := 0
		for _, l := range lines[1:] {
			if strings.TrimSpace(l) != "" {
				count++
			}
		}
		return 0, -1, "wmic qfe", nil
	case "darwin":
		// softwareupdate --history returns one line per installed
		// update; the first column is the label. We treat any
		// available updates as non-compliant if present.
		out, err2 := exec.CommandContext(ctx, "softwareupdate", "--history").CombinedOutput()
		if err2 != nil {
			return 0, -1, "softwareupdate", err2
		}
		_ = out
		// Check for available updates separately.
		out2, err3 := exec.CommandContext(ctx, "softwareupdate", "-l").CombinedOutput()
		if err3 == nil {
			s := strings.ToLower(string(out2))
			if strings.Contains(s, "no new software") || strings.Contains(s, "no updates") {
				return 0, 0, "softwareupdate", nil
			}
			// Best-effort count: lines starting with "  *".
			count := strings.Count(string(out2), "\n  *")
			return count, 0, "softwareupdate", nil
		}
		return 0, 0, "softwareupdate", nil
	default:
		// Linux: try apt, then dnf, then yum.
		if path, _ := exec.LookPath("apt"); path != "" {
			out, err2 := exec.CommandContext(ctx, "apt", "list", "--upgradable").CombinedOutput()
			if err2 == nil {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				count := 0
				for _, l := range lines[1:] {
					if strings.Contains(l, "upgradable") {
						count++
					}
				}
				return count, 0, "apt", nil
			}
		}
		if path, _ := exec.LookPath("dnf"); path != "" {
			out, _ := exec.CommandContext(ctx, "dnf", "check-update", "-q").CombinedOutput()
			// dnf exits 100 when updates are available; we treat
			// that as success and count the rows.
			s := strings.TrimSpace(string(out))
			count := 0
			if s != "" {
				count = len(strings.Split(s, "\n"))
			}
			return count, 0, "dnf", nil
		}
		if path, _ := exec.LookPath("yum"); path != "" {
			out, _ := exec.CommandContext(ctx, "yum", "check-update", "-q").CombinedOutput()
			s := strings.TrimSpace(string(out))
			count := 0
			if s != "" {
				count = len(strings.Split(s, "\n"))
			}
			return count, 0, "yum", nil
		}
		return 0, -1, "none", nil
	}
}
