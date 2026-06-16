package collectors

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// RemoteAccessCollector reports whether RDP, SSH, and VNC are enabled.
//
//   - Windows: RDP via Terminal Services registry; SSH via service state
//   - Linux/macOS: sshd service state; VNC port listeners
type RemoteAccessCollector struct{}

func (c *RemoteAccessCollector) Name() string { return "remote_access" }

func (c *RemoteAccessCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	rdp, ssh, vnc, method := checkRemoteAccess(ctx, runtime.GOOS)
	data.Fields["rdp_enabled"] = rdp
	data.Fields["ssh_enabled"] = ssh
	data.Fields["vnc_enabled"] = vnc
	data.Fields["method"] = method
	// The collector is non-opinionated: it reports raw state and
	// leaves the policy verdict to the Rego layer. The default marks
	// hosts with NO remote-access protocols as compliant; policies
	// that require specific protocols (e.g. SSH for management) can
	// override.
	data.Compliant = !rdp && !ssh && !vnc
	parts := []string{}
	if rdp {
		parts = append(parts, "rdp")
	}
	if ssh {
		parts = append(parts, "ssh")
	}
	if vnc {
		parts = append(parts, "vnc")
	}
	if len(parts) == 0 {
		data.Message = "no remote access protocols detected"
	} else {
		data.Message = "remote access enabled: " + strings.Join(parts, ",")
	}
	return data, nil
}

func checkRemoteAccess(ctx context.Context, goos string) (rdp, ssh, vnc bool, method string) {
	method = "platform-default"
	switch goos {
	case "windows":
		// RDP: HKLM\System\CurrentControlSet\Control\Terminal Server!fDenyTSConnections
		// 0 = enabled, 1 = disabled.
		rdpVal := queryRegDWORDRaw(ctx, `HKLM\System\CurrentControlSet\Control\Terminal Server`, "fDenyTSConnections")
		rdp = rdpVal == 0
		// SSH: check if the sshd Windows capability or service is present.
		out, err := exec.CommandContext(ctx, "sc", "query", "sshd").CombinedOutput()
		if err == nil {
			s := strings.ToLower(string(out))
			if strings.Contains(s, "running") {
				ssh = true
			}
		}
		// VNC: probing specific ports is platform-specific. Best-effort
		// netstat to spot listeners on 5900+.
		if out, err := exec.CommandContext(ctx, "netstat", "-an").CombinedOutput(); err == nil {
			s := string(out)
			if strings.Contains(s, ":5900") || strings.Contains(s, ":5901") {
				vnc = true
			}
		}
		return rdp, ssh, vnc, "windows-default"
	default:
		// Linux/macOS: sshd service.
		out, err := exec.CommandContext(ctx, "sh", "-c", "command -v sshd").CombinedOutput()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			ssh = true
		}
		// VNC: check for listeners on 5900+.
		if out, err := exec.CommandContext(ctx, "sh", "-c", "ss -ltn 2>/dev/null | grep -E ':5900|:5901'").CombinedOutput(); err == nil {
			if len(strings.TrimSpace(string(out))) > 0 {
				vnc = true
			}
		}
		return false, ssh, vnc, "unix-default"
	}
}

// queryRegDWORDRaw is a wrapper around reg query that returns the
// DWORD value as an int. It mirrors the helper in screen_lock.go but
// is duplicated here to keep the collectors self-contained.
func queryRegDWORDRaw(ctx context.Context, key, value string) int {
	cmd := exec.CommandContext(ctx, "reg", "query", key, "/v", value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	s := string(out)
	idx := strings.LastIndex(s, "0x")
	if idx < 0 {
		return parseIntAfter(s, "REG_DWORD")
	}
	hex := strings.TrimSpace(s[idx+2:])
	if space := strings.IndexAny(hex, " \r\n\t"); space >= 0 {
		hex = hex[:space]
	}
	var n int
	if _, err := fmt.Sscanf(hex, "%x", &n); err == nil {
		return n
	}
	return 0
}
