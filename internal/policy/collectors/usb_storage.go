package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
)

// USBStorageCollector checks whether removable USB storage is allowed.
//
//   - Windows: registry HKLM\SYSTEM\...\USBSTOR!Start
//   - Linux:   presence of usb_storage in modprobe blacklist, and
//              matching udev rules.
type USBStorageCollector struct{}

func (c *USBStorageCollector) Name() string { return "usb_storage" }

func (c *USBStorageCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	restricted, method, detail := checkUSBStorage(ctx, runtime.GOOS)
	data.Fields["restricted"] = restricted
	data.Fields["method"] = method
	data.Fields["detail"] = detail
	// The collector is "compliant" when removable storage is
	// restricted. Sites that permit USB will set Compliant=false via
	// policy.
	data.Compliant = restricted
	if restricted {
		data.Message = "USB storage restricted (" + method + ")"
	} else {
		data.Message = "USB storage not restricted (" + method + ")"
	}
	return data, nil
}

func checkUSBStorage(ctx context.Context, goos string) (bool, string, string) {
	switch goos {
	case "windows":
		// USBSTOR!Start == 4 means disabled.
		out, err := exec.CommandContext(ctx, "reg", "query", `HKLM\SYSTEM\CurrentControlSet\Services\USBSTOR`, "/v", "Start").CombinedOutput()
		if err != nil {
			return false, "registry", "reg query failed: " + err.Error()
		}
		s := string(out)
		if strings.Contains(s, "0x4") {
			return true, "registry", "USBSTOR!Start=4 (disabled)"
		}
		return false, "registry", "USBSTOR!Start is not 4"
	default:
		// Linux: check modprobe blacklist and udev rules.
		blacklisted := false
		if path, _ := exec.LookPath("modprobe"); path != "" {
			out, err := exec.CommandContext(ctx, "modprobe", "-n", "-v", "usb_storage").CombinedOutput()
			if err == nil {
				s := strings.ToLower(string(out))
				if strings.Contains(s, "disallow") || strings.Contains(s, "blacklist") {
					blacklisted = true
				}
			}
		}
		// Look for a blacklist file in /etc/modprobe.d/.
		if out, err := exec.CommandContext(ctx, "sh", "-c", "grep -l usb_storage /etc/modprobe.d/* 2>/dev/null").CombinedOutput(); err == nil {
			if len(strings.TrimSpace(string(out))) > 0 {
				blacklisted = true
			}
		}
		// Check for udev rules blocking USB.
		udevBlocked := false
		if out, err := exec.CommandContext(ctx, "sh", "-c", "grep -lE 'usb|vendor|product' /etc/udev/rules.d/* 2>/dev/null").CombinedOutput(); err == nil {
			if len(strings.TrimSpace(string(out))) > 0 {
				udevBlocked = true
			}
		}
		if blacklisted {
			return true, "modprobe-blacklist", "usb_storage blacklisted"
		}
		if udevBlocked {
			return true, "udev", "udev rules present"
		}
		return false, "none", "no USB restrictions detected"
	}
}
