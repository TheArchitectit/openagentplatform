//go:build mesh

package mesh

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// compile-time assert that wgDriver satisfies the driver contract.
var _ WireGuardDriver = (*wgDriver)(nil)

// wgDriver brings up the agent's WireGuard mesh interface using the embedded
// userspace wireguard-go library (the RMM-09 BUILD DECISION: no host `wg`
// tooling, cross-platform). It uses netstack for the tunnel so the mesh
// address lives in-process: no netlink, no `ip`/`ifconfig`/`netsh`, no root,
// no TUN device on the host. The same *netstack.Net is retained so the SSH
// server (RMM-09 step 4) can ListenTCPAddrPort on the mesh IP.
type wgDriver struct {
	log *slog.Logger

	mu     sync.Mutex
	dev    *device.Device
	iface  tun.Device
	tnet   *netstack.Net
	meshIP netip.Addr
}

// defaultDriver returns a wireguard-go driver for the mesh build tag.
func defaultDriver(log *slog.Logger) WireGuardDriver {
	if log == nil {
		log = slog.Default()
	}
	return &wgDriver{log: log}
}

// Name returns "wireguard-go".
func (d *wgDriver) Name() string { return "wireguard-go" }

// Apply tears down any existing interface and brings up a fresh one for cfg.
// It is idempotent under reconfiguration: a second Apply restarts the device
// with the new config.
func (d *wgDriver) Apply(ctx context.Context, cfg *MeshConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Tear down whatever is currently running so a reconfiguration starts
	// from a clean slate.
	if d.dev != nil {
		d.dev.Close()
		d.dev = nil
	}

	prefix, err := netip.ParsePrefix(cfg.Address)
	if err != nil {
		return fmt.Errorf("mesh: parse address %q: %w", cfg.Address, err)
	}

	iface, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{prefix.Addr()},
		nil, // no in-process DNS servers
		cfg.mtu(),
	)
	if err != nil {
		return fmt.Errorf("mesh: create netstack tun: %w", err)
	}

	logger := device.NewLogger(device.LogLevelError, "oap-agent-mesh")
	dev := device.NewDevice(iface, conn.NewDefaultBind(), logger)

	// Apply the peer/interface configuration via wireguard-go's UAPI IPC.
	if err := dev.IpcSet(cfg.uapiConfig()); err != nil {
		dev.Close()
		iface.Close()
		return fmt.Errorf("mesh: ipc set config: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		iface.Close()
		return fmt.Errorf("mesh: bring device up: %w", err)
	}

	d.iface = iface
	d.tnet = tnet
	d.dev = dev
	d.meshIP = prefix.Addr()

	d.log.Info("mesh: wireguard interface up",
		"interface", cfg.interfaceName(),
		"address", cfg.Address,
		"listen_port", cfg.listenPort(),
		"peer", cfg.PeerPublicKey != "")
	return nil
}

// Close tears down the WireGuard device and its interface.
func (d *wgDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dev != nil {
		d.dev.Close()
		d.dev = nil
	}
	if d.iface != nil {
		d.iface.Close()
		d.iface = nil
	}
	d.tnet = nil
	return nil
}

// Net returns the in-process network stack, or nil if the mesh is not up.
// The SSH/VNC/RDP data-plane listeners bind through this stack on the mesh
// IP (RMM-09 step 4).
func (d *wgDriver) Net() *netstack.Net {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tnet
}

// MeshIP returns the agent's mesh address, or the zero Addr if not up.
func (d *wgDriver) MeshIP() netip.Addr {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.meshIP
}
