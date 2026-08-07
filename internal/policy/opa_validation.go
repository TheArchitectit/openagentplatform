package policy

import (
	"errors"
	"regexp"
)

func ValidateRegoSyntax(src string) error {
	if src == "" {
		return errors.New("empty rego source")
	}
	// Must declare a package.
	if !packageRe.MatchString(src) {
		return errors.New("rego must declare a package")
	}
	return nil
}

var packageRe = regexp.MustCompile(`(?m)^\s*package\s+[\w.]+`)

// --- Default Rego policy bodies --------------------------------------------
// These are the built-in compliance checks that ship with the
// platform. The engine seeds them into the database on first boot and
// compiles them into the cache.

const (
	RegoAntivirusInstalled = `package oap_policy

# AV-1: agent must have an AV check assigned and the latest result
# must be "pass".
default allow = false

allow {
    has_av_check
    last_av_result == "pass"
}

has_av_check {
    oap.agent.has_check(input.agent_id, "antivirus")
}

last_av_result = status {
    r := oap.check.last_result(input.agent_id, "antivirus")
    status := r.status
}

violations[{"msg": msg, "details": d}] {
    not oap.agent.has_check(input.agent_id, "antivirus")
    msg := "no antivirus check assigned"
    d := {"check": "antivirus"}
}

violations[{"msg": msg, "details": d}] {
    oap.agent.has_check(input.agent_id, "antivirus")
    r := oap.check.last_result(input.agent_id, "antivirus")
    r.status != "pass"
    msg := sprintf("antivirus check status is %v, expected pass", [r.status])
    d := {"check": "antivirus", "status": r.status}
}
`

	RegoFirewallEnabled = `package oap_policy

default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "firewall")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "firewall")
    r.status != "pass"
    msg := sprintf("firewall status is %v, expected pass", [r.status])
    d := {"check": "firewall", "status": r.status}
}

violations[{"msg": msg, "details": d}] {
    not oap.check.last_result(input.agent_id, "firewall")
    msg := "firewall check has never run"
    d := {"check": "firewall"}
}
`

	RegoDiskEncryption = `package oap_policy

default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "disk_encryption")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "disk_encryption")
    r.status != "pass"
    msg := sprintf("disk encryption status is %v, expected pass", [r.status])
    d := {"check": "disk_encryption", "status": r.status}
}

violations[{"msg": msg, "details": d}] {
    not oap.check.last_result(input.agent_id, "disk_encryption")
    msg := "disk encryption check has never run"
    d := {"check": "disk_encryption"}
}
`

	RegoOSPatching = `package oap_policy

# OS patches must be up-to-date: no critical patches older than 30 days.
default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "os_patching")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "os_patching")
    r.status != "pass"
    msg := sprintf("os patching status is %v, expected pass", [r.status])
    d := {"check": "os_patching", "status": r.status}
}
`

	RegoPasswordPolicy = `package oap_policy

default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "password_policy")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "password_policy")
    r.status != "pass"
    msg := sprintf("password policy status is %v, expected pass", [r.status])
    d := {"check": "password_policy", "status": r.status}
}
`

	RegoScreenLock = `package oap_policy

# Screen lock timeout must be <= 15 minutes (900 seconds).
default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "screen_lock")
    r.status == "pass"
    r.value <= 900
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "screen_lock")
    r.status != "pass"
    msg := sprintf("screen lock status is %v, expected pass", [r.status])
    d := {"check": "screen_lock", "status": r.status}
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "screen_lock")
    r.status == "pass"
    r.value > 900
    msg := sprintf("screen lock timeout is %v seconds; must be <= 900", [r.value])
    d := {"check": "screen_lock", "value": r.value}
}
`

	RegoMonitoringAgentRunning = `package oap_policy

# The OAP agent must be running on the endpoint.
default allow = false

allow {
    oap.agent.status(input.agent_id) == "online"
}

violations[{"msg": msg, "details": d}] {
    oap.agent.status(input.agent_id) != "online"
    msg := sprintf("agent status is %v, expected online", [oap.agent.status(input.agent_id)])
    d := {"agent_id": input.agent_id, "status": oap.agent.status(input.agent_id)}
}
`

	RegoNoSuspiciousServices = `package oap_policy

# No known malicious services should be detected.
default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "suspicious_services")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "suspicious_services")
    r.status != "pass"
    msg := sprintf("suspicious services detected: %v", [r.message])
    d := {"check": "suspicious_services", "details": r.details}
}
`
)

// defaultPolicyMeta maps policy name -> metadata. The engine uses it
// to seed the built-in compliance policies on first boot.
type defaultPolicyMeta struct {
	Rego        string
	Description string
	Category    string
	Severity    string
}

// AllDefaultRegoPolicies maps policy name -> rego body. Used by
// the policy engine's startup seeder.
var AllDefaultRegoPolicies = map[string]defaultPolicyMeta{
	"antivirus_installed": {
		Rego:        RegoAntivirusInstalled,
		Description: "Agent must have an antivirus check that is currently passing.",
		Category:    "security",
		Severity:    "critical",
	},
	"firewall_enabled": {
		Rego:        RegoFirewallEnabled,
		Description: "Host firewall service must be running and passing checks.",
		Category:    "security",
		Severity:    "critical",
	},
	"disk_encryption": {
		Rego:        RegoDiskEncryption,
		Description: "Disk encryption must be active on the host.",
		Category:    "security",
		Severity:    "warning",
	},
	"os_patching": {
		Rego:        RegoOSPatching,
		Description: "Operating system must be patched; no critical patches >30 days old.",
		Category:    "compliance",
		Severity:    "warning",
	},
	"password_policy": {
		Rego:        RegoPasswordPolicy,
		Description: "System password policy must meet complexity requirements.",
		Category:    "compliance",
		Severity:    "warning",
	},
	"screen_lock": {
		Rego:        RegoScreenLock,
		Description: "Screen lock timeout must be <= 15 minutes.",
		Category:    "security",
		Severity:    "info",
	},
	"monitoring_agent_running": {
		Rego:        RegoMonitoringAgentRunning,
		Description: "OpenAgentPlatform agent must be running on the endpoint.",
		Category:    "operational",
		Severity:    "critical",
	},
	"no_suspicious_services": {
		Rego:        RegoNoSuspiciousServices,
		Description: "No known malicious services should be running on the host.",
		Category:    "security",
		Severity:    "critical",
	},
}
