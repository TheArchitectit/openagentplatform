package mesh

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// Defaults for the agent-side WireGuard mesh interface.
const (
	// DefaultMeshInterface is the name of the userspace WireGuard TUN
	// device the agent creates.
	DefaultMeshInterface = "oapmesh0"
	// defaultMeshMTU is the WireGuard mesh MTU (1400 + 20/8 headroom).
	defaultMeshMTU = 1420
	// defaultMeshPort is the UDP listen port for the agent's mesh peer
	// if the server does not assign one.
	defaultMeshPort = 51820
)

// Key size of a WireGuard key: a 32-byte raw scalar, base64-encoded like
// `wg genkey` / `wg pubkey` emit.
const wgKeyBytes = 32

// MeshConfig is the agent's WireGuard interface configuration, published by
// the server on MeshConfigSubject(agentID). It describes the local interface
// (agent private key, address, listen port) and — when the fabric routes
// through a persistent gateway peer — a single peer entry. Operator sessions
// are requested on-demand (internal/mesh.Admission) and are NOT persisted in
// this structure; this is the standing fabric config, not a session grant.
type MeshConfig struct {
	// AgentID and OrgID scope the config. The agent rejects a config whose
	// AgentID differs from the subscribed subject's owner (defense in depth
	// on top of NATS authn).
	AgentID string `json:"agent_id"`
	OrgID   string `json:"org_id"`

	// Interface, when set, overrides the default TUN device name.
	Interface string `json:"interface,omitempty"`
	// PrivateKey is the agent's WireGuard private key, base64 (32 bytes).
	PrivateKey string `json:"private_key"`
	// Address is the local mesh address + prefix, e.g. "10.0.0.2/32".
	Address string `json:"address"`
	// ListenPort is the UDP port the mesh peer listens on. 0 = kernel-chosen
	// ephemeral (not valid for a server-facing agent; prefer an explicit port).
	ListenPort int `json:"listen_port,omitempty"`
	// MTU overrides the default mesh MTU.
	MTU int `json:"mtu,omitempty"`

	// Optional gateway peer the agent keeps a persistent tunnel to.
	PeerPublicKey  string   `json:"peer_public_key,omitempty"`
	PeerEndpoint   string   `json:"peer_endpoint,omitempty"`
	PeerAllowedIPs []string `json:"peer_allowed_ips,omitempty"`
	// PersistentKeepalive seconds; 0 disables the keepalive.
	PersistentKeepalive int `json:"persistent_keepalive,omitempty"`
}

// MeshConfigResult is the acknowledgement / bring-up status the agent
// publishes on MeshConfigResultSubject(agentID) after applying a config.
type MeshConfigResult struct {
	AgentID    string    `json:"agent_id"`
	OrgID      string    `json:"org_id"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
	Driver     string    `json:"driver"`
	ReceivedAt time.Time `json:"received_at"`
}

// ParseMeshConfig decodes + validates a server-issued MeshConfig JSON payload.
func ParseMeshConfig(data []byte) (*MeshConfig, error) {
	if len(data) == 0 {
		return nil, errors.New("mesh: empty config payload")
	}
	var c MeshConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("mesh: parse config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the config for integrity before any interface is touched.
// This is the same gate the driver relies on: a config that passes here is
// safe to hand to wireguard-go.
func (c *MeshConfig) Validate() error {
	if c.AgentID == "" {
		return errors.New("mesh: config missing agent_id")
	}
	if c.OrgID == "" {
		return errors.New("mesh: config missing org_id")
	}
	if err := validateWGKey(c.PrivateKey); err != nil {
		return fmt.Errorf("mesh: private_key: %w", err)
	}
	if c.Address == "" {
		return errors.New("mesh: config missing address")
	}
	if _, _, err := net.ParseCIDR(c.Address); err != nil {
		return fmt.Errorf("mesh: invalid address %q: %w", c.Address, err)
	}
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return fmt.Errorf("mesh: invalid listen_port %d", c.ListenPort)
	}
	if c.PeerPublicKey != "" {
		if err := validateWGKey(c.PeerPublicKey); err != nil {
			return fmt.Errorf("mesh: peer_public_key: %w", err)
		}
	}
	if c.PeerEndpoint != "" {
		if _, _, err := net.SplitHostPort(c.PeerEndpoint); err != nil {
			return fmt.Errorf("mesh: invalid peer_endpoint %q: %w", c.PeerEndpoint, err)
		}
	}
	for _, ip := range c.PeerAllowedIPs {
		if _, _, err := net.ParseCIDR(ip); err != nil {
			return fmt.Errorf("mesh: invalid peer allowed_ip %q", ip)
		}
	}
	return nil
}

func validateWGKey(b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("invalid base64: %w", err)
	}
	if len(raw) != wgKeyBytes {
		return fmt.Errorf("expected %d bytes, got %d", wgKeyBytes, len(raw))
	}
	return nil
}

// interfaceName resolves the TUN device name for this config.
func (c *MeshConfig) interfaceName() string {
	if c.Interface != "" {
		return c.Interface
	}
	return DefaultMeshInterface
}

// mtu resolves the MTU for this config.
func (c *MeshConfig) mtu() int {
	if c.MTU > 0 {
		return c.MTU
	}
	return defaultMeshMTU
}

// listenPort resolves the UDP port for this config.
func (c *MeshConfig) listenPort() int {
	if c.ListenPort > 0 {
		return c.ListenPort
	}
	return defaultMeshPort
}

// uapiConfig renders the wireguard-go IPC configuration for this mesh peer.
// It emits the local interface keys plus (when configured) the gateway peer.
func (c *MeshConfig) uapiConfig() string {
	var b []byte
	b = append(b, "private_key="...)
	b = append(b, c.PrivateKey...)
	b = append(b, '\n')

	port := strconv.Itoa(c.listenPort())
	b = append(b, "listen_port="...)
	b = append(b, port...)
	b = append(b, '\n')

	if c.PeerPublicKey != "" {
		b = append(b, "replace_peers=true\npublic_key="...)
		b = append(b, c.PeerPublicKey...)
		b = append(b, '\n')
		if c.PeerEndpoint != "" {
			b = append(b, "endpoint="...)
			b = append(b, c.PeerEndpoint...)
			b = append(b, '\n')
		}
		if len(c.PeerAllowedIPs) > 0 {
			for _, ip := range c.PeerAllowedIPs {
				b = append(b, "allowed_ip="...)
				b = append(b, ip...)
				b = append(b, '\n')
			}
		} else {
			// Default: route the whole mesh subnet through the gateway.
			b = append(b, "allowed_ip=0.0.0.0/0\n"...)
		}
		if c.PersistentKeepalive > 0 {
			b = append(b, "persistent_keepalive_interval="...)
			b = append(b, strconv.Itoa(c.PersistentKeepalive)...)
			b = append(b, '\n')
		}
	}
	return string(b)
}

// WireGuardDriver brings up and tears down the agent's mesh interface.
// Implementing it separates the host/device specifics (TUN, netstack, IP
// assignment) from the NATS-driven handler logic, which is what the unit
// tests exercise.
type WireGuardDriver interface {
	// Apply creates/reconfigures the mesh interface to match cfg. It is the
	// ONLY place TLS-fabric state is mutated; Apply must be safe to call
	// repeatedly (idempotent under reconfiguration).
	Apply(ctx context.Context, cfg *MeshConfig) error
	// Close tears down the interface and releases resources.
	Close() error
	// Name identifies the driver implementation for status reporting.
	Name() string
}
