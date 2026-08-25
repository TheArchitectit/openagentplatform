package mesh

import "fmt"

// Subject builders for the agent side of the RMM-09 tunnel fabric.
//
// These mirror internal/mesh's subject builders exactly (the server and
// agent must agree on the wire namespace). They live in pkg/agent/mesh
// rather than importing internal/mesh so the agent binary does not drag in
// the server-side store / KeyManager / pgx dependency tree just to learn a
// subject string. The namespace is the existing oap.agents.<id>.* one; the
// fabric uses no rmm.winupdate.* subjects.

// MeshConfigSubject is the subject the server publishes this agent's
// WireGuard configuration to. The agent subscribes on it at bring-up.
func MeshConfigSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.mesh.config", agentID)
}

// MeshConfigResultSubject is the subject the agent publishes its mesh
// config acknowledgement / bring-up status back on.
func MeshConfigResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.mesh.config.result", agentID)
}

// UpdateSubject is the subject the control plane publishes a pinned-release
// notice to. The agent subscribes and, if the version is newer, verifies the
// Ed25519 signature before applying. The agent NEVER fetches over the public
// net — it only acts on control-plane-pushed notices (which ride NATS mTLS).
func UpdateSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.mesh.update", agentID)
}

// UpdateStatusSubject is the subject the agent publishes self-update status on
// (e.g. "staged", "refused", "applied").
func UpdateStatusSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.mesh.update.status", agentID)
}
