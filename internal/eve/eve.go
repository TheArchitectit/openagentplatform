// Package eve implements the managed hypervisor (EVE) integration for
// OpenAgentPlatform. A dual surface: an OAP agent on each host supplies
// per-host metrics via the existing agent framework, and a server-side
// connector queries the cluster API (Proxmox / libvirt / vSphere) for
// VM/CT inventory, HA events, storage pools, and backup status.
package eve

import (
	"context"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// NodeInfo is the normalized view of a hypervisor host node.
type NodeInfo struct {
	Name      string
	Status    string // online | offline | running | stopped
	CPUCount  int
	MemoryMB  int64
	UptimeSec int64
}

// VMInfo is the normalized view of a virtual machine.
type VMInfo struct {
	ID       string
	Name     string
	Status   string // running | stopped | paused | suspended
	CPUCount int
	MemoryMB int64
	DiskGB   int64
	ParentID string
}

// ContainerInfo is the normalized view of an LXC container (Proxmox).
// libvirt/vSphere return empty for this surface.
type ContainerInfo struct {
	ID       string
	Name     string
	Status   string
	CPUCount int
	MemoryMB int64
	DiskGB   int64
	ParentID string
}

// StoragePoolInfo is the normalized view of a storage pool.
type StoragePoolInfo struct {
	Name    string
	Type    string // LVM | ZFS | NFS | Ceph | Dir
	Status  string
	TotalGB int64
	UsedGB  int64
}

// ClusterEvent is one event pulled from the cluster log.
type ClusterEvent struct {
	Type      string         // ha_failover | vm_started | vm_stopped | storage_warning
	Timestamp time.Time
	Payload   map[string]any
}

// HypervisorClient is the contract a hypervisor adapter must implement.
// It normalizes the host/VM/CT/storage/event surfaces across providers.
type HypervisorClient interface {
	// Name returns the provider identifier (e.g. "proxmox", "libvirt", "vsphere").
	Name() models.HypervisorProvider

	// ListNodes returns the hypervisor host nodes visible to this client.
	ListNodes(ctx context.Context) ([]NodeInfo, error)

	// ListVMs returns every VM on the host or cluster.
	ListVMs(ctx context.Context) ([]VMInfo, error)

	// ListContainers returns LXC containers (Proxmox only). Other providers
	// return nil.
	ListContainers(ctx context.Context) ([]ContainerInfo, error)

	// ListStoragePools returns storage pool info.
	ListStoragePools(ctx context.Context) ([]StoragePoolInfo, error)

	// ListRecentEvents returns events since the given time.
	ListRecentEvents(ctx context.Context, since time.Time) ([]ClusterEvent, error)
}
