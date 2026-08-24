# Managed Backup

> **Phase:** 2 (Managed Services) — Proposed
> **STATUS: DRAFT** — greenfield; no implementation exists yet. This spec is the
> OAP-side integration contract and the canonical owner of the cross-service
> seam contracts (run states, event names, integrity states) that the separate
> Managed Backup product must import.
> **Source:** second-opinion spec review (2026-08-24); boundary invariants adapted
> from the review bundle's `proposed-additions/managed-backup/spec.md`
> **Implementation:** intended for a separate managed-backup repository and
> first-party `oap-backup-client`

---

## Description

Managed Backup is a native OAP managed-service experience with a hard
architectural boundary between the OAP control plane and a separately
deployable backup control/data plane. OAP owns operator experience, tenant/
device association, entitlement, audit, alerting, reporting, and HITL
orchestration. The backup product owns backup-client identity, policy
execution, recovery metadata, repository integrity, encryption, retention,
and restore execution.

The backup payload data plane — chunked, encrypted, deduplicated backup
objects flowing between `oap-backup-client` and an authorized repository —
never transits the OAP API, OAP PostgreSQL, the OAP WebSocket layer, or the
OAP NATS event bus. OAP receives only bounded metadata/events: client health,
run lifecycle, recovery-point summaries, and restore status.

`OAP Device`, `OAP Agent`, and `Backup Client` are distinct identities even
when correlated one-to-one. The backup service is authoritative for
recovery-point existence/integrity and restore execution; OAP may project
but never invent that state.

---

## User Story

**As** an MSP operator,
**I want** backup protection to appear as a native OAP capability while a
purpose-built client and backup service handle high-volume backup data,
**so that** I can prove protection, investigate failures, browse recovery
points, and perform controlled restores without coupling backup correctness
to the RMM control plane.

---

## Canonical Cross-Service Contracts

These contracts are normatively owned by this spec. Both OAP and the
standalone Managed Backup product MUST import them from here rather than
re-describing them in their own words. Breaking changes require a versioned
migration and updates to all dependent specifications.

### Backup Run State Machine

Nonterminal phases:

| State | Meaning |
|-------|---------|
| `QUEUED` | Run created, awaiting execution start |
| `STARTING` | Execution beginning, resources allocated |
| `SCANNING` | Discovering source file/object set |
| `SNAPSHOTTING` | Creating point-in-time snapshot of protected sources |
| `UPLOADING` | Transferring backup data to repository |
| `VERIFYING` | Verifying uploaded data integrity and finalizing catalog |

Terminal states:

| State | Meaning |
|-------|---------|
| `COMPLETED` | Run succeeded; a usable recovery point exists |
| `COMPLETED_WITH_WARNINGS` | Run succeeded with non-fatal source failures |
| `FAILED` | Run failed; no usable new recovery point |
| `CANCELLED` | Run cancelled by operator; no new recovery point |

"Finalization" is an operation performed before a successful terminal
transition (catalog commit + integrity check), not a separate state.
`FINALIZING` is not a state in contract version 1.

Only one terminal successful run points to exactly one finalized recovery
point for its protected source set. Partial upload is never represented as a
usable recovery point.

### Backup Event Type Catalog

The product MUST emit exactly these canonical event types. Each event carries
a stable event ID, schema version, occurred-at time, tenant ID, subject/
resource ID, producer identity, and causality/correlation metadata. Events
contain metadata only — never backup payloads, file listings, plaintext
secrets, decryption keys, or reusable repository credentials.

| Event type | When emitted |
|------------|--------------|
| `backup.client.registered` | A backup client completes enrollment |
| `backup.client.online` | Client heartbeat indicates active |
| `backup.client.offline` | Client missed heartbeat threshold |
| `backup.policy.applied` | Client acknowledged effective policy revision |
| `backup.policy.drifted` | Desired and acknowledged revisions diverge |
| `backup.run.started` | Run transitions to STARTING |
| `backup.run.progress` | Phase or byte-count progress update |
| `backup.run.completed` | Run reaches COMPLETED or COMPLETED_WITH_WARNINGS |
| `backup.run.failed` | Run reaches FAILED |
| `backup.recovery_point.created` | New recovery point finalized in catalog |
| `backup.recovery_point.verified` | Integrity verification passed |
| `backup.recovery_point.degraded` | Integrity verification found non-fatal issue |
| `backup.recovery_point.corrupt` | Integrity verification confirmed corruption |
| `backup.restore.requested` | Restore request submitted |
| `backup.restore.approved` | HITL approval granted for destructive restore |
| `backup.restore.started` | Restore execution began |
| `backup.restore.completed` | Restore finished successfully |
| `backup.restore.failed` | Restore failed |

Events that require per-resource ordering MUST carry a sequence/revision or
equivalent concurrency signal so consumers can reject stale projection
updates. At-least-once delivery is safe because events have stable IDs and
consumers MUST deduplicate.

