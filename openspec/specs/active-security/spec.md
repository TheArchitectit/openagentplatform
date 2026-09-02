# Active Security / EDR

> **Phase:** P3 — RMM parity gap
> **Status:** DRAFT
> **Source:** `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.5 (G-SEC-001..006), §2.7 (G-AV-001..003)
> **App Path:** `internal/security/`, `internal/security/crowdstrike/`, `internal/security/defender/`, `internal/security/sentinelone/`, `internal/security/siem/`, `internal/api/security_webhook.go`, `pkg/models/models_security.go`
> **Depends on:** openspec/specs/rmm-core/spec.md §14

---

## Description

The OAP posture engine already detects the *presence* of EDR agents (antivirus.go collector). It does not ingest the alerts those agents emit. Active Security closes that gap: webhook-based real-time ingest from CrowdStrike Falcon, Microsoft Defender for Endpoint, and SentinelOne; periodic reconciliation pulls to catch anything missed during outages; and SIEM forwarding to Splunk/ELK for customers who want their alerts in a SIEM of record.

This spec does **not** invent mechanisms. Each requirement is anchored to an existing pattern.

---

## User Story

**As** an MSP technician,
**I want** security events from CrowdStrike, Defender, and SentinelOne flowing into OAP in real time, correlated to the agents and sites I already manage, and optionally forwarded to my SIEM, with high-volume bursts handled without losing data,
**so that** I can respond to endpoint threats from one console without logging into three vendor portals, and satisfy my customers' compliance auditors who want a single audit trail.

---

## Requirements

### 1. Integration Model — Webhook + Reconciliation Pull

Two ingestion surfaces, both required:

1.1. **Webhook (primary, real-time)**: OAP exposes a public-facing endpoint `POST /api/v1/security-events/ingest` that receives EDR events as JSON. Each EDR vendor has a separate sub-path for vendor-specific payload parsing: `/ingest/crowdstrike`, `/ingest/defender`, `/ingest/sentinelone`. Authentication is via HMAC signature header (vendor-specific) or bearer token, configurable per integration.

1.2. **Reconciliation pull (secondary, fallback)**: a scheduled job polls the EDR API for events that occurred in a recent window (default last 10 minutes), catching anything missed during webhook outages. The poll cadence is configurable per integration (default 15 minutes). The poll window always overlaps the last successful poll time to avoid gaps.

1.3. Both paths write to the same `security_events` table. Events from the webhook are tagged `ingestion:webhook`; from the poll, `ingestion:poll`. The dedup key is `(provider, provider_event_id)` — a unique constraint prevents double-counting.

### 2. Supported EDR Vendors

2.1. **CrowdStrike Falcon** — primary target. Webhook events arrive via Falcon Real Time Response / Streaming API; reconciliation via Falcon Detection API. Severity, host ID, tactic/technique (MITRE ATT&CK), detection type, host agent ID.

2.2. **Microsoft Defender for Endpoint** — primary target. Webhook via Defender for Endpoint Alert API; reconciliation via the same API's `getAlerts` with a time filter. Maps to Azure AD tenant.

2.3. **SentinelOne** — secondary target. Webhook via Singularity XDR; reconciliation via the API's threat activity endpoint.

2.4. **OUT of scope**: SentinelOne Singularity Hologram, CrowdStrike Falcon LogScale, Defender for Cloud Apps. These are different product lines; can be added by implementing the `EDRProvider` interface.

### 3. Correlation to OAP Agents

3.1. Each EDR event includes a host identifier (CrowdStrike `device_id`, Defender `machineId`, SentinelOne `agent_id`). The integration layer maps this to an OAP `agent_id` via a `edr_agent_mapping` table: `id, org_id, provider, edr_host_id, agent_id, last_seen`.

3.2. When a webhook event arrives with an unknown `edr_host_id`, the integration creates a **virtual agent** in the `agents` table (`platform = "edr/{provider}"`, synthetic hostname) and records the mapping. The MSP can later promote the virtual agent to a real one or archive it.

3.3. Events correlated to an OAP agent emit a `security_event` alert via `oap.events.alerts`. The alert payload includes the EDR severity, tactic/technique, and a link to the EDR console for triage.

3.4. **OUT of scope**: automatic OAP agent installation on EDR-managed endpoints (deployment problem, not RMM).

### 4. SIEM Forwarding

4.1. OAP audit events and security events can be forwarded to a SIEM. The forwarder is a new package `internal/security/siem/` that subscribes to `oap.events.audit` and `oap.events.alerts`, filters to events matching a configured org/site/severity, and POSTs them to a configured SIEM endpoint.

4.2. Supported SIEMs: **Splunk HEC** (HTTP Event Collector), **Elastic/ELK** (Elasticsearch Bulk API), **generic syslog/HTTPS webhook** (CEF or LEEF format).

4.3. Credentials (HEC token, Elasticsearch API key) are stored via `SecretBackend` at `ref:oap://secret/siem/{siem_id}`. A `CircuitBreaker` per SIEM isolates failures.

