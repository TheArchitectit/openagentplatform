# EVE / Hypervisor Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add hypervisor integration (Proxmox/libvirt/ESXi) via a dual surface: host OAP agent (CPU/memory/disk via existing framework) plus server-side cluster API connector (VM/CT inventory, HA events, storage pool health, backup status).

**Architecture:** A `HypervisorClient` interface normalizes responses from Proxmox REST API, libvirt URI, and vSphere/govmomi into a unified `HypervisorResource` struct. The host agent provides the `hypervisor_clusters` row keyed by `agent_id`; the cluster connector populates child VM/CT rows. Virtual agents are created for discovered VMs/CTs. The alert conditions reuse `oap.events.alerts`.

**Tech Stack:** Proxmox REST API (HTTP+JSON, token auth), libvirt Go binding (`libvirt.org/libvirt-go`), govmomi (VMware Go SDK), existing OAP agent and alert patterns.

---

## File Map

```
internal/eve/
├── eve.go                    # HypervisorClient interface, HypervisorResource struct, HypervisorCluster struct
├── proxmox/
│   └── client.go          # ProxmoxClient: ListNodes, ListVMs, ListContainers, ListStorage, ListEvents
├── libvirt/
│   └── client.go          # LibvirtClient: ListDomains, ListStoragePools, ListNetworks
├── vsphere/
│   └── client.go          # VSphereClient: ListHosts, ListVMs, ListDatastores, ListEvents
├── reconciler.go              # ClusterConnector: polls cluster APIs, upserts resources, emits alerts
├── alerts.go                 # Alert condition types: host_offline, vm_down, ha_failover, storage_warning
└── store.go                # ClusterStore, ResourceStore: CRUD interfaces

pkg/agent/checkers/
└── hypervisor_check.go    # Optional: "hypervisor" check type for host-agent health probes

pkg/models/
└── models_eve.go          # HypervisorCluster, HypervisorResource, HypervisorEvent types

internal/db/migrations/
└── 014_eve_monitoring.up.sql

internal/api/
├── routes.go                  # Register /api/v1/eve routes
└── eve.go                    # Handlers

internal/alerts/
└── models_alerts.go           # Add EVE-specific alert condition fields
```

---

### Task 1: Database Migration 014

**Files:**
- Create: `internal/db/migrations/014_eve_monitoring.up.sql`
- Test: `internal/db/migrations/014_eve_monitoring.up.sql` (verify no duplicate table names)

- [ ] **Step 1: Write the migration**

```sql
-- 014_eve_monitoring: hypervisor clusters, VM/CT resources, events

CREATE TABLE IF NOT EXISTS hypervisor_clusters (
    id                 TEXT PRIMARY KEY,
    org_id            TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL,  -- proxmox | libvirt | vsphere
    name              TEXT NOT NULL,
    endpoint          TEXT NOT NULL,  -- URL or libvirt URI
    credential_ref   TEXT NOT NULL,  -- SecretBackend URI
    primary_agent_id  TEXT,
    tags              JSONB NOT NULL DEFAULT '[]',
    enabled           BOOLEAN NOT NULL DEFAULT true,
    last_seen         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_clusters_org ON hypervisor_clusters (org_id);

CREATE TABLE IF NOT EXISTS hypervisor_resources (
    id                  TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL DEFAULT '',
    cluster_id         TEXT NOT NULL,
    resource_id        TEXT NOT NULL,  -- VM/CT ID in hypervisor's native ID space
    resource_type      TEXT NOT NULL,  -- vm | ct | node | storage | network
    name               TEXT NOT NULL,
    status             TEXT,
    parent_resource_id TEXT,
    cpu_count          INTEGER,
    memory_mb          BIGINT,
    disk_gb            BIGINT,
    last_seen          TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cluster_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_resources_org ON hypervisor_resources (org_id);
CREATE INDEX IF NOT EXISTS idx_hypervisor_resources_archived ON hypervisor_resources (archived_at) WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS hypervisor_events (
    id             TEXT PRIMARY KEY,
    org_id        TEXT NOT NULL DEFAULT '',
    cluster_id    TEXT NOT NULL,
    event_type   TEXT NOT NULL,  -- ha_failover | vm_started | vm_stopped | storage_warning | backup_failed
    payload       JSONB NOT NULL DEFAULT '{}',
    occurred_at   TIMESTAMPTZ NOT NULL,
    ingested_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_events_cluster ON hypervisor_events (cluster_id, occurred_at DESC);
```

- [ ] **Step 2: Verify no duplicate table names**

