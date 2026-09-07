package libvirt

import (
	"context"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/eve"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// LibvirtClient is a HypervisorClient adapter for libvirt-managed hosts.
//
// BUILD CONSTRAINT: This client requires the native libvirt C library
// (libvirt-dev / libvirt-devel) and the github.com/libvirt/libvirt-go
// Go bindings. The dependency is intentionally not wired into go.mod
// because the build environment lacks the native headers and the
// import would break `go build` on host systems without libvirt.
//
// To enable libvirt support:
//
//  1. Install the native headers:
//       Debian/Ubuntu: sudo apt install libvirt-dev pkg-config
//       RHEL/Fedora:    sudo dnf install libvirt-devel pkgconfig-pkg-config
//  2. Add the Go binding:
//       go get github.com/libvirt/libvirt-go
//  3. Replace this stub with the concrete implementation (see git
//     history; uses libvirt.NewConnect, conn.GetNodeInfo,
//     conn.ListAllDomains, conn.ListAllStoragePools)
//
// Until then, data methods return a clear "not compiled in" error so
// callers know the provider is registered but unbuilt.
type LibvirtClient struct {
	uri string
}

func NewLibvirtClient(uri string) *LibvirtClient {
	return &LibvirtClient{uri: uri}
}

func (c *LibvirtClient) Name() models.HypervisorProvider {
	return models.HypervisorLibvirt
}

func (c *LibvirtClient) notCompiled() error {
	return fmt.Errorf("libvirt: client not compiled in — install libvirt-dev + add github.com/libvirt/libvirt-go to enable")
}

func (c *LibvirtClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
	return nil, c.notCompiled()
}

func (c *LibvirtClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
	return nil, c.notCompiled()
}

// ListContainers returns nil: libvirt doesn't differentiate LXC containers
// from VMs at the API surface — both are Domains.
func (c *LibvirtClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
	return nil, nil
}

func (c *LibvirtClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
	return nil, c.notCompiled()
}

// ListRecentEvents returns nil: libvirt has no persistent event log over the wire.
func (c *LibvirtClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
	return nil, nil
}
