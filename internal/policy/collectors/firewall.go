package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
)

// FirewallCollector reports whether the host firewall is enabled.
// Detection strategy varies by platform:
//
//   - Windows:  netsh advfirewall show allprofiles
//   - Linux:    ufw status, iptables -L -n, nft list ruleset
//   - macOS:    pfctl -s info
type FirewallCollector struct{}

func (c *FirewallCollector) Name() string { return "firewall" }

func (c *FirewallCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	enabled, method, detail := checkFirewall(ctx, runtime.GOOS)
	data.Fields["enabled"] = enabled
	data.Fields["method"] = method
	data.Fields["detail"] = detail
	data.Compliant = enabled
	if enabled {
		data.Message = "firewall enabled via " + method
	} else {
		data.Message = "firewall not enabled (checked via " + method + ")"
	}
	return data, nil
}

// checkFirewall returns (enabled, method, detail) for the current OS.
func checkFirewall(ctx context.Context, goos string) (bool, string, string) {
	switch goos {
	case "windows":
		out, err := exec.CommandContext(ctx, "netsh", "advfirewall", "show", "allprofiles").CombinedOutput()
		if err != nil {
			return false, "netsh", "netsh failed: " + err.Error()
		}
		s := strings.ToLower(string(out))
		if strings.Contains(s, "state") && strings.Contains(s, "on") {
			return true, "netsh advfirewall", "all profiles report firewall on"
		}
		// Count how many profiles are on.
		onCount := strings.Count(s, "on")
		return onCount > 0, "netsh advfirewall", "profiles_on=" + itoa(onCount)
	case "darwin":
		out, err := exec.CommandContext(ctx, "pfctl", "-s", "info").CombinedOutput()
		if err != nil {
			return false, "pfctl", "pfctl failed: " + err.Error()
		}
		s := strings.ToLower(string(out))
		if strings.Contains(s, "status: enabled") {
			return true, "pfctl", "pf status: enabled"
		}
		return false, "pfctl", "pf status not enabled"
	default:
		// Linux: try ufw first, then iptables, then nft.
		if path, _ := exec.LookPath("ufw"); path != "" {
			out, err := exec.CommandContext(ctx, "ufw", "status").CombinedOutput()
			if err == nil {
				s := strings.ToLower(string(out))
				if strings.Contains(s, "status: active") {
					return true, "ufw", "ufw active"
				}
				return false, "ufw", "ufw not active"
			}
		}
		if path, _ := exec.LookPath("iptables"); path != "" {
			out, err := exec.CommandContext(ctx, "iptables", "-L", "-n").CombinedOutput()
			if err == nil {
				// If the OUTPUT chain has any non-default policy rule, the
				// host has active firewall rules.
				s := string(out)
				hasRules := strings.Contains(s, "policy DROP") || strings.Contains(s, "policy REJECT")
				return hasRules, "iptables", "iptables policies inspected"
			}
		}
		if path, _ := exec.LookPath("nft"); path != "" {
			out, err := exec.CommandContext(ctx, "nft", "list", "ruleset").CombinedOutput()
			if err == nil && len(strings.TrimSpace(string(out))) > 0 {
				return true, "nftables", "nftables ruleset present"
			}
			return false, "nftables", "nftables ruleset empty"
		}
		return false, "none", "no firewall tooling found"
	}
}

// itoa is a small helper to avoid pulling in strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
