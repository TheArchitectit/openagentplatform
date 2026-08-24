package events

const (
	// SubjectHeartbeatPrefix is the wildcard subject every agent publishes
	// heartbeats on. Each agent's full subject is
	// oap.agents.<agent_id>.heartbeat.
	SubjectHeartbeatPrefix = "oap.agents.*.heartbeat"

	// SubjectCheckResultsPrefix is the wildcard subject agents publish
	// check results on. SubjectCheckResultPrefix is an alias used by the
	// ingest pipeline — both point to the same NATS subject.
	SubjectCheckResultsPrefix = "oap.agents.*.results"

	// SubjectAgentEvents is where the server publishes lifecycle events
	// (AgentOnline, AgentOffline, etc.) for downstream consumers.
	SubjectAgentEvents = "oap.events.agent"

	// SubjectCheckAssignmentPrefix is the per-agent subject check
	// assignments are published on.
	SubjectCheckAssignmentPrefix = "oap.agents"

	// SubjectCheckResultPrefix is the wildcard subject the check result
	// ingest pipeline subscribes to. It is an alias for
	// SubjectCheckResultsPrefix, kept for backward compatibility.
	SubjectCheckResultPrefix = SubjectCheckResultsPrefix

	// SubjectAlertEvents is the wildcard subject the threshold evaluator
	// publishes alert lifecycle events on. Consumers (WebSocket hub, pager
	// integrations) subscribe to this subject to receive AlertFired /
	// AlertResolved notifications.
	SubjectAlertEvents = "oap.events.alerts"

	// SubjectCheckResultEvent is the wildcard subject the ingest pipeline
	// publishes to whenever a new check result is persisted. The WebSocket
	// hub subscribes here to broadcast live result updates to connected
	// dashboards.
	SubjectCheckResultEvent = "oap.events.checks.result"

	// SubjectPatchEvents is the wildcard subject the patch management
	// subsystem publishes to whenever a patch is approved, deployed,
	// rolled back, or its status changes. The WebSocket hub subscribes
	// here to broadcast live patch updates to connected dashboards.
	SubjectPatchEvents = "oap.events.patches"

	// SubjectScriptEvents is the wildcard subject the script execution
	// subsystem publishes to whenever a script is run, completes, or
	// its status changes. The WebSocket hub subscribes here to broadcast
	// live script updates to connected dashboards.
	SubjectScriptEvents = "oap.events.scripts"
)

// HeartbeatStaleThreshold is the duration after which a silent agent is
// considered offline.
const HeartbeatStaleThreshold = 120 * 1_000_000_000 // 120s in ns; kept as a hint for callers using time.Duration elsewhere
