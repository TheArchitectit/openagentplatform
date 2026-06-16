package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
)

// EncryptionCollector checks whether the system disk is encrypted.
//
//   - Windows: manage-bde -status (BitLocker)
//   - Linux:   cryptsetup status <device> for /, or lsblk FSTYPE=crypto_LUKS
//   - macOS:   fdesetup status (FileVault)
type EncryptionCollector struct{}

func (c *EncryptionCollector) Name() string { return "encryption" }

func (c *EncryptionCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	encrypted, method, detail := checkDiskEncryption(ctx, runtime.GOOS)
	data.Fields["encrypted"] = encrypted
	data.Fields["method"] = method
	data.Fields["detail"] = detail
	data.Compliant = encrypted
	if encrypted {
		data.Message = "disk encryption active (" + method + ")"
	} else {
		data.Message = "disk encryption not detected (" + method + ")"
	}
	return data, nil
}

func checkDiskEncryption(ctx context.Context, goos string) (bool, string, string) {
	switch goos {
	case "windows":
		out, err := exec.CommandContext(ctx, "manage-bde", "-status").CombinedOutput()
		if err != nil {
			return false, "manage-bde", "manage-bde failed: " + err.Error()
		}
		s := strings.ToLower(string(out))
		// BitLocker reports "Conversion Status:    Fully Encrypted" or
		// "Protection Status:    Protection On" for already-encrypted volumes.
		if strings.Contains(s, "fully encrypted") || strings.Contains(s, "protection on") {
			return true, "bitlocker", "BitLocker reports protection active"
		}
		return false, "bitlocker", "BitLocker protection not fully on"
	case "darwin":
		out, err := exec.CommandContext(ctx, "fdesetup", "status").CombinedOutput()
		if err != nil {
			return false, "fdesetup", "fdesetup failed: " + err.Error()
		}
		s := strings.ToLower(string(out))
		if strings.Contains(s, "filevault is on") || strings.Contains(s, "on.") {
			return true, "filevault", "FileVault is on"
		}
		return false, "filevault", "FileVault is off"
	default:
		// Linux: check for LUKS-mapped root via lsblk, then cryptsetup.
		if path, _ := exec.LookPath("lsblk"); path != "" {
			out, err := exec.CommandContext(ctx, "lsblk", "-o", "NAME,FSTYPE", "-n").CombinedOutput()
			if err == nil {
				s := string(out)
				if strings.Contains(s, "crypto_LUKS") {
					return true, "luks", "lsblk reports crypto_LUKS filesystem"
				}
			}
		}
		if path, _ := exec.LookPath("cryptsetup"); path != "" {
			// Try the root device; failure is non-fatal.
			out, err := exec.CommandContext(ctx, "cryptsetup", "status", "$(rootdev 2>/dev/null || echo /dev/sda1)").CombinedOutput()
			// shell-out: rootdev may not be present, so ignore parse errors
			// and rely on string match.
			_ = err
			s := strings.ToLower(string(out))
			if strings.Contains(s, "active") && strings.Contains(s, "is active") {
				return true, "cryptsetup", "cryptsetup reports active mapping"
			}
		}
		return false, "none", "no LUKS / FileVault / BitLocker detected"
	}
}
