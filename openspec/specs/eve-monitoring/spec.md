# EVE / Hypervisor Monitoring

> **Phase:** P3 — RMM parity gap
> **Status:** DRAFT
> **Source:** `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.6 (G-EVE-001..005)
> **App Path:** `internal/eve/`, `internal/eve/proxmox/`, `internal/eve/libvirt/`, `internal/eve/vsphere/`, `pkg/agent/checkers/hypervisor_*.go`, `pkg/models/models_eve.go`
> **Depends on:** openspec/specs/rmm-core/spec.md §14

---

## Description

MSPs running customer infrastructure on Proxmox VE, KVM/libvirt, or ESXi/vSphere need OAP to monitor the hypervisor host, its child VMs and containers, and cluster-wide state (HA, storage pools, migration events). EVE Monitoring adds a dual integration: an OAP agent on each hypervisor host (for per-host metrics and local inventory), plus a server-side connector to the hypervisor cluster API (for cluster-wide state).

This spec does **not** invent mechanisms. Each requirement is anchored to an existing pattern.

---

## User Story

**As** an MSP technician,
**I want** to see hypervisor host health, VM/CT inventory, and cluster state for each customer's Proxmox cluster or vCenter instance, get alerted when a VM goes down or HA fails over, and apply the same OAP alerting and patching workflow to hypervisor hosts as I do to regular servers,
**so that** I can manage virtualized infrastructure at parity with bare-metal agents, without logging into each hypervisor console.

---

## Requirements

### 1. Integration Model — Host Agent + Server Connector

Two complementary integration surfaces, both required:

1.1. **Host agent**: install the OAP agent on the hypervisor host (Proxmox node, libvirt host, ESXi host). The existing agent framework reports host metrics (CPU, memory, disk, load) via the standard `oap.agents.*.heartbeat` and `oap.agents.*.results` subjects. No new NATS subjects are required for host-level data.

1.2. **Server-side connector**: the OAP server connects to the cluster API (Proxmox VE API, libvirt URI, vCenter) using credentials stored via `SecretBackend`. Polls cluster-wide state: nodes, VMs, containers, storage pools, HA status, recent events.

1.3. Both surfaces feed into a unified `hypervisor_resources` table. The host agent provides the per-host row; the cluster connector provides the child VM/CT rows. The host agent row is keyed by `agent_id` (existing); the cluster rows are keyed by `(cluster_id, resource_id)`.

### 2. Supported Hypervisors

2.1. **Proxmox VE** — primary target. Server connector: Proxmox API at `https://{host}:8006/api2/json` with token-based auth. Discovers nodes, VMs (QEMU), containers (LXC), storage pools (LVM, ZFS, NFS, Ceph), cluster HA status, recent migration events.

2.2. **libvirt** — primary target. Server connector: libvirt URI (`qemu:///system` or remote `qemu+ssh://`). Discovers domains (VMs), storage pools, networks, node info. Works against any libvirt backend: KVM, QEMU, Xen, LXC.

2.3. **ESXi/vSphere** — secondary target. Server connector: vCenter REST API (or `govmomi` Go SDK for richer queries). Discovers ESXi hosts, VMs, datastores, resource pools, vMotion events, HA status.

2.4. **OUT of scope**: Hyper-V (no current MSP demand), XCP-ng, Nutanix, oVirt. Can be added by implementing the `HypervisorClient` interface.

### 3. Agent as Hypervisor — First-Class Subtype

3.1. The `Agent` model has no `type`/`subtype` field; classification is via `Tags`. EVE uses a canonical tag prefix: `eve:hypervisor:<provider>` (e.g., `eve:hypervisor:proxmox`, `eve:hypervisor:libvirt`, `eve:hypervisor:vsphere`). A host agent with one of these tags is recognized as a hypervisor host.

3.2. A new `hypervisor_clusters` table stores cluster metadata: `id, org_id, provider, name, endpoint, credential_ref, tags JSONB, enabled, last_seen`. The `endpoint` is a URL or URI the server connector queries. The `credential_ref` is a `SecretBackend` URI.