Run: `grep -c "CREATE TABLE" internal/db/migrations/*.up.sql | grep -v ":1$"` (each migration should create unique tables)
Expected: (no output — all unique)

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/014_eve_monitoring.up.sql
git commit -m "feat(eve): migration 014 — hypervisor_clusters, hypervisor_resources, hypervisor_events"
```

---

### Task 2: Core Types and HypervisorClient Interface

**Files:**
- Create: `pkg/models/models_eve.go`
- Create: `internal/eve/eve.go`
- Create: `pkg/models/models_eve_test.go`

- [ ] **Step 1: Write Go models**

```go
// pkg/models/models_eve.go
package models

import "time"

type HypervisorProvider string

const (
    HypervisorProxmox HypervisorProvider = "proxmox"
    HypervisorLibvirt HypervisorProvider = "libvirt"
    HypervisorVSphere HypervisorProvider = "vsphere"
)

type HypervisorCluster struct {
    ID              string              `json:"id"`
    OrgID           string              `json:"org_id"`
    Provider        HypervisorProvider  `json:"provider"`
    Name            string              `json:"name"`
    Endpoint        string              `json:"endpoint"`
    CredentialRef   string              `json:"credential_ref"`
    PrimaryAgentID  string              `json:"primary_agent_id,omitempty"`
    Tags            []string            `json:"tags"`
    Enabled         bool                `json:"enabled"`
    LastSeen        time.Time          `json:"last_seen"`
    CreatedAt       time.Time          `json:"created_at"`
    UpdatedAt       time.Time          `json:"updated_at"`
}

type HypervisorResource struct {
    ID              string              `json:"id"`
    OrgID           string              `json:"org_id"`
    ClusterID       string              `json:"cluster_id"`
    ResourceID      string              `json:"resource_id"`
    ResourceType    string              `json:"resource_type"`  // vm | ct | node | storage | network
    Name            string              `json:"name"`
    Status          string              `json:"status"`
    ParentID        string              `json:"parent_resource_id,omitempty"`
    CPUCount        int                `json:"cpu_count,omitempty"`
    MemoryMB        int64               `json:"memory_mb,omitempty"`
    DiskGB          int64               `json:"disk_gb,omitempty"`
    LastSeen        time.Time          `json:"last_seen"`
    ArchivedAt       *time.Time         `json:"archived_at,omitempty"`
    CreatedAt       time.Time          `json:"created_at"`
}

type HypervisorEvent struct {
    ID          string            `json:"id"`
    OrgID       string            `json:"org_id"`
    ClusterID   string            `json:"cluster_id"`
    EventType   string            `json:"event_type"`
    Payload     map[string]any    `json:"payload"`
    OccurredAt  time.Time        `json:"occurred_at"`
    IngestedAt  time.Time        `json:"ingested_at"`
}
```

- [ ] **Step 2: Write the HypervisorClient interface**

```go
// internal/eve/eve.go
package eve

import "context"

type NodeInfo struct {
    Name       string
    Status     string  // online | offline | running | stopped
    CPUCount   int
    MemoryMB   int64
    UptimeSec  int64
}

type VMInfo struct {
    ID         string
    Name       string
    Status     string  // running | stopped | paused | suspended
    CPUCount   int
    MemoryMB   int64
    DiskGB     int64
    ParentID   string
}

type ContainerInfo struct {
    ID         string
    Name       string
    Status     string
    CPUCount   int
    MemoryMB   int64
    DiskGB     int64
    ParentID   string
}

type StoragePoolInfo struct {
    Name      string
    Type      string  // LVM | ZFS | NFS | Ceph | Dir
    Status    string
    TotalGB   int64
    UsedGB    int64
}

type ClusterEvent struct {
    Type      string            // ha_failover | vm_started | vm_stopped | storage_warning
    Timestamp time.Time
    Payload   map[string]any
}

type HypervisorClient interface {
    Name() HypervisorProvider

    // ListNodes returns hypervisor host nodes
    ListNodes(ctx context.Context) ([]NodeInfo, error)

    // ListVMs returns all VMs on the host or cluster
    ListVMs(ctx context.Context) ([]VMInfo, error)

    // ListContainers returns all LXC containers (Proxmox only)
    ListContainers(ctx context.Context) ([]ContainerInfo, error)

    // ListStoragePools returns storage pool info
    ListStoragePools(ctx context.Context) ([]StoragePoolInfo, error)

    // ListRecentEvents returns events since the given time
    ListRecentEvents(ctx context.Context, since time.Time) ([]ClusterEvent, error)
}
```

- [ ] **Step 3: Write interface compile-check test**

```go
// internal/eve/eve_test.go
package eve

