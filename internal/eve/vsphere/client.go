package vsphere

import (
	"context"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/eve"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// VSphereClient is a HypervisorClient adapter for VMware vCenter/ESXi.
// The concrete govmomi-backed implementation is built when the
// github.com/vmware/govmomi dependency is added; this stub returns a
// clear error so callers know the provider is registered but unbuilt.
type VSphereClient struct {
	host string
}

func NewVSphereClient(host string) *VSphereClient {
	return &VSphereClient{host: host}
}

func (c *VSphereClient) Name() models.HypervisorProvider {
	return models.HypervisorVSphere
}

func (c *VSphereClient) notBuilt() error {
	return fmt.Errorf("vsphere: provider registered but not built; add github.com/vmware/govmomi dependency to enable")
}

func (c *VSphereClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
	return nil, c.notBuilt()
}

func (c *VSphereClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
	return nil, c.notBuilt()
}

// ListContainers returns nil: vSphere has no separate container surface.
func (c *VSphereClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
	return nil, nil
}

// ListStoragePools returns nil: vSphere handles storage via datastore views
// which are out of scope for this adapter.
func (c *VSphereClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
	return nil, nil
}

// ListRecentEvents returns nil; the concrete impl will pull from the vCenter
// task/event log when built.
func (c *VSphereClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
	return nil, nil
}
