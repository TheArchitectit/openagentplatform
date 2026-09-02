# Cloud Control

> **Phase:** P3 — RMM parity gap
> **Status:** DRAFT
> **Source:** `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.4 (G-CLD-001..006)
> **App Path:** `internal/cloud/`, `internal/cloud/aws/`, `internal/cloud/azure/`, `internal/cloud/gcp/`, `internal/reconciliation/`, `pkg/models/models_cloud.go`, `internal/db/migrations/`
> **Depends on:** openspec/specs/rmm-core/spec.md §14

---

## Description

An MSP managing dozens of customer environments needs a unified view of cloud resources across AWS, Azure, and GCP — inventory, cost telemetry, and tag drift — without stitching together three vendor consoles. Cloud Control adds cloud-provider integrations to OAP: centralized discovery and normalization, per-org credential management, virtual agent enrollment for unmanaged resources, and drift alerting.

This spec does **not** invent mechanisms. Each requirement is anchored to an existing pattern in the codebase.

---

## User Story

**As** an MSP technician,
**I want** to see all cloud compute, storage, and network resources across my customers' AWS/Azure/GCP environments in one view, get alerted when a resource's tags drift from policy, and have unmanaged cloud instances auto-enrolled so nothing falls through the gaps,
**so that** I can manage hybrid cloud infrastructure at parity with on-prem agents, without context-switching between cloud consoles or losing visibility into untagged resources.

---

## Requirements

### 1. Integration Model

**Credential ownership** is hybrid: MSP-level credentials live in server config for cross-account discovery (e.g., AWS Org master role, Azure Lighthouse, GCP organization); per-org credentials are stored via `SecretBackend` for tenant-specific accounts. Both are used by the same reconciliation engine.

1.1. Centralized discovery: a configurable set of MSP-level cloud accounts (AWS Org, Azure Lighthouse, GCP Org) is stored in `internal/config/cloud.go`. Credentials for these are env vars or `SecretBackend` refs. The server polls these for resource discovery across all customer environments.

1.2. Per-org credentials: each org can register additional cloud accounts it owns (e.g., a customer's AWS sub-account, Azure subscription). Stored via `SecretBackend` at `external/cloud/{provider}/{account_id}`. The reconciliation engine queries both the centralized and per-org credential sets.

1.3. NATS taxonomy: no new `oap.*` subjects are required for cloud discovery. Cloud events (resource found, drift detected, enrollment changed) are emitted as alert events via the existing `oap.events.alerts` subject, using a new `cloud_drift` alert condition type.

### 2. Resource Inventory — Compute + Storage + Network

Cloud resources are normalized into a unified `cloud_resources` table, keyed by `(org_id, provider, account_id, resource_id)`.

2.1. **Compute** — tracked resources: EC2 instances, Auto Scaling Groups, RDS/Aurora DB instances, Lambda functions, Azure VMs, Azure Virtual Machine Scale Sets, Azure App Service Plans, GCP Compute Engine instances, GCP Cloud Functions.

2.2. **Storage** — tracked: S3 buckets, Azure Blob Storage containers, Azure Files shares, GCP Cloud Storage buckets.

2.3. **Network** — tracked: VPCs, subnets, route tables, internet gateways, NAT gateways, security groups, NACLs, VPN gateways, Direct Connect connections, Azure Virtual Networks, Azure Subnets, Azure Network Security Groups, Azure Load Balancers, GCP VPCs, GCP Subnetworks, GCP Firewall Rules, GCP Routers, GCP Cloud NAT.

2.4. Each resource record includes: `resource_id` (cloud-native ID), `resource_type` (e.g. `ec2_instance`, `azure_vm`, `gcp_vpc`), `account_id`, `region`, `tags` (JSONB, normalized to lowercase tag keys), `status`, `created_at`, `last_seen`. Resources that disappear from the cloud API are marked `archived` (not deleted) with `archived_at`.

2.5. **OUT of scope**: EKS/GKE/AKS node inventory (separate from compute instances); Lambda layers; Secrets Manager / Parameter Store; Azure Resource Manager nested resources; GCP Cloud Router BGP peers.

### 3. Auto-Enrollment (Cloud-First)

Resources discovered in cloud that have no matching OAP agent are enrolled as **virtual agents**.

3.1. A virtual agent is a row in `agents` with `platform = "cloud/{provider}"` (e.g., `cloud/aws`). It has an `agent_id` derived from the cloud resource ID and a synthetic hostname. It is not a real daemon process — it has no heartbeat TTL.

3.2. Enrollment is opt-in per org: the org has an `auto_enroll_cloud_resources` boolean (default false). When true, the discovery job creates virtual agents automatically. When false, new resources appear in a "Pending Enrollment" queue.

3.3. The MSP technician can promote a virtual agent to a real agent (install OAP agent on the cloud resource, update the record), ignore it (archive it), or keep it virtual. Virtual agents can receive check assignments — useful for cloud-native checks that don't require an agent binary.

3.4. **OUT of scope**: automatic OAP agent installation on cloud instances (that is a deployment problem, not an RMM problem). Pushing agent installers to cloud instances is a future capability.

### 4. Tag Drift Detection

4.1. A `CloudPolicy` entity defines expected tags per cloud account. Fields: `id, org_id, provider, account_id, required_tags JSONB, tag_rules JSONB (e.g., `{"env": ["prod","staging"]}`)`, `enabled, created_at, updated_at`.

4.2. The reconciliation worker runs on the scheduler (30s tick, per-org). It compares cloud resource tags against the applicable `CloudPolicy`. Non-compliant resources emit a `cloud_drift` alert via `oap.events.alerts`.

4.3. Drift types: `missing_required_tag`, `invalid_tag_value`, `unmanaged_resource` (virtual agent enrolled with no policy applied). The alert payload includes the resource ID, drift type, and expected vs. actual tags.

4.4. **OUT of scope**: automated remediation (changing tags). Alert only.

### 5. Cost Telemetry

5.1. A `CostSnapshot` table stores monthly cost per cloud account: `id, org_id, provider, account_id, billing_period, total_cost_usd, service_costs JSONB, created_at`. Snapshots are populated by the cloud poller fetching the cost/usage API.

5.2. Cost anomalies are a `cost_anomaly` alert condition: fires when a service's cost exceeds the rolling 90-day average by more than 2x. Threshold is configurable per account.

5.3. **OUT of scope**: cross-account cost allocation reports (requires a separate reporting module). Budget alerts (nice-to-have, not in this spec).

### 6. Data Model

New tables (additive, canonical migrations):

```
cloud_accounts     — org_id, provider, account_id, display_name, is_msp_hub, enabled, last_seen
cloud_resources   — org_id, provider, account_id, resource_id, resource_type, region, tags JSONB,
                     status, archived_at, created_at, updated_at
