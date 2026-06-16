package collectors

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// ScreenLockCollector reports whether the host is configured to lock
// the screen after a period of inactivity.
//
//   - Windows: registry HKCU\...\ScreenSaveTimeOut / ScreenSaverIsSecure
//   - Linux:   gsettings / dconf for org.gnome.desktop.session
//   - macOS:   defaults read com.apple.screensaver
type ScreenLockCollector struct{}

func (c *ScreenLockCollector) Name() string { return "screen_lock" }

func (c *ScreenLockCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	enabled, timeoutSec, method := checkScreenLock(ctx, runtime.GOOS)
	data.Fields["enabled"] = enabled
	data.Fields["timeout_sec"] = timeoutSec
	data.Fields["method"] = method
	// Compliant when the lock is enabled and the timeout is <= 15
	// minutes. Policies may enforce stricter thresholds.
	data.Compliant = enabled && timeoutSec > 0 && timeoutSec <= 900
	if data.Compliant {
		data.Message = "screen lock enabled (timeout=" + strconv.Itoa(timeoutSec) + "s)"
	} else {
		data.Message = "screen lock not properly configured (enabled=" +
			strconv.FormatBool(enabled) + " timeout=" + strconv.Itoa(timeoutSec) + "s)"
	}
	return data, nil
}

func checkScreenLock(ctx context.Context, goos string) (bool, int, string) {
	switch goos {
	case "windows":
		// Query two registry values: the screen-saver timeout and the
		// secure-flag (whether the lock requires a password on resume).
		timeout, _ := queryRegDWORD(ctx, `HKCU\Software\Policies\Microsoft\Windows\Control Panel\Desktop`, "ScreenSaveTimeOut")
		secure, _ := queryRegDWORD(ctx, `HKCU\Software\Policies\Microsoft\Windows\Control Panel\Desktop`, "ScreenSaverIsSecure")
		if timeout == 0 {
			timeout, _ = queryRegDWORD(ctx, `HKCU\Control Panel\Desktop`, "ScreenSaveTimeOut")
		}
		if secure == 0 {
			secure, _ = queryRegDWORD(ctx, `HKCU\Control Panel\Desktop`, "ScreenSaverIsSecure")
		}
		return secure == 1, timeout, "registry"
	case "darwin":
		out, err := exec.CommandContext(ctx, "defaults", "read", "com.apple.screensaver").CombinedOutput()
		if err != nil {
			return false, 0, "defaults"
		}
		s := string(out)
		timeout := parseIntAfter(s, "idleTime")
		enabled := strings.Contains(s, "askForPassword = 1") || strings.Contains(s, "askForPassword=1")
		return enabled, timeout, "defaults"
	default:
		// Linux: try gsettings for GNOME, then dconf.
		if path, _ := exec.LookPath("gsettings"); path != "" {
			enabled := false
			if out, err := exec.CommandContext(ctx, "gsettings", "get", "org.gnome.desktop.screensaver", "lock-enabled").CombinedOutput(); err == nil {
				enabled = strings.Contains(string(out), "true")
			}
			timeout := 0
			if out, err := exec.CommandContext(ctx, "gsettings", "get", "org.gnome.desktop.session", "idle-delay").CombinedOutput(); err == nil {
				// idle-delay is in seconds (uint32). The output wraps in
				// single quotes; strip them before parsing.
				v := strings.Trim(strings.TrimSpace(string(out)), "'")
				if n, err := strconv.Atoi(v); err == nil {
					timeout = n
				}
			}
			return enabled, timeout, "gsettings"
		}
		if path, _ := exec.LookPath("dconf"); path != "" {
			out, err := exec.CommandContext(ctx, "dconf", "read", "/org/gnome/desktop/screensaver/lock-enabled").CombinedOutput()
			if err == nil {
				enabled := strings.Contains(string(out), "true")
				return enabled, 0, "dconf"
			}
		}
		return false, 0, "none"
	}
}

// queryRegDWORD queries a Windows registry value using `reg query`.
// Returns 0 and no error when the value is absent or reg is missing.
func queryRegDWORD(ctx context.Context, key, value string) (int, error) {
	cmd := exec.CommandContext(ctx, "reg", "query", key, "/v", value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	s := string(out)
	// reg query output: "<name>    REG_DWORD    0x<hex>" or "0x<hex>".
	idx := strings.LastIndex(s, "0x")
	if idx < 0 {
		// Some locales emit decimal instead.
		return parseIntAfter(s, "REG_DWORD"), nil
	}
	hex := strings.TrimSpace(s[idx+2:])
	// The hex may be followed by a newline; take the first token.
	if space := strings.IndexAny(hex, " \r\n\t"); space >= 0 {
		hex = hex[:space]
	}
	var n int
	if _, err := fmt.Sscanf(hex, "%x", &n); err == nil {
		return n, nil
	}
	if v, err := strconv.Atoi(hex); err == nil {
		return v, nil
	}
	return 0, nil
}
