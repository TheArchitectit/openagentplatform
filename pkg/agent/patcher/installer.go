package patcher

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// InstallResult is the outcome of a single patch install attempt.
type InstallResult struct {
	Success        bool   `json:"success"`
	Output         string `json:"output"`
	RebootRequired bool   `json:"reboot_required"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// PatchInstaller is the platform-agnostic install interface. The
// agent's NATS handler will look up the correct installer for the
// current OS and call Install with the patch metadata to apply.
type PatchInstaller interface {
	// Name returns the installer name (e.g. "windows", "linux", "macos").
	Name() string
	// Install applies a single patch. The returned InstallResult
	// captures success, output, and reboot-required flag.
	Install(ctx context.Context, patch *PatchInfo) (*InstallResult, error)
	// Rollback attempts to undo a previously applied patch. Not all
	// package managers support deterministic rollback; when they
	// don't, this returns a "not supported" error and the caller
	// falls back to a snapshot-based rollback.
	Rollback(ctx context.Context, patch *PatchInfo) (*InstallResult, error)
}

// InstallerRegistry holds the set of available PatchInstaller
// implementations. The agent looks up the right installer at request
// time using the same auto-detection logic as PatchScanner.
type InstallerRegistry struct {
	mu         sync.RWMutex
	installers map[string]PatchInstaller
}

// NewInstallerRegistry creates an empty registry.
func NewInstallerRegistry() *InstallerRegistry {
	return &InstallerRegistry{installers: make(map[string]PatchInstaller)}
}

// Register adds an installer under the given name.
func (r *InstallerRegistry) Register(name string, i PatchInstaller) {
	if r == nil || i == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.installers[name]; !ok {
		r.installers[name] = i
	}
}

// Get retrieves a registered installer.
func (r *InstallerRegistry) Get(name string) (PatchInstaller, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.installers[name]
	return i, ok
}

// AutoInstaller picks the right PatchInstaller for the current OS.
type AutoInstaller struct {
	registry *InstallerRegistry
}

// NewAutoInstaller creates an AutoInstaller with an optional
// registry. If registry is nil, the built-in platform default
// installer is used.
func NewAutoInstaller(reg *InstallerRegistry) *AutoInstaller {
	return &AutoInstaller{registry: reg}
}

// Name returns "auto".
func (a *AutoInstaller) Name() string { return "auto" }

// Install auto-detects the host OS and dispatches to the correct
// platform installer.
func (a *AutoInstaller) Install(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	i := a.selectInstaller()
	if i == nil {
		return nil, fmt.Errorf("patcher: no installer for OS %s", runtime.GOOS)
	}
	return i.Install(ctx, patch)
}

// Rollback auto-detects the host OS and dispatches.
func (a *AutoInstaller) Rollback(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	i := a.selectInstaller()
	if i == nil {
		return nil, fmt.Errorf("patcher: no installer for OS %s", runtime.GOOS)
	}
	return i.Rollback(ctx, patch)
}

func (a *AutoInstaller) selectInstaller() PatchInstaller {
	preferred := preferredScannerName()
	if a.registry != nil {
		if inst, ok := a.registry.Get(preferred); ok {
			return inst
		}
	}
	switch runtime.GOOS {
	case "windows":
		return &WindowsInstaller{}
	case "linux":
		return &LinuxInstaller{}
	case "darwin":
		return &MacOSInstaller{}
	}
	return nil
}

// WindowsInstaller installs Windows patches using wusa.exe for .msu
// files, msiexec for .msi packages, and winget for general app
// upgrades. The choice is driven by the PatchInfo.PackageManager
// field set by the scanner.
type WindowsInstaller struct{}

// Name returns "windows".
func (w *WindowsInstaller) Name() string { return "windows" }

// Install dispatches based on the patch's package manager hint.
func (w *WindowsInstaller) Install(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch == nil {
		return nil, fmt.Errorf("windows installer: nil patch")
	}
	switch patch.PackageManager {
	case "winget":
		return w.installWinget(ctx, patch)
	case "wmic":
		// The scanner's "wmic" source covers Windows hotfixes. Use
		// wusa.exe to install the corresponding .msu by KB id.
		return w.installWusa(ctx, patch)
	case "msi":
		return w.installMSI(ctx, patch)
	}
	// Fallback: try wusa first; if not available, winget.
	if _, err := execLookPath("wusa"); err == nil {
		return w.installWusa(ctx, patch)
	}
	return w.installWinget(ctx, patch)
}

// Rollback attempts to uninstall a Windows hotfix via wusa /uninstall.
func (w *WindowsInstaller) Rollback(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch == nil || patch.KBID == "" {
		return &InstallResult{
			Success:      false,
			ErrorMessage: "rollback requires KB id",
		}, nil
	}
	out, err := runCmd(ctx, "wusa.exe", "/uninstall", "/kb:"+strings.TrimPrefix(patch.KBID, "KB"), "/quiet", "/norestart")
	res := &InstallResult{
		Success:      err == nil,
		Output:       out.Stdout + out.Stderr,
		RebootRequired: out.RC == 3010, // wusa's "reboot required" code
	}
	if err != nil {
		res.ErrorMessage = err.Error()
	}
	return res, nil
}

func (w *WindowsInstaller) installWusa(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch.KBID == "" {
		return &InstallResult{
			Success:      false,
			ErrorMessage: "wusa install requires KB id",
		}, nil
	}
	out, err := runCmd(ctx, "wusa.exe", "/install", "/kb:"+strings.TrimPrefix(patch.KBID, "KB"), "/quiet", "/norestart")
	res := &InstallResult{
		Success:        err == nil,
		Output:         out.Stdout + out.Stderr,
		RebootRequired: out.RC == 3010,
	}
	if err != nil {
		res.ErrorMessage = err.Error()
	}
	return res, nil
}

func (w *WindowsInstaller) installMSI(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch.Name == "" {
		return &InstallResult{
			Success:      false,
			ErrorMessage: "msi install requires msi path",
		}, nil
	}
	out, err := runCmd(ctx, "msiexec.exe", "/i", patch.Name, "/qn", "/norestart")
	res := &InstallResult{
		Success:        err == nil,
		Output:         out.Stdout + out.Stderr,
		RebootRequired: out.RC == 3010,
	}
	if err != nil {
		res.ErrorMessage = err.Error()
	}
	return res, nil
}

func (w *WindowsInstaller) installWinget(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch.Name == "" {
		return &InstallResult{
			Success:      false,
			ErrorMessage: "winget install requires package id",
		}, nil
	}
	// `winget upgrade` with --id and the new version is the closest
	// to a deterministic install.
	args := []string{"upgrade", "--id", patch.Name, "--accept-package-agreements", "--accept-source-agreements", "--silent"}
	if patch.AvailableVersion != "" {
		args = append(args, "--version", patch.AvailableVersion)
	}
	out, err := runCmd(ctx, "winget", args...)
	res := &InstallResult{
		Success:      err == nil,
		Output:       out.Stdout + out.Stderr,
	}
	if err != nil {
		res.ErrorMessage = err.Error()
	}
	return res, nil
}

// LinuxInstaller installs Linux packages using the appropriate
// package manager. Like the scanner, it detects the active manager
// at install time.
type LinuxInstaller struct{}

// Name returns "linux".
func (l *LinuxInstaller) Name() string { return "linux" }

// Install detects the package manager and runs the matching install
// command.
func (l *LinuxInstaller) Install(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch == nil {
		return nil, fmt.Errorf("linux installer: nil patch")
	}
	switch patch.PackageManager {
	case "apt":
		return l.runApt(ctx, patch)
	case "dnf":
		return l.runDnf(ctx, patch, "dnf")
	case "yum":
		return l.runDnf(ctx, patch, "yum")
	case "zypper":
		return l.runZypper(ctx, patch)
	case "apk":
		return l.runApk(ctx, patch)
	case "pacman":
		return l.runPacman(ctx, patch)
	}
	// Fallback: try to detect the package manager from the patch's
	// package name suffix (.deb, .rpm, .apk, .pkg.tar.zst).
	return l.installBySuffix(ctx, patch)
}

// Rollback uses the package manager's downgrade support where
// available. apt and dnf both support `downgrade`; pacman uses the
// package cache; zypper and apk require an explicit version.
func (l *LinuxInstaller) Rollback(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch == nil {
		return nil, fmt.Errorf("linux installer: nil patch")
	}
	switch patch.PackageManager {
	case "apt":
		if patch.InstalledVersion == "" {
			return &InstallResult{Success: false, ErrorMessage: "rollback requires installed_version"}, nil
		}
		out, err := runCmd(ctx, "apt-get", "install", "-y", "--allow-downgrades",
			fmt.Sprintf("%s=%s", patch.Name, patch.InstalledVersion))
		return makeResult(out, err), nil
	case "dnf", "yum":
		if patch.InstalledVersion == "" {
			return &InstallResult{Success: false, ErrorMessage: "rollback requires installed_version"}, nil
		}
		out, err := runCmd(ctx, patch.PackageManager, "downgrade", "-y", patch.Name)
		return makeResult(out, err), nil
	case "pacman":
		out, err := runCmd(ctx, "pacman", "-U", "--noconfirm", fmt.Sprintf("/var/cache/pacman/pkg/%s", patch.Name))
		return makeResult(out, err), nil
	}
	return &InstallResult{
		Success:      false,
		ErrorMessage: "rollback not supported for " + patch.PackageManager,
	}, nil
}

func (l *LinuxInstaller) runApt(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	// Use `apt-get install -y --only-upgrade` so we don't accidentally
	// install a previously removed package.
	out, err := runCmd(ctx, "apt-get", "install", "-y", "--only-upgrade", patch.Name)
	res := makeResult(out, err)
	if res.RebootRequired == false && (patch.Category == "os_update" || strings.Contains(patch.Name, "kernel")) {
		res.RebootRequired = true
	}
	return res, nil
}

func (l *LinuxInstaller) runDnf(ctx context.Context, patch *PatchInfo, bin string) (*InstallResult, error) {
	out, err := runCmd(ctx, bin, "upgrade", "-y", patch.Name)
	res := makeResult(out, err)
	if patch.Category == "os_update" || strings.Contains(patch.Name, "kernel") {
		res.RebootRequired = true
	}
	return res, nil
}

func (l *LinuxInstaller) runZypper(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	out, err := runCmd(ctx, "zypper", "--non-interactive", "update", patch.Name)
	return makeResult(out, err), nil
}

func (l *LinuxInstaller) runApk(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	out, err := runCmd(ctx, "apk", "upgrade", patch.Name)
	return makeResult(out, err), nil
}

func (l *LinuxInstaller) runPacman(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	out, err := runCmd(ctx, "pacman", "-S", "--noconfirm", patch.Name)
	return makeResult(out, err), nil
}

func (l *LinuxInstaller) installBySuffix(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	lower := strings.ToLower(patch.Name)
	switch {
	case strings.HasSuffix(lower, ".deb"):
		out, err := runCmd(ctx, "dpkg", "-i", patch.Name)
		return makeResult(out, err), nil
	case strings.HasSuffix(lower, ".rpm"):
		out, err := runCmd(ctx, "rpm", "-Uvh", patch.Name)
		return makeResult(out, err), nil
	}
	return &InstallResult{
		Success:      false,
		ErrorMessage: "linux installer: cannot determine install method for " + patch.Name,
	}, nil
}

// MacOSInstaller installs macOS updates and Homebrew packages.
type MacOSInstaller struct{}

// Name returns "macos".
func (m *MacOSInstaller) Name() string { return "macos" }

// Install dispatches based on the patch's package manager hint.
func (m *MacOSInstaller) Install(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch == nil {
		return nil, fmt.Errorf("macos installer: nil patch")
	}
	switch patch.PackageManager {
	case "softwareupdate":
		return m.runSoftwareUpdate(ctx, patch)
	case "brew":
		return m.runBrewUpgrade(ctx, patch)
	}
	// Heuristic: Apple system updates carry no version prefix and
	// often have spaces; brew packages do not.
	if strings.Contains(patch.Name, " ") {
		return m.runSoftwareUpdate(ctx, patch)
	}
	return m.runBrewUpgrade(ctx, patch)
}

// Rollback is not well-defined for macOS software updates. We attempt
// `brew pin` to prevent the package from being upgraded again as a
// soft rollback. Full system update rollback requires a Time Machine
// restore which is outside the scope of this agent.
func (m *MacOSInstaller) Rollback(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	if patch == nil {
		return nil, fmt.Errorf("macos installer: nil patch")
	}
	if patch.PackageManager == "brew" {
		out, err := runCmd(ctx, "brew", "pin", patch.Name)
		return makeResult(out, err), nil
	}
	return &InstallResult{
		Success:      false,
		ErrorMessage: "macos installer: softwareupdate rollback not supported; use Time Machine",
	}, nil
}

func (m *MacOSInstaller) runSoftwareUpdate(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	// -i installs the named update; -R restarts automatically (we
	// report reboot-required but don't actually restart from the
	// agent process).
	out, err := runCmd(ctx, "softwareupdate", "-i", patch.Name, "--agree-to-license")
	res := makeResult(out, err)
	// Most Apple system updates require a reboot.
	if patch.Category == "os_update" {
		res.RebootRequired = true
	}
	return res, nil
}

func (m *MacOSInstaller) runBrewUpgrade(ctx context.Context, patch *PatchInfo) (*InstallResult, error) {
	out, err := runCmd(ctx, "brew", "upgrade", patch.Name)
	return makeResult(out, err), nil
}

// makeResult converts a RunOutput + error into an InstallResult.
func makeResult(out RunOutput, err error) *InstallResult {
	res := &InstallResult{
		Success:        err == nil,
		Output:         out.Stdout + out.Stderr,
		RebootRequired: out.RC == 3010,
	}
	if err != nil {
		res.ErrorMessage = err.Error()
	}
	return res
}

// execLookPath is a thin wrapper to keep the installer package's
// import surface minimal; the real implementation is in the
// platform-specific files (windows.go, linux.go, macos.go).
var execLookPath = func(name string) (string, error) {
	return execPath(name)
}