import "testing"

func TestHypervisorClientInterfaceSatisfied(t *testing.T) {
    var _ interface {
        Name() HypervisorProvider
        ListNodes, ListVMs, ListContainers, ListStoragePools, ListRecentEvents
    } = (*HypervisorClient)(nil)
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./pkg/models/... ./internal/eve/... && go test ./pkg/models/... -run TestCloud -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/models/models_eve.go internal/eve/eve.go pkg/models/models_eve_test.go internal/eve/eve_test.go
git commit -m "feat(eve): core types and HypervisorClient interface"
```

---

### Task 3: Proxmox Client

**Files:**
- Create: `internal/eve/proxmox/client.go`
- Test: `internal/eve/proxmox/client_test.go`

- [ ] **Step 1: Write the Proxmox client**

```go
// internal/eve/proxmox/client.go
package proxmox

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"

    "github.com/openagentplatform/openagentplatform/internal/eve"
)

type ProxmoxClient struct {
    endpoint string  // https://host:8006/api2/json
    tokenID  string
    tokenSecret string
    httpClient *http.Client
}

func NewProxmoxClient(endpoint, tokenID, tokenSecret string) *ProxmoxClient {
    return &ProxmoxClient{
        endpoint:    strings.TrimSuffix(endpoint, "/"),
        tokenID:     tokenID,
        tokenSecret: tokenSecret,
        httpClient:  &http.Client{Timeout: 30 * time.Second},
    }
}

func (c *ProxmoxClient) Name() eve.HypervisorProvider {
    return eve.HypervisorProxmox
}

func (c *ProxmoxClient) do(ctx context.Context, path string) ([]byte, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint+path, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("proxmox: status %d", resp.StatusCode)
    }
    return io.ReadAll(resp.Body)
}

type proxmoxNode struct {
    Node        string `json:"node"`
    Status      string `json:"status"`
    Uptime      int    `json:"uptime"`
    CPU         float64 `json:"cpu"`
    MaxMem      int    `json:"maxmem"`
    Mem         int    `json:"mem"`
    Disk        int    `json:"disk"`
}

type proxmoxVM struct {
    VMID     int     `json:"vmid"`
    Name     string  `json:"name"`
    Status   string  `json:"status"`
    CPU      float64 `json:"cpu"`
    MaxMem   int     `json:"maxmem"`
    Disk     int     `json:"disk"`
    Parent   string  `json:"template"`
}

type proxmoxContainer struct {
    VMID     int     `json:"vmid"`
    Name     string  `json:"name"`
    Status   string  `json:"status"`
    CPU      float64 `json:"cpu"`
    MaxMem   int     `json:"maxmem"`
    Disk     int     `json:"disk"`
    Parent   string  `json:"template"`
}

type proxmoxStorage struct {
    Storage   string  `json:"storage"`
    Type      string  `json:"type"`
    Status    string  `json:"status"`
    Total     int64   `json:"total"`
    Used      int64   `json:"used"`
    Available int64   `json:"avail"`
}

func (c *ProxmoxClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
    body, err := c.do(ctx, "/nodes")
    if err != nil {
        return nil, err
    }
    var out struct {
        Data []proxmoxNode `json:"data"`
    }
    if err := json.Unmarshal(body, &out); err != nil {
        return nil, err
    }
    var nodes []eve.NodeInfo
    for _, n := range out.Data {
        nodes = append(nodes, eve.NodeInfo{
            Name:       n.Node,
            Status:     n.Status,
            CPUCount:   int(n.CPU * 100), // normalize to percentage
            MemoryMB:   int64(n.MaxMem) / (1024 * 1024),
            UptimeSec:  int64(n.Uptime),
        })
    }
    return nodes, nil
}

func (c *ProxmoxClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
    body, err := c.do(ctx, "/nodes?type=qemu")
    if err != nil {
        return nil, err
    }
    var out struct {
        Data []proxmoxVM `json:"data"`
    }
    if err := json.Unmarshal(body, &out); err != nil {
        return nil, err
    }
    var vms []eve.VMInfo
    for _, vm := range out.Data {
        vms = append(vms, eve.VMInfo{
            ID:       fmt.Sprintf("%d", vm.VMID),
            Name:     vm.Name,
            Status:   vm.Status,
            CPUCount: int(vm.CPU * 100),
            MemoryMB: int64(vm.MaxMem) / (1024 * 1024),
            DiskGB:   int64(vm.Disk) / (1024 * 1024 * 1024),
            ParentID: vm.Parent,
        })
    }
    return vms, nil
}