4.4. A forwarder batch is flushed every 10 seconds or when 100 events accumulate, whichever comes first. Failed batches are retried with exponential backoff (3 attempts). A `siem_forward_lag` alert fires when the queue depth exceeds 10,000 events.

4.5. **OUT of scope**: real-time streaming via Kafka/HTTP/2 (HTTP/1.1 batched POST is the supported model). SIEM-side parsing/field-mapping customization is a per-SIEM config file, not arbitrary user mapping.

### 5. Data Model

New tables:

```
edr_integrations       — org_id, provider, name, credential_ref, webhook_secret, poll_interval_seconds,
                          enabled, last_poll_at, last_event_at
edr_agent_mapping     — org_id, provider, edr_host_id, agent_id, hostname, last_seen
security_events       — org_id, provider, provider_event_id, agent_id, severity, tactic, technique,
                          detection_type, payload JSONB, occurred_at, ingested_at, ingestion_method
siem_forwarders       — org_id, name, siem_type, endpoint, credential_ref, batch_size, batch_interval_seconds,
                          enabled, last_flush_at, last_error
siem_forward_queue    — forwarder_id, event_type, event_id, payload, attempts, next_retry_at
```

A unique constraint on `security_events (provider, provider_event_id)` enforces dedup.

### 6. API Surface

New route group `/api/v1/security`:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/edr/integrations` | Admin/Technician | List EDR integrations |
| POST | `/edr/integrations` | Admin | Create EDR integration |
| PUT | `/edr/integrations/{id}` | Admin | Update integration |
| DELETE | `/edr/integrations/{id}` | Admin | Remove integration |
| GET | `/events` | Admin/Technician | List security events (filterable) |
| GET | `/events/{id}` | Admin/Technician | Event detail |
| POST | `/siem/forwarders` | Admin | Register a SIEM forwarder |
| GET | `/siem/forwarders` | Admin/Technician | List forwarders |
| PUT | `/siem/forwarders/{id}` | Admin | Update forwarder |
| DELETE | `/siem/forwarders/{id}` | Admin | Remove forwarder |
| POST | `/ingest/{provider}` | HMAC/bearer | Webhook receive endpoint (public-facing) |

### 7. Rate Limiting and Burst Handling

7.1. EDR vendors can emit thousands of events per minute per tenant. The webhook handler reads the body, enqueues to a buffered channel (size 10,000), and returns 202 Accepted. The actual ingest is async.

7.2. A worker pool processes the channel with 16 workers by default (configurable). The webhook latency stays under 100ms.

7.3. When the channel is full, the webhook returns 503 Service Unavailable with `Retry-After: 30`. EDR vendors queue their own retries, so no events are lost.

7.4. The `internal/resilience/breaker.go` `CircuitBreaker` pattern is applied per integration: if a vendor's API is failing, the reconciliation pull backs off and the webhook fast-fails.

---

## Cross-References

- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.5, §2.7
- `internal/events/subjects.go` — NATS taxonomy
- `internal/secrets/backend.go` — `SecretBackend` interface
- `internal/resilience/breaker.go` — `CircuitBreaker` pattern
- `internal/scheduled/scheduler.go` — periodic poll pattern
- `internal/audit/audit.go` — hash-chained audit for compliance
- `openspec/specs/rmm-core/spec.md` §14
