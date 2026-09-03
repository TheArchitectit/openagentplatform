package libvirt

import (
	"context"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/eve"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// LibvirtClient is a HypervisorClient adapter for libvirt-managed hosts.
// It connects to a libvirt URI (qemu:///system or qemu+ssh://...).
type LibvirtClient struct {
	uri string
}

func NewLibvirtClient(uri string) *LibvirtClient {
	return &LibvirtClient{uri: uri}
}

func (c *LibvirtClient) Name() models.HypervisorProvider {
	return models.HypervisorLibvirt
}

// connect is a thin wrapper that returns a connection factory. The libvirt-go
// library requires a build tag (libvirt.org/libvirt-go) to compile against
// the C bindings. We expose a no-op stub here; concrete libvirt support is
// built when the libvirt-go dependency is wired in via build flags.
func (c *LibvirtClient) connect() error {
	return fmt.Errorf("libvirt: client not compiled in; add libvirt.org/libvirt-go dependency and build tag to enable")
}

func (c *LibvirtClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
	return nil, c.connect()
}

func (c *LibvirtClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
	return nil, c.connect()
}

// ListContainers returns nil: libvirt doesn't differentiate LXC containers
// from VMs at the API surface — they are all Domains.
func (c *LibvirtClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
	return nil, nil
}

func (c *LibvirtClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
	return nil, c.connect()
}

// ListRecentEvents returns nil: libvirt has no persistent event log over the wire.
func (c *LibvirtClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
	return nil, nil
}