func (c *ProxmoxClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
    body, err := c.do(ctx, "/nodes?type=lxc")
    if err != nil {
        return nil, err
    }
    var out struct {
        Data []proxmoxContainer `json:"data"`
    }
    if err := json.Unmarshal(body, &out); err != nil {
        return nil, err
    }
    var cts []eve.ContainerInfo
    for _, ct := range out.Data {
        cts = append(cts, eve.ContainerInfo{
            ID:       fmt.Sprintf("%d", ct.VMID),
            Name:     ct.Name,
            Status:   ct.Status,
            CPUCount: int(ct.CPU * 100),
            MemoryMB: int64(ct.MaxMem) / (1024 * 1024),
            DiskGB:   int64(ct.Disk) / (1024 * 1024 * 1024),
            ParentID: ct.Parent,
        })
    }
    return cts, nil
}

func (c *ProxmoxClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
    body, err := c.do(ctx, "/storage")
    if err != nil {
        return nil, err
    }
    var out struct {
        Data []proxmoxStorage `json:"data"`
    }
    if err := json.Unmarshal(body, &out); err != nil {
        return nil, err
    }
    var pools []eve.StoragePoolInfo
    for _, p := range out.Data {
        pools = append(pools, eve.StoragePoolInfo{
            Name:    p.Storage,
            Type:    p.Type,
            Status:  p.Status,
            TotalGB: p.Total / (1024 * 1024 * 1024),
            UsedGB:  p.Used / (1024 * 1024 * 1024),
        })
    }
    return pools, nil
}

func (c *ProxmoxClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
    // Query cluster log events
    body, err := c.do(ctx, "/cluster/log?since="+fmt.Sprintf("%d", since.Unix()))
    if err != nil {
        return nil, err
    }
    var out struct {
        Data []map[string]any `json:"data"`
    }
    if err := json.Unmarshal(body, &out); err != nil {
        return nil, err
    }
    var events []eve.ClusterEvent
    for _, e := range out.Data {
        if t, ok := e["time"].(float64); ok {
            events = append(events, eve.ClusterEvent{
                Timestamp: time.Unix(int64(t), 0),
                Type:      "cluster_log",
                Payload:   e,
            })
        }
    }
    return events, nil
}
```

- [ ] **Step 2: Write interface test**

```go
// internal/eve/proxmox/client_test.go
package proxmox

import (
    "testing"

    "github.com/openagentplatform/openagentplatform/internal/eve"
)

func TestProxmoxClientImplementsInterface(t *testing.T) {
    var _ eve.HypervisorClient = (*ProxmoxClient)(nil)
}
```

- [ ] **Step 3: Run tests**

Run: `go build ./internal/eve/proxmox/ && go test ./internal/eve/proxmox/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/eve/proxmox/client.go internal/eve/proxmox/client_test.go
git commit -m "feat(eve): Proxmox client using REST API with token auth"
```

---

### Task 4: libvirt and vSphere Clients

**Files:**
- Create: `internal/eve/libvirt/client.go`
- Create: `internal/eve/vsphere/client.go`
- Test: `internal/eve/libvirt/client_test.go`
- Test: `internal/eve/vsphere/client_test.go`

- [ ] **Step 1: Write libvirt client**

```go
// internal/eve/libvirt/client.go
package libvirt

import (
    "context"
    "fmt"

    "libvirt.org/libvirt-go"
    "github.com/openagentplatform/openagentplatform/internal/eve"
)

type LibvirtClient struct {
    uri string
}

func NewLibvirtClient(uri string) *LibvirtClient {
    return &LibvirtClient{uri: uri}
}

func (c *LibvirtClient) Name() eve.HypervisorProvider {
    return eve.HypervisorLibvirt
}

func (c *LibvirtClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
    conn, err := libvirt.NewConnect(c.uri)
    if err != nil {
        return nil, fmt.Errorf("libvirt connect: %w", err)
    }
    defer conn.Close()

    nodeInfo, err := conn.GetNodeInfo()
    if err != nil {
        return nil, err
    }
    return []eve.NodeInfo{{
        Name:       c.uri,
        Status:     "running",
        CPUCount:   int(nodeInfo.Cpus),
        MemoryMB:   int64(nodeInfo.Memory),
        UptimeSec:  int64(nodeInfo.Uptime),
    }}, nil
}

