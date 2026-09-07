package vsphere

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/openagentplatform/openagentplatform/internal/eve"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// VSphereClient is a HypervisorClient adapter for VMware vCenter/ESXi.
type VSphereClient struct {
	endpoint string // e.g. https://vcenter.example.com/sdk
	insecure bool
	user     string
	password string
}

func NewVSphereClient(endpoint, user, password string, insecure bool) *VSphereClient {
	return &VSphereClient{endpoint: endpoint, user: user, password: password, insecure: insecure}
}

func (c *VSphereClient) Name() models.HypervisorProvider {
	return models.HypervisorVSphere
}

func (c *VSphereClient) client(ctx context.Context) (*govmomi.Client, func(), error) {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("vsphere parse endpoint %q: %w", c.endpoint, err)
	}
	if c.user != "" {
		u.User = url.UserPassword(c.user, c.password)
	}
	client, err := govmomi.NewClient(ctx, u, c.insecure)
	if err != nil {
		return nil, nil, fmt.Errorf("vsphere connect: %w", err)
	}
	return client, func() { _ = client.Logout(ctx) }, nil
}

func (c *VSphereClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
	client, logout, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	defer logout()

	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		return nil, fmt.Errorf("vsphere CreateContainerView(HostSystem): %w", err)
	}
	defer v.Destroy(ctx)

	var hosts []mo.HostSystem
	if err := v.Retrieve(ctx, []string{"HostSystem"}, []string{"summary", "runtime"}, &hosts); err != nil {
		return nil, fmt.Errorf("vsphere Retrieve(HostSystem): %w", err)
	}
	nodes := make([]eve.NodeInfo, 0, len(hosts))
	for _, h := range hosts {
		nodes = append(nodes, eve.NodeInfo{
			Name:      h.Name,
			Status:    string(h.Runtime.ConnectionState),
			CPUCount:  int(h.Summary.Hardware.NumCpuCores),
			MemoryMB:  int64(h.Summary.Hardware.MemorySize) / (1024 * 1024),
			UptimeSec: int64(h.Summary.QuickStats.Uptime),
		})
	}
	return nodes, nil
}

func (c *VSphereClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
	client, logout, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	defer logout()

	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("vsphere CreateContainerView(VirtualMachine): %w", err)
	}
	defer v.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"summary", "config", "runtime"}, &vms); err != nil {
		return nil, fmt.Errorf("vsphere Retrieve(VirtualMachine): %w", err)
	}
	out := make([]eve.VMInfo, 0, len(vms))
	for _, vm := range vms {
		diskGB := int64(0)
		for _, d := range vm.Config.Hardware.Device {
			if disk, ok := d.(*types.VirtualDisk); ok {
				diskGB += disk.CapacityInKB / (1024 * 1024)
			}
		}
		out = append(out, eve.VMInfo{
			ID:       vm.Config.InstanceUuid,
			Name:     vm.Name,
			Status:   powerState(vm.Runtime.PowerState),
			CPUCount: int(vm.Config.Hardware.NumCPU),
			MemoryMB: int64(vm.Config.Hardware.MemoryMB),
			DiskGB:   diskGB,
		})
	}
	return out, nil
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

// ListRecentEvents returns nil: pulling from the vCenter task/event log is
// out of scope for this iteration.
func (c *VSphereClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
	return nil, nil
}

func powerState(s types.VirtualMachinePowerState) string {
	switch s {
	case types.VirtualMachinePowerStatePoweredOn:
		return "running"
	case types.VirtualMachinePowerStatePoweredOff:
		return "stopped"
	case types.VirtualMachinePowerStateSuspended:
		return "suspended"
	default:
		return "unknown"
	}
}