### Recovery-Point Integrity State

| State | Meaning |
|-------|---------|
| `UNKNOWN` | Verification has not yet established integrity |
| `PENDING` | Verification scheduled or in progress |
| `VERIFIED` | Required verification completed successfully |
| `DEGRADED` | Non-fatal integrity deficiency; point still usable |
| `CORRUPT` | Required data cannot be proven usable |

A freshly created recovery point starts at `UNKNOWN`. It MUST NOT be
presented as healthy until verification moves it to `VERIFIED` or `DEGRADED`.
Transient infrastructure/key outages during verification produce `PENDING`
with retryable evidence, not `CORRUPT`.

### Restore State Machine

| State | Meaning |
|-------|---------|
| `REQUESTED` | Restore request submitted |
| `AWAITING_APPROVAL` | Destructive/in-place restore paused for HITL approval |
| `QUEUED` | Approved (or non-destructive), awaiting execution |
| `PREPARING` | Assembling restore manifest, resolving recovery point |
| `RESTORING` | Writing recovered data to destination |
| `VERIFYING` | Re-verifying restored data integrity |
| `COMPLETED` | Restore finished successfully |
| `FAILED` | Restore failed |
| `CANCELLED` | Restore cancelled |

Destructive or in-place restores MUST require HITL approval by default unless
an explicit tenant policy authorizes a safer class. Every restore MUST
re-authorize tenant, target, recovery point, destination, and overwrite
behavior. Client restore writes MUST prevent path traversal, unauthorized
destinations, and silent overwrite beyond approved policy.

---

## Requirements

### Requirement: Product boundary

Managed Backup MUST be independently deployable from OAP core services and
SHOULD live in a separate repository with its own versioning, CI/CD, releases,
and internal OpenSpec tree.

#### Scenario: Backup service unavailable

- **WHEN** the backup control plane is temporarily unreachable
- **THEN** unrelated OAP RMM functions continue to start and operate

#### Scenario: OAP UI unavailable

- **WHEN** a client is already performing an authorized backup
- **THEN** the in-progress data-plane operation is not corrupted merely because OAP presentation services are down

### Requirement: Systems of record

OAP MUST remain authoritative for OAP tenant/site/device/user/role/entitlement
identity. Managed Backup MUST remain authoritative for backup clients,
effective execution policy, runs, recovery points, repository manifests,
retention, integrity, and restore execution.

#### Scenario: Recovery point shown in OAP cache

- **WHEN** the backup service no longer considers that point valid
- **THEN** restore eligibility follows backup-service truth and OAP refreshes/removes the stale projection

### Requirement: Dedicated backup client

A first-party `oap-backup-client` MUST be a separate process/release artifact
from `oap-agent`, authenticate independently, initiate normal connections
outbound, and perform endpoint backup/restore data-plane work.

#### Scenario: RMM agent stops

- **WHEN** the backup client remains healthy
- **THEN** scheduled backup operation can continue independently

#### Scenario: Backup client stops

- **WHEN** the RMM agent remains healthy
- **THEN** OAP reports backup degradation without marking the endpoint itself offline

### Requirement: Enrollment and identity

Each backup client MUST receive a globally unique tenant-bound identity
through short-lived or single-use enrollment and MUST use rotatable
client-scoped runtime credentials.

#### Scenario: Enrollment token replayed

- **WHEN** a previously consumed/expired/revoked credential is presented
- **THEN** registration is rejected and no usable client identity is created

#### Scenario: Cross-tenant reassignment requested

- **WHEN** an ordinary update attempts to move a client to another tenant
- **THEN** the operation is rejected and requires explicit secure re-enrollment

### Requirement: Declarative policy

Backup policy MUST be declarative, revisioned, tenant-owned,
capability-validated, and include protected sources, schedule, retention,
network/resource controls, and backup options.

#### Scenario: Policy updated after completed run

- **WHEN** an operator changes retention/source settings
- **THEN** a new durable revision is created and the completed run still references the exact old revision

#### Scenario: Unsupported feature selected

- **WHEN** a Linux client lacks a requested snapshot capability
- **THEN** activation fails explicitly rather than silently ignoring the option

### Requirement: Policy assignment and drift

OAP MUST support device policy assignment and the backup service MUST resolve
one unambiguous effective policy per protected source set, with desired and
client-acknowledged revisions visible.

#### Scenario: Client has old revision

- **WHEN** desired policy changes while endpoint is offline
- **THEN** OAP reports policy drift until the client acknowledges the new effective revision

### Requirement: Scheduling and idempotency

Scheduled and on-demand backup initiation MUST use durable scheduling/
idempotency and define missed-window, overlap, reconnect, and service
concurrency behavior.

#### Scenario: On-demand request retried