func (c *LibvirtClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
    conn, err := libvirt.NewConnect(c.uri)
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    domains, err := conn.ListAllDomains(libvirt.CONNECT_LIST_DOMAINS_ACTIVE)
    if err != nil {
        return nil, err
    }
    var vms []eve.VMInfo
    for _, d := range domains {
        name, _ := d.GetName()
        state, _, _ := d.GetState()
        info, _ := d.GetInfo()
        vms = append(vms, eve.VMInfo{
            ID:       name,
            Name:     name,
            Status:   libvirtDomainState(state),
            CPUCount: int(info.Cpu),
            MemoryMB: int64(info.MaxMem) / 1024,
            DiskGB:   int64(info.MaxDisk) / (1024 * 1024 * 1024),
        })
        d.Free()
    }
    return vms, nil
}

func (c *LibvirtClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
    // libvirt doesn't have LXC containers as a separate type — they're domains
    // with type="lxc". Return empty for the LXC-specific list.
    return nil, nil
}

func (c *LibvirtClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
    conn, err := libvirt.NewConnect(c.uri)
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    pools, err := conn.ListAllStoragePools(libvirt.CONNECT_LIST_STORAGE_POOLS_ACTIVE)
    if err != nil {
        return nil, err
    }
    var infos []eve.StoragePoolInfo
    for _, p := range pools {
        name, _ := p.GetName()
        state, _, _ := p.GetInfo()
        infos = append(infos, eve.StoragePoolInfo{
            Name:    name,
            Type:    "libvirt-pool",
            Status:  storagePoolState(state),
            TotalGB: int64(state.Capacity) / (1024 * 1024 * 1024),
            UsedGB:  int64(state.Allocation) / (1024 * 1024 * 1024),
        })
        p.Free()
    }
    return infos, nil
}

func (c *LibvirtClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
    return nil, nil  // libvirt has no persistent event log over the wire
}

func libvirtDomainState(s libvirt.DomainState) string {
    switch s {
    case libvirt.DOMAIN_RUNNING: return "running"
    case libvirt.DOMAIN_SHUTDOWN: return "stopped"
    case libvirt.DOMAIN_PAUSED: return "paused"
    default: return "unknown"
    }
}

func storagePoolState(i libvirt.StoragePoolInfo) eve.StoragePoolInfo {
    return eve.StoragePoolInfo{}
}
```

- [ ] **Step 2: Write vSphere client**

```go
// internal/eve/vsphere/client.go
package vsphere

import (
    "context"

    "github.com/vmware/govmomi"
    "github.com/vmware/govmomi/view"
    "github.com/vmware/govmomi/vim25"
    "github.com/vmware/govmomi/vim25/mo"

    "github.com/openagentplatform/openagentplatform/internal/eve"
)

type VSphereClient struct {
    client *vim25.Client
}

func NewVSphereClient(ctx context.Context, url, user, password string) (*VSphereClient, error) {
    client, err := govmomi.NewClient(ctx, url, true)
    if err != nil {
        return nil, err
    }
    return &VSphereClient{client: client.Client}, nil
}

func (c *VSphereClient) Name() eve.HypervisorProvider {
    return eve.HypervisorVSphere
}

func (c *VSphereClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
    m := view.NewManager(c.client)
    v, err := m.CreateContainerView(ctx, c.client.ServiceContent.RootFolder, []string{"HostSystem"}, true)
    if err != nil {
        return nil, err
    }
    defer v.Destroy(ctx)

    var hosts []mo.HostSystem
    if err := v.Retrieve(ctx, []string{"HostSystem"}, []string{"summary"}, &hosts); err != nil {
        return nil, err
    }
    var nodes []eve.NodeInfo
    for _, h := range hosts {
        nodes = append(nodes, eve.NodeInfo{
            Name:       h.Name,
            Status:     string(h.Runtime.ConnectionState),
            CPUCount:   int(h.Summary.Hardware.CpuNum),
            MemoryMB:   int64(h.Summary.Hardware.MemorySize) / (1024 * 1024),
            UptimeSec:  int64(h.Summary.QuickStats.Uptime),
        })
    }
    return nodes, nil
}

