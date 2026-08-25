//go:build !mesh

package mesh

import (
	"context"
	"log/slog"
)

// noopDriver is the default WireGuardDriver when the agent is NOT compiled
// with the `mesh` build tag (or runs on a host without TUN support). It does
// not create an interface; instead it logs clearly that the data plane is
// disabled so the agent keeps running control-plane workloads (heartbeats,
// checks, patches) without a tunnel. The server's MeshConfigResult will carry
// driver="noop" so an operator can see the agent is not on the fabric.
type noopDriver struct {
	log *slog.Logger
}

// Apply logs the config and returns nil (a no-op "success").
func (d *noopDriver) Apply(ctx context.Context, cfg *MeshConfig) error {
	d.log.Warn("mesh: data plane disabled in this build; not bringing up WireGuard",
		"agent_id", cfg.AgentID,
		"org_id", cfg.OrgID,
		"reason", "no mesh build tag / TUN driver")
	return nil
}

// Close is a no-op.
func (d *noopDriver) Close() error { return nil }

// Name returns "noop".
func (d *noopDriver) Name() string { return "noop" }

// defaultDriver returns the build's default WireGuardDriver.
func defaultDriver(log *slog.Logger) WireGuardDriver {
	if log == nil {
		log = slog.Default()
	}
	return &noopDriver{log: log}
}
