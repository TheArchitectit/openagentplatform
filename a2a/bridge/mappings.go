// Package bridge - mappings.go defines the event-to-skill-tag
// translation table used by the Event-to-Task bridge. Each NATS event
// subject maps to one or more A2A skill tags that downstream agents
// can use for routing and capability matching.
package bridge

// Skill tag constants used across the bridge. Agents advertise these
// tags in their AgentCard to signal which event types they can handle.
const (
	// SkillDiagnostics indicates an agent can analyse and report on
	// diagnostic data (check results, system health).
	SkillDiagnostics = "diagnostics"

	// SkillRemediation indicates an agent can take corrective action
	// (restart services, clear disk, run repair scripts).
	SkillRemediation = "remediation"

	// SkillTriage indicates an agent can classify and prioritise
	// incoming alerts.
	SkillTriage = "triage"

	// SkillCompliance indicates an agent handles policy violations
	// and regulatory compliance checks.
	SkillCompliance = "compliance"

	// SkillPatching indicates an agent manages OS/software patch
	// deployment workflows.
	SkillPatching = "patching"

	// SkillScripting indicates an agent can interpret and act on
	// script execution results.
	SkillScripting = "scripting"

	// SkillShellAccess indicates an agent handles interactive remote
	// shell session events.
	SkillShellAccess = "shell-access"

	// SkillFleetManagement indicates an agent monitors agent
	// lifecycle (online/offline state changes).
	SkillFleetManagement = "fleet-management"
)

// EventSubjectToSkills maps a NATS event subject to the A2A skill
// tags that should be attached to the generated task. A single event
// may produce a task with multiple skill tags so that any agent that
// advertises at least one matching skill can pick the work up.
var EventSubjectToSkills = map[string][]string{
	SubjectCheckResult: {
		SkillDiagnostics,
		SkillTriage,
	},
	SubjectAlertEvents: {
		SkillTriage,
		SkillRemediation,
		SkillDiagnostics,
	},
	SubjectAgentOnline: {
		SkillFleetManagement,
	},
	SubjectAgentOffline: {
		SkillFleetManagement,
		SkillRemediation,
	},
	SubjectPolicyViolation: {
		SkillCompliance,
		SkillTriage,
	},
	SubjectPatchStatus: {
		SkillPatching,
	},
	SubjectScriptResult: {
		SkillScripting,
		SkillDiagnostics,
	},
	SubjectShellSession: {
		SkillShellAccess,
		SkillDiagnostics,
	},
}

// EventSubjectToName maps a NATS event subject to a human-readable
// task name. Used as the default name when the event payload does not
// contain an explicit title.
var EventSubjectToName = map[string]string{
	SubjectCheckResult:    "Check Result Triage",
	SubjectAlertEvents:    "Alert Investigation",
	SubjectAgentOnline:    "Agent Came Online",
	SubjectAgentOffline:   "Agent Went Offline",
	SubjectPolicyViolation: "Policy Violation Response",
	SubjectPatchStatus:    "Patch Deployment Status",
	SubjectScriptResult:   "Script Execution Result",
	SubjectShellSession:   "Remote Shell Session",
}

// EventSubjectToContextPrefix maps a NATS event subject to a
// deterministic context-ID prefix. Tasks generated from the same
// event type within a short window share a context so that an agent
// can correlate related work.
var EventSubjectToContextPrefix = map[string]string{
	SubjectCheckResult:    "ctx-check",
	SubjectAlertEvents:    "ctx-alert",
	SubjectAgentOnline:    "ctx-agent-online",
	SubjectAgentOffline:   "ctx-agent-offline",
	SubjectPolicyViolation: "ctx-policy",
	SubjectPatchStatus:    "ctx-patch",
	SubjectScriptResult:   "ctx-script",
	SubjectShellSession:   "ctx-shell",
}
