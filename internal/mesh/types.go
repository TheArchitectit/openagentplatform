package mesh

import "time"

// Purpose identifies why an operator opened a mesh session. The allow-list
// constrains what the signed SSH certificate may forward to, limiting blast
// radius if a grant leaks.
const (
	PurposeVNC   = "vnc"
	PurposeRDP   = "rdp"
	PurposeShell = "shell"
	PurposeUpdate = "update"
	PurposeFile  = "file"
)

// validPurposes is the closed set accepted by Admission.RequestSession.
var validPurposes = map[string]struct{}{
	PurposeVNC:   {},
	PurposeRDP:   {},
	PurposeShell: {},
	PurposeUpdate: {},
	PurposeFile:  {},
}

// ValidPurpose reports whether p is in the allow-list.
func ValidPurpose(p string) bool {
	_, ok := validPurposes[p]
	return ok
}

// SessionGrant is the bundle an operator client needs to open a tunnel to an
// agent: WireGuard peer config for the operator side plus the SSH user
// certificate the agent's SSH server will accept.
type SessionGrant struct {
	SessionID                string    `json:"session_id"`
	AgentID                  string    `json:"agent_id"`
	AgentMeshIP              string    `json:"agent_mesh_ip"`
	OperatorMeshIP           string    `json:"operator_mesh_ip"`
	OperatorPrivateKeyWGB64  string    `json:"operator_private_key_wg_b64"`
	AgentPublicKeyWGB64      string    `json:"agent_public_key_wg_b64"`
	SSHCertPEM               string    `json:"ssh_cert_pem"`
	SSHCAPublicKeyPEM        string    `json:"ssh_ca_public_key_pem"`
	ExpiresAt                time.Time `json:"expires_at"`
}

// WireGuardConfig describes one side of a mesh peer link. The operator client
// uses OperatorPrivateKey/OperatorIP; PeerPublicKey/PeerIP identify the agent
// endpoint the operator connects through.
type WireGuardConfig struct {
	OperatorPrivateKey string `json:"operator_private_key"`
	OperatorIP         string `json:"operator_ip"`
	PeerPublicKey      string `json:"peer_public_key"`
	PeerIP             string `json:"peer_ip"`
	ListenPort         int    `json:"listen_port"`
}