3.3. When a host agent reports itself as a hypervisor (via tag), the server creates or finds a `hypervisor_clusters` record for that host's cluster. The host's `agent_id` becomes the `primary_agent_id` on the cluster row.

3.4. The cluster's child VMs/containers appear as **virtual agents** in the `agents` table, similar to cloud auto-enrollment: `platform = "eve/{provider}"`, `agent_id` derived from the VM/CT ID, no heartbeat TTL.

### 4. Alert Conditions

New alert condition types in `pkg/models/models_alerts.go`:

4.1. `hypervisor_host_offline` — fires when a hypervisor host's last_seen exceeds the standard 120s threshold. Reuses the existing offline detection (no new liveness logic).

4.2. `hypervisor_vm_down` — fires when a VM/CT that was running in the last poll is now reported as stopped. The cluster connector detects this on each poll; the alert is emitted on transition only (state machine, not re-emitted every poll).

4.3. `hypervisor_ha_failover` — fires when a HA failover event appears in the cluster event log.

4.4. `hypervisor_storage_warning` — fires when a storage pool exceeds 85% utilization (threshold configurable).

4.5. All four conditions emit via the existing `oap.events.alerts` subject. No new subjects.

### 5. Data Model

New tables:

```
hypervisor_clusters     — org_id, provider, name, endpoint, credential_ref, primary_agent_id,
                          tags JSONB, enabled, last_seen
hypervisor_resources   — org_id, cluster_id, resource_id, resource_type (vm/ct/node/storage/network),
                          name, status, parent_resource_id, cpu_count, memory_mb, disk_gb,
                          last_seen, archived_at
hypervisor_events      — org_id, cluster_id, event_type, payload JSONB, occurred_at, ingested_at
```

All use the standard conventions. RLS enabled. No FKs.

### 6. API Surface

New route group `/api/v1/eve`:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/clusters` | Admin/Technician | List hypervisor clusters |
| POST | `/clusters` | Admin | Register a cluster |
| PUT | `/clusters/{id}` | Admin | Update cluster |
| DELETE | `/clusters/{id}` | Admin | Remove cluster |
| GET | `/clusters/{id}/resources` | Admin/Technician | List VMs/containers/nodes |
| GET | `/clusters/{id}/events` | Admin/Technician | Recent cluster events |
| GET | `/resources/{id}` | Admin/Technician | Resource detail |
| POST | `/resources/{id}/enroll` | Admin/Technician | Promote virtual → real agent |

### 7. Credentials

7.1. Proxmox: `ref:oap://secret/eve/proxmox/{cluster_id}` containing `token_id` + `token_secret`. Token requires `PVEAdmin` or a custom role with `VM.Audit`, `VM.PowerMgmt`, `Datastore.Audit`, `Sys.Audit`.

7.2. libvirt: `ref:oap://secret/eve/libvirt/{cluster_id}` containing a `qemu+ssh://user@host/system` URI with an embedded SSH key path. The server opens a libvirt connection on the local network (no public exposure).

7.3. vSphere: `ref:oap://secret/eve/vsphere/{cluster_id}` containing vCenter URL + username + password. vCenter's REST API is queried over HTTPS.

7.4. A `CircuitBreaker` per cluster isolates failures — one bad cluster does not block the others.

### 8. Backup Status (Nice-to-Have)

8.1. A `hypervisor_backup_status` table stores the latest known state of backup jobs discovered via the cluster API: `org_id, cluster_id, resource_id, job_id, status, last_run_at, duration_seconds, size_bytes`.

8.2. Proxmox supports this via `pvesr` and PBS integration. vSphere via VADP. libvirt has no standard backup — left as `unknown` for libvirt resources.

8.3. A `hypervisor_backup_failed` alert condition fires when a backup job's status transitions to `error` or when a scheduled job has not run in > 25 hours.

8.4. **OUT of scope**: triggering backups, restoring from backups, configuring backup jobs. Discovery and alerting only.

---

## Cross-References

- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.6
- `internal/events/subjects.go` — NATS taxonomy
- `pkg/models/models.go` — Agent struct (no type field; use `Tags`)
- `internal/scheduled/scheduler.go` — periodic polling pattern
- `openspec/specs/rmm-core/spec.md` §14
