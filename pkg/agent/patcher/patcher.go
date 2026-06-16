// Package patcher provides agent-side OS patch scanning capabilities.
// It detects the host operating system and delegates to the appropriate
// platform-specific scanner (Windows, Linux, macOS).
package patcher

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// PatchInfo describes a single available OS patch/update detected on
// the agent's host. Fields are normalised across platforms so the
// server-side catalog can aggregate results from heterogeneous agents.
type PatchInfo struct {
	Name              string   `json:"name"`
	InstalledVersion  string   `json:"installed_version,omitempty"`
	AvailableVersion  string   `json:"available_version,omitempty"`
	Severity          string   `json:"severity,omitempty"`        // critical, important, moderate, low
	Category          string   `json:"category,omitempty"`         // security, os_update, driver, application
	CVEIDs            []string `json:"cve_ids,omitempty"`
	KBID              string   `json:"kb_id,omitempty"`            // Windows knowledge-base article id
	PackageManager    string   `json:"package_manager,omitempty"`  // apt, dnf, yum, zypper, apk, pacman, winget, wmic, softwareupdate, brew
	SizeBytes         int64    `json:"size_bytes,omitempty"`
	RebootRequired    bool     `json:"reboot_required"`
	Source            string   `json:"source,omitempty"`           // free-form origin (e.g. "wmic", "apt list --upgradable")
}

// PatchScanner is the platform-agnostic interface every OS-specific
// scanner implements. Scan returns the set of available patches.
type PatchScanner interface {
	// Name returns the human-readable scanner name (e.g. "windows-wmic").
	Name() string
	// Scan enumerates available patches on the local host.
	Scan(ctx context.Context) ([]PatchInfo, error)
}

// ErrUnsupportedOS is returned by AutoScanner when the runtime OS has
// no registered scanner.
var ErrUnsupportedOS = fmt.Errorf("patcher: unsupported OS: %s", runtime.GOOS)

// ScannerRegistry holds the set of available PatchScanner
// implementations keyed by scanner name. Platform-specific scanners
// register themselves in init().
type ScannerRegistry struct {
	mu       sync.RWMutex
	scanners map[string]PatchScanner
}

// NewScannerRegistry constructs an empty registry.
func NewScannerRegistry() *ScannerRegistry {
	return &ScannerRegistry{scanners: make(map[string]PatchScanner)}
}

// Register adds a scanner under the given name. Duplicate
// registrations are silently ignored (first wins).
func (r *ScannerRegistry) Register(name string, s PatchScanner) {
	if r == nil || s == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.scanners[name]; !ok {
		r.scanners[name] = s
	}
}

// Get returns a registered scanner by name.
func (r *ScannerRegistry) Get(name string) (PatchScanner, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.scanners[name]
	return s, ok
}

// Names returns the list of registered scanner names.
func (r *ScannerRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.scanners))
	for k := range r.scanners {
		out = append(out, k)
	}
	return out
}

// AutoScanner picks the best PatchScanner for the current OS. It
// consults the registry first, then falls back to the built-in
// platform default scanner if none is registered.
type AutoScanner struct {
	registry *ScannerRegistry
}

// NewAutoScanner creates an AutoScanner without an explicit registry.
// It falls back to the built-in platform default scanner at Scan
// time. Use NewAutoScannerFromRegistry when you have registered
// custom scanners and want the AutoScanner to consult them first.
func NewAutoScanner() *AutoScanner {
	return &AutoScanner{}
}

// NewAutoScannerFromRegistry creates an AutoScanner with an explicit
// registry. Use this when the caller has already populated the
// registry with custom scanners.
func NewAutoScannerFromRegistry(reg *ScannerRegistry) *AutoScanner {
	return &AutoScanner{registry: reg}
}

// Name returns "auto".
func (a *AutoScanner) Name() string { return "auto" }

// Scan auto-detects the host OS and dispatches to the appropriate
// platform-specific scanner. Returns ErrUnsupportedOS if no scanner
// matches the current runtime.GOOS.
func (a *AutoScanner) Scan(ctx context.Context) ([]PatchInfo, error) {
	s := a.selectScanner()
	if s == nil {
		return nil, ErrUnsupportedOS
	}
	return s.Scan(ctx)
}

// selectScanner returns the best scanner for the current OS, checking
// the registry first and then falling back to the built-in default.
func (a *AutoScanner) selectScanner() PatchScanner {
	preferred := preferredScannerName()
	if a.registry != nil {
		if s, ok := a.registry.Get(preferred); ok {
			return s
		}
	}
	return defaultScannerForOS()
}

// preferredScannerName returns the registry key for the scanner that
// best matches runtime.GOOS.
func preferredScannerName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	case "darwin":
		return "macos"
	}
	return runtime.GOOS
}

// defaultScannerForOS returns a fresh, built-in scanner for the
// current OS, or nil if the OS is unsupported.
func defaultScannerForOS() PatchScanner {
	switch runtime.GOOS {
	case "windows":
		return &WindowsScanner{}
	case "linux":
		return &LinuxScanner{}
	case "darwin":
		return &MacOSScanner{}
	}
	return nil
}

// RunOutput is a small helper returned by the exec helpers in each
// platform scanner; the parser functions use it to split stdout
// handling from command launching.
type RunOutput struct {
	Stdout string
	Stderr string
	RC     int
}
