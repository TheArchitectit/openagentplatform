package models

import "time"

// HypervisorProvider is the kind of hypervisor platform a cluster runs.
type HypervisorProvider string

const (
	HypervisorProxmox HypervisorProvider = "proxmox"
	HypervisorLibvirt HypervisorProvider = "libvirt"
	HypervisorVSphere HypervisorProvider = "vsphere"
)

// String returns the canonical lowercase identifier.
func (p HypervisorProvider) String() string { return string(p) }

// HypervisorCluster is a registered hypervisor cluster (one of: Proxmox
// cluster, libvirt host, vSphere). The host agent on the primary node
// (if any) is referenced by PrimaryAgentID.
type HypervisorCluster struct {
	ID             string             `json:"id"`
	OrgID          string             `json:"org_id"`
	Provider       HypervisorProvider `json:"provider"`
	Name           string             `json:"name"`
	Endpoint       string             `json:"endpoint"`
	CredentialRef  string             `json:"credential_ref"`
	PrimaryAgentID string             `json:"primary_agent_id,omitempty"`
	Tags           []string           `json:"tags"`
	Enabled        bool               `json:"enabled"`
	LastSeen       time.Time          `json:"last_seen"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// HypervisorResource is a child of a cluster: a VM, container, host node,
// storage pool, or network. ResourceType is one of: "vm", "ct", "node",
// "storage", "network".
type HypervisorResource struct {
	ID           string     `json:"id"`
	OrgID        string     `json:"org_id"`
	ClusterID    string     `json:"cluster_id"`
	ResourceID   string     `json:"resource_id"`
	ResourceType string     `json:"resource_type"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	ParentID     string     `json:"parent_resource_id,omitempty"`
	CPUCount     int        `json:"cpu_count,omitempty"`
	MemoryMB     int64      `json:"memory_mb,omitempty"`
	DiskGB       int64      `json:"disk_gb,omitempty"`
	LastSeen     time.Time  `json:"last_seen"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// HypervisorEvent is an audit record of a cluster event (HA failover, VM
// start/stop, storage warning, backup outcome). Append-only.
type HypervisorEvent struct {
	ID         string         `json:"id"`
	OrgID      string         `json:"org_id"`
	ClusterID  string         `json:"cluster_id"`
	EventType  string         `json:"event_type"`
	Payload    map[string]any `json:"payload"`
	OccurredAt time.Time      `json:"occurred_at"`
	IngestedAt time.Time      `json:"ingested_at"`
}