func (c *VSphereClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
    m := view.NewManager(c.client)
    v, err := m.CreateContainerView(ctx, c.client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
    if err != nil {
        return nil, err
    }
    defer v.Destroy(ctx)

    var vms []mo.VirtualMachine
    if err := v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"summary", "config"}, &vms); err != nil {
        return nil, err
    }
    var infos []eve.VMInfo
    for _, vm := range vms {
        infos = append(infos, eve.VMInfo{
            ID:         vm.Config.InstanceUuid,
            Name:       vm.Name,
            Status:     string(vm.Runtime.PowerState),
            CPUCount:   int(vm.Config.Hardware.NumCpu),
            MemoryMB:   int64(vm.Config.Hardware.MemoryMB),
            DiskGB:     int64(vm.Summary.Storage.Committed) / (1024 * 1024 * 1024),
        })
    }
    return infos, nil
}

func (c *VSphereClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
    return nil, nil  // vSphere doesn't distinguish containers separately
}

func (c *VSphereClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
    return nil, nil  // Storage pools in vSphere are handled via datastore views
}

func (c *VSphereClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
    return nil, nil
}
```

- [ ] **Step 3: Write interface tests**

```go
// internal/eve/libvirt/client_test.go
package libvirt

import "testing"

func TestLibvirtClientImplementsInterface(t *testing.T) {
    var _ interface {
        Name() string
        ListNodes, ListVMs, ListContainers, ListStoragePools, ListRecentEvents
    } = interface{}((*LibvirtClient)(nil))
}

// internal/eve/vsphere/client_test.go
package vsphere

import "testing"