cloud_policies    — org_id, provider, account_id, required_tags JSONB, tag_rules JSONB, enabled
cost_snapshots    — org_id, provider, account_id, billing_period, total_cost_usd, service_costs JSONB
```

No existing table is altered. All new tables use the standard conventions: `CREATE TABLE IF NOT EXISTS`, `org_id TEXT NOT NULL DEFAULT ''`, no FKs, RLS enabled.

### 7. API Surface

New route group `/api/v1/cloud`:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/accounts` | Admin/Technician | List registered cloud accounts |
| POST | `/accounts` | Admin | Register a cloud account |
| DELETE | `/accounts/{id}` | Admin | Remove a cloud account |
| GET | `/resources` | Admin/Technician | List cloud resources (filterable) |
| GET | `/resources/{id}` | Admin/Technician | Resource detail |
| POST | `/resources/{id}/enroll` | Admin/Technician | Promote virtual → real agent |
| POST | `/resources/{id}/ignore` | Admin/Technician | Archive virtual agent |
| GET | `/policies` | Admin/Technician | List tag policies |
| POST | `/policies` | Admin | Create tag policy |
| PUT | `/policies/{id}` | Admin | Update tag policy |
| DELETE | `/policies/{id}` | Admin | Delete tag policy |
| GET | `/costs` | Admin/Technician | Cost snapshots per account |
| GET | `/drift` | Admin/Technician | Current drift summary |

### 8. Credential Storage

Cloud account credentials use the `SecretBackend` at paths:
- MSP hub accounts: `ref:oap://secret/cloud/{provider}/hub`
- Per-org accounts: `ref:oap://secret/cloud/{provider}/{account_id}`

For AWS: access key + secret, or `ref:oap://external/aws/{account_id}` for cross-account IAM role assumption via STS. For Azure: client ID + secret + tenant ID. For GCP: service account JSON key.

Each cloud provider package (`internal/cloud/aws/`, `internal/cloud/azure/`, `internal/cloud/gcp/`) implements a `ProviderClient` interface:
```go
type ProviderClient interface {
    ListResources(ctx context.Context, accountID string) ([]CloudResource, error)
    ListAccounts(ctx context.Context) ([]CloudAccount, error)
    GetCost(ctx context.Context, accountID, period string) (CostSnapshot, error)
    Name() string
}
```

A `CircuitBreaker` is instantiated per cloud account (not per provider) to isolate failures.

---

## Cross-References

- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.4
- `internal/events/subjects.go` — NATS taxonomy
- `internal/secrets/backend.go` — `SecretBackend` interface
- `internal/resilience/breaker.go` — `CircuitBreaker` pattern
- `internal/scheduled/scheduler.go` — periodic job pattern
- `openspec/specs/rmm-core/spec.md` §14