- **WHEN** the caller repeats the same idempotency key
- **THEN** one logical run is created

#### Scenario: Endpoint sleeps through window

- **WHEN** the endpoint reconnects later
- **THEN** the documented catch-up/skip policy deterministically decides whether a run is due

### Requirement: Backup run state machine

Backup runs MUST use the canonical run-state enum defined above in §Backup
Run State Machine. Once a run reaches a terminal state, ordinary progress
updates MUST NOT move it back to a nonterminal state.

#### Scenario: Run completes successfully

- **WHEN** verification requirements pass and a usable recovery point exists
- **THEN** the run becomes `COMPLETED` and records the resulting recovery-point ID

#### Scenario: Fatal upload/verification failure

- **WHEN** a usable new recovery point cannot be established
- **THEN** the run becomes `FAILED` and does not imply a usable new recovery point

#### Scenario: Late progress arrives after cancellation

- **WHEN** a delayed progress event arrives after `CANCELLED`
- **THEN** the terminal cancelled state remains authoritative

### Requirement: Backup data plane

Bulk backup payloads MUST flow directly between `oap-backup-client` and an
authorized repository/storage gateway over encrypted transport and MUST bypass
OAP APIs, database, WebSocket, and NATS.

#### Scenario: Large backup uploads

- **WHEN** the client sends protected content
- **THEN** OAP receives only bounded metadata/events and never proxies object payload bytes

### Requirement: Chunking, compression, and deduplication

The backup implementation MAY use content-defined or fixed chunking,
compression, and deduplication. Cryptographic integrity MUST be verifiable.
Cross-tenant deduplication MUST NOT create confidentiality or authorization
side channels.

#### Scenario: Duplicate content exists in two tenants

- **WHEN** repository optimization evaluates chunks
- **THEN** one tenant cannot infer another tenant's content/existence through timing, identifiers, quota, restore, or API behavior

### Requirement: Encryption and keys

Backup data MUST be encrypted in transit and at rest. Payload encryption keys
MUST be separated from general OAP application credentials. Key rotation/
revocation/recovery responsibilities MUST be explicit.

#### Scenario: OAP database compromised

- **WHEN** an attacker obtains ordinary OAP relational data
- **THEN** that alone is insufficient to decrypt repository backup payloads

#### Scenario: Key rotated

- **WHEN** new encryption material becomes active
- **THEN** existing retained recovery points remain restorable according to documented key-version retention

### Requirement: Repository authorization

Repository access MUST use short-lived least-privilege authorization scoped to
tenant/client/run/object operation and MUST NOT grant a client broad listing/
deletion rights across repositories.

#### Scenario: Upload credential stolen

- **WHEN** the credential is replayed outside its object/run/time scope
- **THEN** repository authorization rejects the operation

### Requirement: Recovery points and integrity

A recovery point MUST be a first-class immutable tenant-bound catalog object
with source scope, creation time, policy/run linkage, retention/immutability
metadata, logical size, and an integrity-verification status using the
canonical `RECOVERY_POINT_INTEGRITY_STATE` enum defined above.

#### Scenario: Integrity check fails

- **WHEN** a retained point cannot be proven usable
- **THEN** the point is marked `DEGRADED` or `CORRUPT`, alerting is raised, and restore does not present it as healthy without an explicit degraded-recovery workflow

### Requirement: Retention and deletion

Retention expiration and deletion MUST be deterministic, idempotent,
auditable, and compatible with immutability/legal-hold constraints. Ordinary
policy changes MUST NOT retroactively bypass active immutability. Retention-
until, immutable-until, and legal-hold state MUST be exposed separately.

#### Scenario: Retention shortened

- **WHEN** older points are still under immutable-until date
- **THEN** they remain protected until immutability expires

#### Scenario: Retention expired but legal hold active

- **WHEN** the nominal retention date passes
- **THEN** the point remains protected and reports legal hold as the blocking reason

### Requirement: Restore state machine and safety

Restore requests MUST use the canonical restore-state enum defined above in
§Restore State Machine. Destructive or in-place restores MUST require HITL
approval by default unless an explicit tenant policy authorizes a safer class.

#### Scenario: AI proposes in-place restore

- **WHEN** automation determines a restore may remediate an incident
- **THEN** the request pauses for authorized human approval before destructive writes

#### Scenario: Alternate-location restore permitted

- **WHEN** policy classifies the action as non-destructive and autonomous
- **THEN** execution may proceed while remaining fully audited

### Requirement: Restore authorization and path safety

Every restore MUST re-authorize tenant, target, recovery point, destination,
and overwrite behavior. Client restore writes MUST prevent path traversal,
unauthorized destinations, and silent overwrite beyond approved policy.

#### Scenario: Manifest contains traversal path

- **WHEN** a restore entry would escape the approved destination
- **THEN** the client rejects that entry/run and records a security failure

