package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
)

// AVProduct identifies a known antivirus product. The match functions
// return true if the product appears to be installed and running.
type AVProduct struct {
	Name      string
	Processes []string
	Services  []string
	// LinuxPaths lists filesystem paths whose existence indicates installation.
	LinuxPaths []string
	// WindowsRegistry lists registry value names to look for.
	WindowsRegistry []string
}

// KnownAVProducts is the registry of AV products the collector checks
// for. The list is intentionally small and biased toward products with
// a strong enterprise presence; consumers can extend it via a
// custom collector.
var KnownAVProducts = []AVProduct{
	{
		Name:      "windows_defender",
		Processes: []string{"MsMpEng.exe"},
		Services:  []string{"WinDefend"},
	},
	{
		Name:      "clamav",
		Processes: []string{"clamd", "clamav-daemon"},
		Services:  []string{"clamav-daemon", "clamd"},
		LinuxPaths: []string{
			"/usr/sbin/clamd",
			"/usr/bin/clamdscan",
			"/etc/clamav",
		},
	},
	{
		Name:      "sophos",
		Processes: []string{"savservice", "SophosAnti-Virus"},
		Services:  []string{"savservice", "Sophos Anti-Virus"},
		LinuxPaths: []string{
			"/opt/sophos-av",
			"/usr/local/bin/savscan",
		},
	},
	{
		Name:      "crowdstrike",
		Processes: []string{"falcon-sensor", "CSAgent"},
		Services:  []string{"CSAgent", "csagent", "falcon-sensor"},
		LinuxPaths: []string{
			"/opt/CrowdStrike",
			"/opt/falcon-sensor",
		},
	},
	{
		Name:      "sentinelone",
		Processes: []string{"sentinel-agent", "sentinelone-agent"},
		Services:  []string{"sentinel-agent", "sentinelone-agent"},
		LinuxPaths: []string{
			"/opt/sentinelone",
		},
	},
}

// AntivirusCollector checks for the presence of a known antivirus
// product on the host. On Windows it inspects the WinDefend service
// and the MsMpEng.exe process. On Linux/macOS it looks for process
// names via pgrep and checks known filesystem paths.
type AntivirusCollector struct{}

// Name implements Collector.
func (c *AntivirusCollector) Name() string { return "antivirus" }

func (c *AntivirusCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	detected := []string{}
	for _, product := range KnownAVProducts {
		found, detail := checkAVProduct(ctx, product)
		data.Fields[product.Name] = map[string]interface{}{
			"detected": found,
			"detail":   detail,
		}
		if found {
			detected = append(detected, product.Name)
		}
	}

	data.Fields["detected_products"] = detected
	data.Fields["product_count"] = len(detected)
	data.Compliant = len(detected) > 0
	if data.Compliant {
		data.Message = "antivirus product detected: " + strings.Join(detected, ", ")
	} else {
		data.Message = "no recognised antivirus product detected"
	}
	return data, nil
}

// checkAVProduct returns (detected, detail-string) for a product.
// The detection strategy depends on the platform; we only inspect
// fields that are relevant to the current OS to avoid spurious
// failures.
func checkAVProduct(ctx context.Context, p AVProduct) (bool, string) {
	switch runtime.GOOS {
	case "windows":
		// On Windows, check the WinDefend service for Defender and
		// sc query for other services.
		for _, svc := range p.Services {
			if _, err := exec.LookPath("sc"); err == nil {
				cmd := exec.CommandContext(ctx, "sc", "query", svc)
				if out, err := cmd.CombinedOutput(); err == nil {
					if strings.Contains(strings.ToLower(string(out)), "running") {
						return true, "service: " + svc
					}
				}
			}
		}
		// Fall back to tasklist for process detection.
		if _, err := exec.LookPath("tasklist"); err == nil {
			out, err := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq "+p.Processes[0]).CombinedOutput()
			if err == nil && strings.Contains(string(out), p.Processes[0]) {
				return true, "process: " + p.Processes[0]
			}
		}
	case "darwin":
		// On macOS, use pgrep to look for known process names.
		for _, proc := range p.Processes {
			if _, err := exec.LookPath("pgrep"); err == nil {
				cmd := exec.CommandContext(ctx, "pgrep", "-fl", proc)
				if out, err := cmd.CombinedOutput(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
					return true, "process: " + proc
				}
			}
		}
	default:
		// Linux / BSDs: pgrep + filesystem probes.
		for _, proc := range p.Processes {
			if _, err := exec.LookPath("pgrep"); err == nil {
				cmd := exec.CommandContext(ctx, "pgrep", "-f", proc)
				if out, err := cmd.CombinedOutput(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
					return true, "process: " + proc
				}
			}
		}
		// Also probe known paths so we detect products whose daemons
		// are not currently running but which are installed.
		for _, path := range p.LinuxPaths {
			if _, err := exec.CommandContext(ctx, "test", "-e", path).Output(); err == nil {
				return true, "path: " + path
			}
		}
	}
	return false, "not detected"
}
