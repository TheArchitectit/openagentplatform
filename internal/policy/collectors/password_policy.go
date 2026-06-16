package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// PasswordPolicyCollector inspects the host's password policy settings.
//
//   - Windows: net accounts
//   - Linux:   chage -l <user> (per-user) or /etc/login.defs
//   - macOS:   pwpolicy -getaccountpolicies
type PasswordPolicyCollector struct{}

func (c *PasswordPolicyCollector) Name() string { return "password_policy" }

func (c *PasswordPolicyCollector) Collect(ctx context.Context, agentID string) (*ComplianceData, error) {
	data := &ComplianceData{
		Collector: c.Name(),
		Platform:  runtime.GOOS,
		Fields:    make(map[string]interface{}),
	}

	minLength := 0
	maxDays := 0
	minDays := 0
	warnDays := 0
	method := "unknown"

	switch runtime.GOOS {
	case "windows":
		out, err := exec.CommandContext(ctx, "net", "accounts").CombinedOutput()
		if err == nil {
			method = "net accounts"
			s := string(out)
			minLength = parseIntAfter(s, "Minimum password length")
			maxDays = parseIntAfter(s, "Maximum password age")
			minDays = parseIntAfter(s, "Minimum password age")
			warnDays = parseIntAfter(s, "Length of password history maintained")
		}
	case "darwin":
		out, err := exec.CommandContext(ctx, "pwpolicy", "-getaccountpolicies").CombinedOutput()
		if err == nil {
			method = "pwpolicy"
			s := string(out)
			minLength = parseIntAfter(s, "policyAttributePassword matches")
			maxDays = parseIntAfter(s, "policyAttributePasswordExpiresAfter")
		}
	default:
		// Linux: try /etc/login.defs first (system-wide defaults).
		if out, err := exec.CommandContext(ctx, "grep", "-E", "^(PASS_MAX_DAYS|PASS_MIN_DAYS|PASS_MIN_LEN)", "/etc/login.defs").CombinedOutput(); err == nil {
			method = "login.defs"
			s := string(out)
			maxDays = parseIntAfter(s, "PASS_MAX_DAYS")
			minDays = parseIntAfter(s, "PASS_MIN_DAYS")
			minLength = parseIntAfter(s, "PASS_MIN_LEN")
		}
	}

	data.Fields["min_length"] = minLength
	data.Fields["max_age_days"] = maxDays
	data.Fields["min_age_days"] = minDays
	data.Fields["warn_days"] = warnDays
	data.Fields["method"] = method
	// Heuristic: compliant when the minimum length is >= 8 and the
	// maximum age is between 1 and 365 days. Policies may override
	// this via Rego.
	data.Compliant = minLength >= 8 && maxDays > 0 && maxDays <= 365
	data.Message = "min_length=" + strconv.Itoa(minLength) +
		" max_age_days=" + strconv.Itoa(maxDays) +
		" method=" + method
	return data, nil
}

// parseIntAfter finds the first integer that follows the given label
// in a multi-line text block. Returns 0 if no integer is found.
func parseIntAfter(s, label string) int {
	idx := strings.Index(s, label)
	if idx < 0 {
		return 0
	}
	rest := s[idx+len(label):]
	// Skip non-digit characters.
	i := 0
	for i < len(rest) && (rest[i] < '0' || rest[i] > '9') {
		i++
	}
	start := i
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if start == i {
		return 0
	}
	n, err := strconv.Atoi(rest[start:i])
	if err != nil {
		return 0
	}
	return n
}