### Requirement: Events and OAP projection

Managed Backup MUST publish the canonical event types defined above in
§Backup Event Type Catalog. Events MUST carry tenant and stable object/event
IDs and MUST NOT contain backup payloads or secrets. OAP MUST translate these
events into its internal event bus rather than requiring backup services to
know OAP subject internals.

#### Scenario: Run fails

- **WHEN** the backup service records terminal failure
- **THEN** OAP receives an idempotent failure event/projection sufficient for alerting and drill-down

#### Scenario: OAP changes internal NATS taxonomy

- **WHEN** the public backup event contract is unchanged
- **THEN** the backup service does not require a release solely for an OAP internal transport change

### Requirement: Entitlement and metering

OAP MAY gate Managed Backup by entitlement and meter protected endpoints/
stored/transferred/retained bytes, but licensing MUST NOT substitute for
tenant authorization or repository security.

#### Scenario: Entitlement expires

- **WHEN** a tenant becomes commercially ineligible
- **THEN** new protected operations follow commercial policy while existing retained data/restore/offboarding obligations remain explicitly governed

### Requirement: Audit and non-repudiation

Policy changes, assignment, manual backup/cancel, recovery-point deletion,
restore request/approval/execution, repository/key configuration, client
enrollment/revocation, and privileged data access MUST be auditable with
actor, tenant, target, outcome, and causality.

#### Scenario: Restore completed

- **WHEN** an operator later reviews the action
- **THEN** audit evidence identifies who requested/approved it, source recovery point, destination, times, outcome, and relevant policy

### Requirement: MVP boundary

Initial release MUST limit itself to explicitly supported file/folder
protection, Windows VSS-aware file backup, Linux file backup, S3-compatible
managed repository, incremental operation, encryption/compression, policy/
retention, recovery-point browsing, and file/folder restore. Deferred
workloads MUST NOT be marketed as implemented.

#### Scenario: User requests bare-metal restore

- **WHEN** no dedicated capability spec/implementation exists
- **THEN** the product identifies it as unsupported rather than offering a misleading partial flow

---

## Open Architecture Decisions

These items are genuinely undecided and MUST NOT be given fake certainty:

1. **Deployment model:** hosted-only vs self-hosted backup control plane;
   whether customer-owned S3 buckets are supported in MVP.
2. **Encryption ownership:** service-managed, customer-managed, or
   zero-knowledge/client-held; key-loss/recovery consequences.
3. **Deduplication boundary:** device or tenant scope (safer) vs global
   cross-tenant dedupe.
4. **Repository manifest/chunk protocol:** to be defined in the separate
   backup repository, not in this OAP integration contract.
5. **RPO/RTO/SLO targets:** retention-delete timing, immutability semantics,
   and restore verification evidence before production SLA commitments.
6. **Client bootstrap:** secure `oap-agent` bootstrap/update handoff for
   `oap-backup-client` without creating a shared credential/security boundary.
7. **Verification cadence:** full vs sampled periodic re-verification of
   retained recovery points.
8. **Restore test isolation:** whether restore-test targets are in-process
   sandboxes, disposable containers, or dedicated verification environments.

---

## Verification

- OpenSpec structure/requirement validation MUST pass for this file.
- The canonical contracts (run-state enum, event catalog, integrity-state
  enum, restore-state enum) MUST be imported, not re-described, by the
  standalone backup product's `oap-integration`, `backup-runs`,
  `event-contract`, `recovery-points`, and `integrity-verification` specs.
- Unit tests MUST cover state transitions, validation bounds, and negative/
  security paths owned by this capability.
- Integration tests MUST exercise each production boundary named by the
  requirements rather than only mocks.
- A capability MUST NOT be promoted from `DRAFT` until the documented
  production wiring and a representative end-to-end path are verified.

---

## Related Specifications

- `endpoint-agent` — `oap-agent` vs `oap-backup-client` identity boundary
- `event-bus` — OAP-side event translation target (core NATS, not JetStream)
- `multi-tenancy` — tenant isolation and RLS
- `auth-rbac` — authorization and role separation
- `commercial-licensing` — entitlement gating
- `audit-log` — audit trail for backup operations
- `notifications` — alert dispatch for backup failures
- `reporting` — backup health reporting
- `hitl-approval` — destructive restore approval workflow
- `secret-management` — encryption key and repository credential handling
- `data-model` — tenant/device identity model
- `observability` — backup health metrics
- `resilience` — retry, circuit breaking for backup service calls

---

## Change Control

Breaking changes to public APIs, persisted schemas, event subjects/envelopes,
state-machine values, security boundaries, or cross-service contracts MUST be
introduced with an explicit compatibility/migration plan and corresponding
updates to dependent specifications. The canonical contract block (run states,
event names, integrity states, restore states) is versioned; changes require
a contract-version increment.
