package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// BrowserExtensionsCollector checks whether browser extension
// installation is policy-controlled.
//
//   - Windows: Chrome/Edge ExtensionInstallBlocklist policy
//   - macOS:   /Library/Managed Preferences/<bundle>.plist
//   - Linux:   managed policies.json in /etc/opt/chrome/policies/
type BrowserExtensionsCollector struct{}

func (c *BrowserExtensionsCollector) Name() string { return "browser_extensions" }

func (c *BrowserExtensionsCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	chromePolicy, edgePolicy, method := checkBrowserPolicies(ctx, runtime.GOOS)
	data.Fields["chrome_policy_present"] = chromePolicy
	data.Fields["edge_policy_present"] = edgePolicy
	data.Fields["method"] = method
	// Compliant when at least one browser is policy-controlled.
	data.Compliant = chromePolicy || edgePolicy
	if data.Compliant {
		data.Message = "browser extension policy present (chrome=" +
			strconv.FormatBool(chromePolicy) + " edge=" + strconv.FormatBool(edgePolicy) + ")"
	} else {
		data.Message = "no browser extension policy detected"
	}
	return data, nil
}

func checkBrowserPolicies(ctx context.Context, goos string) (bool, bool, string) {
	switch goos {
	case "windows":
		chrome := hasChromePolicy(ctx)
		edge := hasEdgePolicy(ctx)
		return chrome, edge, "registry"
	case "darwin":
		// /Library/Managed Preferences is the canonical managed-
		// preferences path on macOS. We use defaults to read it.
		chrome := hasPlistManagedPref(ctx, "com.google.Chrome")
		edge := hasPlistManagedPref(ctx, "com.microsoft.Edge")
		return chrome, edge, "managed-preferences"
	default:
		// Linux: Chrome reads /etc/opt/chrome/policies/managed/*.json.
		// Edge uses the same path. Presence of any managed policy file
		// indicates policy-controlled extension installation.
		chrome := false
		edge := false
		if out, err := exec.CommandContext(ctx, "sh", "-c", "ls /etc/opt/chrome/policies/managed/ 2>/dev/null").CombinedOutput(); err == nil {
			chrome = len(strings.TrimSpace(string(out))) > 0
		}
		if out, err := exec.CommandContext(ctx, "sh", "-c", "ls /etc/opt/edge/policies/managed/ 2>/dev/null").CombinedOutput(); err == nil {
			edge = len(strings.TrimSpace(string(out))) > 0
		}
		return chrome, edge, "filesystem"
	}
}

func hasChromePolicy(ctx context.Context) bool {
	// Chrome's Windows policy key is HKLM\Software\Policies\Google\Chrome.
	out, err := exec.CommandContext(ctx, "reg", "query", `HKLM\Software\Policies\Google\Chrome`).CombinedOutput()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "extensioninstallblocklist") || strings.Contains(s, "extensioninstallallowlist")
}

func hasEdgePolicy(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "reg", "query", `HKLM\Software\Policies\Microsoft\Edge`).CombinedOutput()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "extensioninstallblocklist") || strings.Contains(s, "extensioninstallallowlist")
}

func hasPlistManagedPref(ctx context.Context, bundleID string) bool {
	out, err := exec.CommandContext(ctx, "defaults", "read", "/Library/Managed Preferences/"+bundleID+".plist").CombinedOutput()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "extensioninstallblocklist") || strings.Contains(s, "extensioninstallallowlist")
}