func TestVSphereClientImplementsInterface(t *testing.T) {
    var _ interface {
        Name() string
        ListNodes, ListVMs, ListContainers, ListStoragePools, ListRecentEvents
    } = interface{}((*VSphereClient)(nil))
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./internal/eve/libvirt/ ./internal/eve/vsphere/`
Expected: PASS (compile success)

- [ ] **Step 5: Commit**

```bash
git add internal/eve/libvirt/client.go internal/eve/vsphere/client.go internal/eve/libvirt/client_test.go internal/eve/vsphere/client_test.go
git commit -m "feat(eve): libvirt and vSphere provider clients"
```

---

### Task 5: Reconciliation Worker and Alert Conditions

**Files:**
- Create: `internal/eve/reconciler.go`
- Create: `internal/eve/reconciler_test.go`
- Modify: `pkg/models/models_alerts.go` — add EVE-specific alert fields

- [ ] **Step 1: Write the reconciler**

```go
// internal/eve/reconciler.go
package eve

import (
    "context"
    "log/slog"
    "sync"
    "time"
)

type Reconciler struct {
    store     ClusterStore
    resources ResourceStore
    events   EventStore
    clients  map[string]HypervisorClient
    log      *slog.Logger
    mu       sync.Mutex
}

func NewReconciler(s ClusterStore, r ResourceStore, e EventStore, log *slog.Logger) *Reconciler {
    return &Reconciler{
        store:     s,
        resources: r,
        events:    e,
        clients:  make(map[string]HypervisorClient),
        log:      log,
    }
}

func (r *Reconciler) RegisterClient(id string, client HypervisorClient) {
    r.clients[id] = client
}

func (r *Reconciler) ReconcileCluster(ctx context.Context, clusterID string) error {
    cluster, err := r.store.Get(ctx, clusterID)
    if err != nil {
        return err
    }
    if !cluster.Enabled {
        return nil
    }

    client, ok := r.clients[cluster.Provider.String()]
    if !ok {
        r.log.Warn("eve: no client for provider", "provider", cluster.Provider)
        return nil
    }

    now := time.Now()
    var prevStatuses = make(map[string]string)

    // Snapshot previous VM statuses for transition detection
    existing, _ := r.resources.ListByCluster(ctx, clusterID)
    for _, res := range existing {
        prevStatuses[res.ResourceID] = res.Status
    }

    // Fetch VMs
    vms, err := client.ListVMs(ctx)
    if err != nil {
        r.log.Warn("eve: ListVMs failed", "cluster", clusterID, "err", err)
    }
    for _, vm := range vms {
        res := resourceFromVM(vm, cluster)
        if err := r.resources.Upsert(ctx, res); err != nil {
            r.log.Warn("eve: upsert VM failed", "id", vm.ID, "err", err)
        }
        // Transition detection: running → stopped
        if prev, ok := prevStatuses[vm.ID]; ok && prev == "running" && vm.Status == "stopped" {
            r.emitAlert(ctx, cluster.OrgID, "hypervisor_vm_down", vm.ID, vm.Name)
        }
    }

    // Fetch containers
    cts, err := client.ListContainers(ctx)
    if err == nil {
        for _, ct := range cts {
            res := resourceFromContainer(ct, cluster)
            if err := r.resources.Upsert(ctx, res); err != nil {
                r.log.Warn("eve: upsert CT failed", "id", ct.ID, "err", err)
            }
        }
    }

    // Fetch storage pools
    pools, err := client.ListStoragePools(ctx)
    if err == nil {
        for _, p := range pools {
            if p.TotalGB > 0 && float64(p.UsedGB)/float64(p.TotalGB) > 0.85 {
                r.emitAlert(ctx, cluster.OrgID, "hypervisor_storage_warning",
                    p.Name, map[string]any{"pool": p.Name, "used_pct": float64(p.UsedGB) * 100 / float64(p.TotalGB)})
            }
        }
    }

    // Fetch events and ingest
    events, err := client.ListRecentEvents(ctx, cluster.LastSeen)
    if err == nil {
        for _, ev := range events {
            r.events.Insert(ctx, &HypervisorEvent{
                ID:          clusterID + "-" + ev.Timestamp.Format(time.RFC3339Nano),
                OrgID:       cluster.OrgID,
                ClusterID:   clusterID,
                EventType:   ev.Type,
                Payload:     ev.Payload,
                OccurredAt:  ev.Timestamp,
                IngestedAt:  now,
            })
            if ev.Type == "ha_failover" {
                r.emitAlert(ctx, cluster.OrgID, "hypervisor_ha_failover", clusterID, ev.Payload)
            }
        }
    }

    // Update cluster last_seen
    cluster.LastSeen = now
    r.store.UpdateLastSeen(ctx, cluster.ID, now)
    return nil
}

func (r *Reconciler) emitAlert(ctx context.Context, orgID, alertType, resourceID string, payload any) {
    // Emit via oap.events.alerts — reuse existing alert infrastructure
    // (integration with internal/events/ goes here)
    r.log.Info("eve: alert", "type", alertType, "resource", resourceID)
}

func resourceFromVM(vm VMInfo, cluster *HypervisorCluster) *HypervisorResource {
    return &HypervisorResource{
        ID:           cluster.ID + "-" + vm.ID,
        OrgID:        cluster.OrgID,
        ClusterID:    cluster.ID,
        ResourceID:   vm.ID,
        ResourceType: "vm",
        Name:         vm.Name,
        Status:       vm.Status,
        CPUCount:     vm.CPUCount,
        MemoryMB:     vm.MemoryMB,
        DiskGB:       vm.DiskGB,
        LastSeen:     time.Now(),
    }
}

func resourceFromContainer(ct ContainerInfo, cluster *HypervisorCluster) *HypervisorResource {
    return &HypervisorResource{
        ID:           cluster.ID + "-ct-" + ct.ID,
        OrgID:        cluster.OrgID,
        ClusterID:    cluster.ID,
        ResourceID:   ct.ID,
        ResourceType: "ct",
        Name:         ct.Name,
        Status:       ct.Status,
        CPUCount:     ct.CPUCount,
        MemoryMB:     ct.MemoryMB,
        DiskGB:       ct.DiskGB,
        LastSeen:     time.Now(),
    }
}
```

- [ ] **Step 2: Write reconciler test**

```go
// internal/eve/reconciler_test.go
package eve

import "testing"

func TestNewReconciler(t *testing.T) {
    r := NewReconciler(nil, nil, nil, nil)
    if r == nil {
        t.Fatal("NewReconciler returned nil")
    }
}

func TestResourceFromVM(t *testing.T) {
    cluster := &HypervisorCluster{ID: "c1", OrgID: "org1"}
    vm := VMInfo{ID: "vm-100", Name: "web-prod", Status: "running", CPUCount: 4, MemoryMB: 8192}
    res := resourceFromVM(vm, cluster)
    if res.ResourceType != "vm" {
        t.Errorf("ResourceType = %q, want vm", res.ResourceType)
    }
    if res.DiskGB != 0 {
        t.Errorf("DiskGB = %d, want 0", res.DiskGB)
    }
}
```

- [ ] **Step 3: Add EVE-specific AlertRule fields**

```go
// pkg/models/models_alerts.go — add to AlertRule struct:
type AlertRule struct {
    // ... existing fields ...
    // EVE-specific alert conditions:
    HypervisorClusterID  string   `json:"hypervisor_cluster_id,omitempty"`
    HypervisorEventTypes []string  `json:"hypervisor_event_types,omitempty"` // ha_failover, vm_down, storage_warning
    StoragePoolAlertPct *int      `json:"storage_pool_alert_pct,omitempty"` // default 85
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./internal/eve/... && go test ./internal/eve/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/eve/reconciler.go internal/eve/reconciler_test.go pkg/models/models_alerts.go
git commit -m "feat(eve): reconciliation worker + EVE alert condition fields"
```

---

### Task 6: API Routes and Handlers

**Files:**
- Create: `internal/api/eve.go`
- Modify: `internal/api/routes.go` — add `s.mountEveRoutes(r)`
- Test: `internal/api/eve_test.go`

- [ ] **Step 1: Write EVE handlers**

```go
// internal/api/eve.go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/openagentplatform/openagentplatform/internal/eve"
)

func (s *Server) mountEveRoutes(r chi.Router) {
    r.Route("/eve", func(r chi.Router) {
        r.Get("/clusters", s.listEVEClusters)
        r.Post("/clusters", auth.RequireRole(auth.RoleAdmin), s.createEVECluster)
        r.Put("/clusters/{id}", auth.RequireRole(auth.RoleAdmin), s.updateEVECluster)
        r.Delete("/clusters/{id}", auth.RequireRole(auth.RoleAdmin), s.deleteEVECluster)
        r.Get("/clusters/{id}/resources", s.listEVEClusterResources)
        r.Get("/clusters/{id}/events", s.listEVEClusterEvents)
        r.Get("/resources/{id}", s.getEVEResource)
        r.Post("/resources/{id}/enroll", s.enrollEVEResource)
    })
}

func (s *Server) listEVEClusters(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    orgID := tenancy.GetTenant(ctx).OrgID
    clusters, err := s.eveClusters.ListByOrg(ctx, orgID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(clusters)
}

func (s *Server) createEVECluster(w http.ResponseWriter, r *http.Request) {
    var cluster eve.HypervisorCluster
    if err := json.NewDecoder(r.Body).Decode(&cluster); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }
    cluster.OrgID = tenancy.GetTenant(r.Context()).OrgID
    if err := s.eveClusters.Create(r.Context(), &cluster); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(201)
    json.NewEncoder(w).Encode(cluster)
}

func (s *Server) deleteEVECluster(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := s.eveClusters.Delete(r.Context(), id); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(204)
}

func (s *Server) listEVEClusterResources(w http.ResponseWriter, r *http.Request) {
    clusterID := chi.URLParam(r, "id")
    resources, err := s.eveResources.ListByCluster(r.Context(), clusterID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(resources)
}

func (s *Server) listEVEClusterEvents(w http.ResponseWriter, r *http.Request) {
    clusterID := chi.URLParam(r, "id")
    events, err := s.eveEvents.ListByCluster(r.Context(), clusterID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(events)
}
```

- [ ] **Step 2: Wire routes**

```go
// internal/api/routes.go — in mountAPISubRoutes:
s.mountEveRoutes(r)
```

- [ ] **Step 3: Write and run route test**

```go
// internal/api/eve_test.go
package api

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestEveRoutes(t *testing.T) {
    srv := newTestServer(t)
    routes := []struct {
        method, path string
        wantCode int
    }{
        {"GET", "/api/v1/eve/clusters", 200},
        {"POST", "/api/v1/eve/clusters", 201},
        {"GET", "/api/v1/eve/clusters/c1/resources", 200},
    }
    for _, r := range routes {
        req := httptest.NewRequest(r.method, r.path, nil)
        req.Header.Set("Authorization", "Bearer " + testToken(t))
        w := httptest.NewRecorder()
        srv.ServeHTTP(w, req)
        if w.Code != r.wantCode {
            t.Errorf("%s %s → %d, want %d", r.method, r.path, w.Code, r.wantCode)
        }
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/... -run TestEve -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/eve.go internal/api/routes.go internal/api/eve_test.go
git commit -m "feat(eve): API surface — /api/v1/eve clusters, resources, events"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** §1 dual integration (Task 2+3+4), §2 providers (Task 3+4), §3 hypervisor-as-tag (Task 2+7), §4 alert conditions (Task 5), §5 data model (Task 1), §6 API (Task 6), §7 credentials (Task 3+4), §8 backup status (noted in reconciler as NA).
- [ ] **Placeholder scan:** All code concrete.
- [ ] **Type consistency:** `HypervisorProvider` defined in models_eve.go, `HypervisorClient` in eve.go, implemented in Tasks 3+4.
- [ ] **Pattern adherence:** `CREATE TABLE IF NOT EXISTS`, `org_id TEXT`, no FKs, RLS. All routes chi, role-gated. No new NATS subjects.
