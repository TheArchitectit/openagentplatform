package mesh

import "fmt"

// Subject builders for the RMM-09 tunnel fabric. All agent-scoped
// subjects live under the existing oap.agents.<id>.* namespace; mesh
// coordination subjects use the oap.mesh.* namespace. No rmm.winupdate.*
// subjects are used — the tunnel fabric is its own concern.

// MeshConfigSubject is the subject the server publishes a mesh peer's
// WireGuard configuration to. The agent subscribes on this subject.
func MeshConfigSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.mesh.config", agentID)
}

// MeshConfigResultSubject is the subject the agent publishes its mesh
// config acknowledgement / status back on.
func MeshConfigResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.mesh.config.result", agentID)
}

// MeshSessionRequestSubject is the server-internal subject used to
// request a new operator tunnel session (RBAC + admission).
func MeshSessionRequestSubject() string {
	return "oap.mesh.session.request"
}
