# RMM Core Architecture

> **Version:** 1.0.0 | **Last Updated:** 2026-06-15 | **Status:** Authoritative Blueprint

---

## 1. Overview

The RMM Core is the heart of OpenAgentPlatform — it provides **device registration**, **monitoring (checks)**, **policy propagation**, **patch management**, **alert lifecycle**, **script execution**, **remote access**, and the **NATS JetStream orchestration layer** that ties everything together.

In a traditional RMM, automation is rigid: checks fire alerts, alerts notify humans, humans remediate. In OpenAgentPlatform's agent-first model, the RMM Core still provides the deterministic backbone — but every RMM event (check failure, alert, patch available) can be delegated to an LLM agent via the A2A protocol for intelligent triage, contextual risk assessment, and autonomous remediation (with human approval gates).

**App Path:** `backend/apps/rmm/`

---

## 2. Dual-Transport Architecture

OpenAgentPlatform uses **two transport layers** for different communication patterns. This is a battle-tested pattern used by Tactical RMM (Django REST + NATS) and MeshCentral (WebSocket relay + MQTT).

```
┌─────────────────────────────────────────────────────────────┐
│                    TRANSPORT SELECTION                       │
│                                                             │
│  ┌─────────────────────┐    ┌─────────────────────────┐    │
│  │    REST / HTTP       │    │   NATS JetStream        │    │
│  │    (JSON)            │    │   (msgpack + JSON)       │    │
│  │                     │    │                         │    │
│  │  • CRUD operations   │    │  • Real-time commands    │    │
│  │  • Periodic checkins │    │  • Streaming output     │    │
│  │  • Query & reporting │    │  • Event distribution   │    │
│  │  • Agent queries     │    │  • Agent→Server events  │    │
│  │                     │    │                         │    │
│  │  Request-response    │    │  Pub-sub + queue         │    │
│  └─────────────────────┘    └─────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

| Pattern | Transport | Why |
|---------|-----------|-----|
| Agent checks in with full inventory snapshot | REST | Large payload, infrequent, idempotent |
| Agent sends heartbeat | NATS | Small payload, frequent (60s), fire-and-forget |
| Server dispatches script to agent | NATS | Must arrive within seconds, needs streaming response |
| Agent streams script stdout | NATS | Chunked, real-time, one-way |
| Technician queries agent list | REST | Paginated, filtered, read-heavy |
| Check failure triggers alert → A2A task | NATS | Event-driven, pub-sub fan-out |

---

## 3. Data Models (10 Models)

### 3.1 Agent (`rmm_agent`)

Represents a managed device running the OAP endpoint binary.

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK, auto | uuid4 | Unique identifier |
| `org` | FK → Organization | NOT NULL | - | Owning organization |
| `agent_id` | StringField(255) | UNIQUE(org, agent_id) | - | Stable agent identity (persisted on disk) |
| `hostname` | StringField(255) | NOT NULL | - | Device hostname |
| `client` | FK → Client | NULL | NULL | MSP client assignment |
| `site` | FK → Site | NULL | NULL | Site within client |
| `platform` | CharField(20) | NOT NULL | "unknown" | windows / linux / macos / unknown |
| `status` | CharField(20) | NOT NULL | "pending" | Agent status (see enums) |
| `last_seen` | DateTimeField | NOT NULL | now() | Last heartbeat/checkin time |
| `operating_system` | StringField(255) | | "" | Full OS string (e.g., "Windows 11 Pro 23H2") |
| `goarch` | CharField(10) | | "" | amd64 / arm64 |
| `total_ram` | IntegerField | | 0 | Total RAM in MB |
| `disks` | JSONField | | [] | Array of {name, total_gb, free_gb, file_system} |
| `services` | JSONField | | [] | Array of {name, status, start_type} |
| `wmi_detail` | JSONField | | {} | Windows-specific WMI data |
| `public_ip` | GenericIPAddressField | | NULL | Public IP reported by agent |
| `boot_time` | DateTimeField | | NULL | Last boot time |
| `logged_in_username` | StringField(255) | | "" | Currently logged-in user |
| `needs_reboot` | BooleanField | | False | Agent reports pending reboot |
| `inventory` | JSONField | | {} | Flexible inventory catch-all |
| `tags` | ArrayField(StringField) | | [] | User-assigned tags for filtering |
| `mesh_token` | StringField(255) | | "" | Remote access session token |
| `created_at` | DateTimeField | | now() | Registration timestamp |
| `updated_at` | DateTimeField | | now() | Last update timestamp |
| `deleted_at` | DateTimeField | NULL | NULL | Soft delete timestamp |

**Indexes:** `(org, status)`, `(org, client, site)`, `(org, platform)`, `(org, last_seen)`, GIN on `tags`

---

### 3.2 Check (`rmm_check`)

Defines a monitoring check template. Uses **flat-table polymorphism** — all check types share one table with a `check_type` discriminator and type-specific nullable fields. This avoids JOIN complexity at scale (validated by Tactical RMM).

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `org` | FK → Organization | NOT NULL | - | |
| `name` | StringField(255) | NOT NULL | - | Human-readable check name |
| `check_type` | CharField(20) | NOT NULL | - | Type discriminator (10 types) |
| `interval_seconds` | IntegerField | CHECK(>=30) | 300 | How often to run (min 30s) |
| `timeout_seconds` | IntegerField | CHECK(<=3600) | 120 | Max execution time |
| `config` | JSONField | | {} | Type-specific configuration |
| `fail_threshold` | IntegerField | | 1 | Consecutive failures before alert |
| `warning_threshold` | FloatField | NULL | NULL | Warning level value |
| `error_threshold` | FloatField | NULL | NULL | Error level value |
| `alert_severity` | CharField(10) | | "warning" | Alert severity if triggered |
| `is_template` | BooleanField | | False | Can be applied to multiple agents |
| `last_status` | CharField(20) | | "pending" | Most recent execution status |
| `enabled` | BooleanField | | True | Check is active |

**Indexes:** `(org, check_type)`, `(org, is_template)`, `(org, last_status)`

**Why flat-table polymorphism?** Tactical RMM validates that a `CHECK` constraint + discriminator on one table out-performs joined-table inheritance at 100K+ check results. Type-specific fields are simply nullable on the same row.

---

### 3.3 AgentCheck (`rmm_agent_check`)

Junction table linking a Check to a specific Agent instance.

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `agent` | FK → Agent | NOT NULL | - | Target agent |
| `check` | FK → Check | NOT NULL | - | Check definition |
| `is_enabled` | BooleanField | | True | Per-agent enable/disable |
| `next_run_at` | DateTimeField | | now() | Next scheduled execution |
| `last_run_at` | DateTimeField | NULL | NULL | Last execution time |
| `override_config` | JSONField | NULL | NULL | Agent-specific config override |

**Constraints:** `UNIQUE(agent, check)`

---

### 3.4 CheckResult (`rmm_check_result`)

Stores the outcome of each check execution. Time-series data — high write volume, read by time range.

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `agent_check` | FK → AgentCheck | NOT NULL | - | Which agent+check |
| `org` | FK → Organization | NOT NULL | - | Denormalized for query performance |
| `status` | CharField(20) | NOT NULL | - | passing / failing / warning / error |
| `value` | JSONField | | {} | Check output (disk free %, CPU %, etc.) |
| `duration_ms` | IntegerField | CHECK(>=0) | 0 | Execution time |
| `execution_start` | DateTimeField | NOT NULL | - | When check started |
| `execution_end` | DateTimeField | NOT NULL | - | When check completed |
| `error_message` | TextField | | "" | Error details if failed |

**Indexes:** `(agent, check, -execution_start)`, `(org, status, -execution_start)`

**Pruning:** `check_history_prune_days` (default 30) — Celery task prunes daily.

---

### 3.5 Policy (`rmm_policy`)

Defines checks, automated tasks, patch behavior, and alert routing. Follows **Client > Site > Agent** hierarchy with enforcement/exclusion semantics.

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `org` | FK → Organization | NOT NULL | - | |
| `name` | StringField(255) | NOT NULL | - | Policy name |
| `enforcement_mode` | CharField(20) | | "inherit" | inherit / enforce / exclude |
| `priority` | IntegerField | | 0 | Higher = wins conflict |
| `checks` | JSONField | | [] | Check definitions to apply |
| `automated_tasks` | JSONField | | [] | Task definitions to apply |
| `win_update_policy` | JSONField | | {} | Patch approval behavior per severity |
| `alert_routing` | JSONField | | {} | Alert → notification channel mapping |
| `is_active` | BooleanField | | True | |
| `created_at` | DateTimeField | | now() | |
| `updated_at` | DateTimeField | | now() | |

**Indexes:** `(org, priority)`

**Propagation:** Policies are evaluated bottom-up: Agent → Site → Client → Organization. The `enforce` flag discards agent-level overrides. `block_policy_inheritance` stops propagation. Excluded entities tracked via M2M fields.

---

### 3.6 PolicyScope (`rmm_policy_scope`)

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `policy` | FK → Policy | NOT NULL | - | Parent policy |
| `scope_type` | CharField(10) | NOT NULL | - | "client" / "site" / "agent" |
| `client` | FK → Client | NULL | NULL | Set if scope_type = client |
| `site` | FK → Site | NULL | NULL | Set if scope_type = site |
| `agent` | FK → Agent | NULL | NULL | Set if scope_type = agent |

**Constraint:** `CHECK(XOR)` — exactly one of client/site/agent must be set per row.

---

### 3.7 WinUpdate (`rmm_win_update`)

Per-agent, per-update patch record with approval workflow.

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `agent` | FK → Agent | NOT NULL | - | |
| `org` | FK → Organization | NOT NULL | - | |
| `kb` | StringField(50) | | "" | KB article ID |
| `guid` | StringField(255) | | "" | Update GUID |
| `title` | TextField | | "" | Update title |
| `severity` | CharField(10) | | "other" | critical/important/moderate/low/other |
| `state` | CharField(20) | | "scanned" | 8-state lifecycle |
| `action` | CharField(20) | | "inherit" | inherit/approve/ignore/nothing |
| `cve_ids` | JSONField | | [] | Associated CVE IDs |
| `installed` | BooleanField | | False | |
| `downloaded` | BooleanField | | False | |
| `result` | TextField | | "" | Last install result |
| `approved_by` | FK → User | NULL | NULL | Who approved |
| `date_installed` | DateTimeField | NULL | NULL | |

**Constraints:** `UNIQUE(agent, kb)`

---

### 3.8 AutomatedTask (`rmm_automated_task`)

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `org` | FK → Organization | NOT NULL | - | |
| `name` | StringField(255) | NOT NULL | - | |
| `task_type` | CharField(20) | NOT NULL | - | daily/weekly/monthly/monthly_dow/onboarding/run_once/check_failure |
| `schedule_bitmask` | BigIntegerField | CHECK([0, 2^21)) | 0 | 21-bit bitmask encoding schedule |
| `actions` | JSONField | NOT NULL | [] | Array of {type, script/cmd, timeout, args, env_vars} |
| `next_run_at` | DateTimeField | | now() | |
| `assigned_check` | FK → Check | NULL | NULL | If task_type = check_failure |
| `supported_platforms` | ArrayField | | [] | windows/linux/macos |
| `is_template` | BooleanField | | False | |

**Why bitmask scheduling?** Compact, queryable with bitwise ops, avoids cron parsing. Bit positions: 0-6 (weekdays Mon-Sun), 7-10 (hours), 11-17 (days of month), 18-20 (months). This is validated by Tactical RMM's production model.

---

### 3.9 Alert (`rmm_alert`)

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `org` | FK → Organization | NOT NULL | - | |
| `severity` | CharField(10) | NOT NULL | "info" | critical/high/medium/low/info |
| `state` | CharField(20) | NOT NULL | "new" | 6-state lifecycle |
| `agent` | FK → Agent | NOT NULL | - | |
| `check` | FK → Check | NULL | NULL | Triggering check |
| `dedup_key` | StringField(255) | | "" | Deduplication key |
| `message` | TextField | | "" | Alert description |
| `notification_channels` | JSONField | | [] | {email: [...], slack: [...], webhook: [...]} |
| `fired_at` | DateTimeField | | now() | |
| `acknowledged_at` | DateTimeField | NULL | NULL | |
| `resolved_on` | DateTimeField | NULL | NULL | |
| `snooze_until` | DateTimeField | NULL | NULL | |

**Indexes:** `(org, state, -fired_at)`, `(org, severity, -fired_at)`, `(dedup_key)`

**Pruning:** `resolved_alerts_prune_days` — resolved alerts pruned on schedule.

---

### 3.10 ScriptResult (`rmm_script_result`)

| Field | Type | Constraints | Default | Description |
|-------|------|-------------|---------|-------------|
| `id` | UUID | PK | uuid4 | |
| `agent` | FK → Agent | NOT NULL | - | |
| `org` | FK → Organization | NOT NULL | - | |
| `script` | FK → Script | NULL | NULL | |
| `runtime` | CharField(20) | NOT NULL | - | powershell/cmd/python/shell/nushell |
| `state` | CharField(20) | NOT NULL | "pending" | 6-state lifecycle |
| `stdout` | TextField | | "" | |
| `stderr` | TextField | | "" | |
| `exit_code` | IntegerField | NULL | NULL | |
| `execution_time` | FloatField | | 0.0 | Seconds |
| `created_at` | DateTimeField | | now() | |

**Pruning:** `agent_history_prune_days` (default 60).

---

## 4. Enums (12)

| Enum | Values | Description |
|------|--------|-------------|
| `AgentStatus` | `pending`, `online`, `offline`, `degraded`, `uninstalled` | Device lifecycle states |
| `AgentPlatform` | `windows`, `linux`, `macos`, `unknown` | Operating system family |
| `CheckType` | `ping`, `cpu`, `memory`, `disk`, `service`, `script`, `event_log`, `process`, `wmi`, `custom` | Monitoring check types |
| `CheckStatus` | `passing`, `failing`, `warning`, `pending`, `paused` | Check execution outcomes |
| `PolicyEnforcementMode` | `inherit`, `enforce`, `exclude` | How policies apply at each hierarchy level |
| `WinUpdateState` | `scanned`, `pending_approval`, `approved`, `rejected`, `installing`, `installed`, `failed`, `reboot_required` | Patch lifecycle (8 states) |
| `AutomatedTaskActionType` | `cmd`, `powershell`, `python`, `shell`, `nushell`, `deno`, `reboot`, `send_keys` | Script/command types |
| `AlertSeverity` | `critical`, `high`, `medium`, `low`, `info` | 5 severity levels |
| `AlertState` | `new`, `acknowledged`, `in_progress`, `resolved`, `snoozed`, `closed` | Alert lifecycle |
| `ScriptRuntime` | `powershell`, `cmd`, `python`, `shell`, `nushell` | Supported script runtimes |
| `RemoteSessionProtocol` | `vnc`, `rdp`, `ssh`, `winrm`, `webterminal` | Remote access methods |
| `RemoteSessionState` | `requested`, `pending_agent`, `active`, `transferring`, `closed`, `failed`, `timeout` | Session lifecycle |

---

## 5. NATS Subject Taxonomy

All subjects are organized into three groups: **Agent→Server** (msgpack), **Server→Agent** (JSON per-agent inbox), and **Broadcast** (JSON).

### 5.1 Agent → Server (msgpack serialization)

| Subject | Direction | Format | Publisher | Subscriber | Trigger | Description |
|---------|-----------|--------|-----------|------------|---------|-------------|
| `rmm.agent.heartbeat` | A→S | msgpack | Agent | CheckinHandler | Every 60s | Lightweight online ping |
| `rmm.agent.checkin` | A→S | msgpack | Agent | CheckinHandler | Every 5-15 min | Full state snapshot |
| `rmm.check.result.{agent_id}` | A→S | msgpack | Agent | CheckEngine | Check completes | Check execution result |
| `rmm.script.result.{agent_id}` | A→S | msgpack | Agent | ScriptEngine | Script finishes | Final script output |
| `rmm.script.chunk.{agent_id}` | A→S | msgpack | Agent | ScriptEngine | Streaming line | Real-time stdout/stderr chunk |
| `rmm.winupdate.scan.{agent_id}` | A→S | msgpack | Agent | PatchEngine | Scan completes | Available patches |
| `rmm.winupdate.install.{agent_id}` | A→S | msgpack | Agent | PatchEngine | Install finishes | Patch install result |
| `rmm.agent.inventory.{agent_id}` | A→S | msgpack | Agent | InventoryCollector | Inventory changes | Hardware/software inventory |
| `rmm.remote.session.event.{agent_id}` | A→S | msgpack | Agent | RemoteAccess | Session event | Remote session lifecycle |

### 5.2 Server → Agent (JSON serialization, per-agent inbox)

| Subject | Direction | Format | Publisher | Subscriber | Trigger | Description |
|---------|-----------|--------|-----------|------------|---------|-------------|
| `rmm.cmd.{agent_id}.script.run` | S→A | JSON | ScriptEngine | Agent | User/API request | Execute a script |
| `rmm.cmd.{agent_id}.script.cancel` | S→A | JSON | ScriptEngine | Agent | Cancel request | Cancel running script |
| `rmm.cmd.{agent_id}.check.run` | S→A | JSON | CheckEngine | Agent | Schedule/reques | Run a check now |
| `rmm.cmd.{agent_id}.winupdate.install` | S→A | JSON | PatchEngine | Agent | Approval | Install approved patches |
| `rmm.cmd.{agent_id}.winupdate.scan` | S→A | JSON | PatchEngine | Agent | Schedule/trigger | Scan for patches |
| `rmm.cmd.{agent_id}.sync` | S→A | JSON | Propagation | Agent | Policy change | Sync agent state |
| `rmm.cmd.{agent_id}.agent.update` | S→A | JSON | API Server | Agent | Update available | Update agent binary |
| `rmm.cmd.{agent_id}.remote.open` | S→A | JSON | RemoteAccess | Agent | User request | Open remote session |
| `rmm.cmd.{agent_id}.remote.close` | S→A | JSON | RemoteAccess | Agent | End request | Close remote session |
| `rmm.cmd.{agent_id}.policy.push` | S→A | JSON | Propagation | Agent | Policy change | Push updated policies |
| `rmm.cmd.{agent_id}.inventory.refresh` | S→A | JSON | InventoryCollector | Agent | Request | Refresh hardware/software inventory |

### 5.3 Broadcast

| Subject | Direction | Format | Publisher | Subscriber | Trigger | Description |
|---------|-----------|--------|-----------|------------|---------|-------------|
| `rmm.broadcast.all` | S→All | JSON | API Server | All Agents | Global event | System-wide broadcast |
| `rmm.broadcast.org.{org_id}` | S→Org | JSON | API Server | Org Agents | Org event | Organization-scoped broadcast |

### 5.4 Serialization Choice: Why msgpack + JSON?

- **msgpack for Agent→Server**: Binary efficiency on constrained endpoints. Agents are Go binaries that serialize msgpack natively and cheaply. Bandwidth matters on remote connections.
- **JSON for Server→Agent**: Human-readable commands for debugging. Agent dispatches on a `Func` string field via switch statement (validated by Tactical RMM's `agent/rpc.go`).
- **Tagged msgpack fields**: Enable schema evolution without breaking older agents.

---

## 6. State Machines (5)

### 6.1 Alert State Machine

```
                 ┌──────────┐
         reopen  │          │  snooze
      ┌──────────│ resolved │←────────┐
      │          │          │         │
      │          └──────────┘         │
      │                               │
      ▼                               │
  ┌────────┐  acknowledge   ┌──────────────┐
  │        │────────────────→│              │
  │  new   │                 │ acknowledged │
  │        │←────────────────│              │
  └────────┘   acknowledge   └──────┬───────┘
      │                               │
      │ resolve                       │ resolve
      ▼                               ▼
  ┌──────────┐                ┌──────────────┐
  │          │  snooze        │              │
  │ resolved │←───────────────│ in_progress  │
  │          │                │              │
  └──────────┘                └──────────────┘
      ▲
      │  close (from any state)
      │
  ┌───┴────┐
  │        │
  │ closed │  ←───────── terminal
  │        │
  └────────┘
```

| Transition | From States | To State | Trigger |
|-----------|-------------|----------|---------|
| acknowledge | new, snoozed | acknowledged | Technician acknowledges |
| resolve | new, acknowledged, in_progress | resolved | Issue fixed or auto-resolved |
| snooze | new, acknowledged, in_progress | snoozed | User snoozes for a period |
| close | any | closed | Manual close or auto-prune |
| reopen | resolved, closed | new | Issue recurs |

### 6.2 WinUpdate State Machine

```
  scanned → pending_approval → approved → installing → installed → reboot_required
                     │                       │
                     ▼                       ▼
                  rejected                failed → (retry → installing)
```

| Transition | From State | To State | Trigger |
|-----------|-----------|----------|---------|
| auto_approve | pending_approval | approved | Policy auto-approves by severity |
| approve | pending_approval | approved | Manual approval |
| reject | pending_approval | rejected | Manual rejection |
| start_install | approved | installing | Deploy command |
| complete_install | installing | installed | Agent reports success |
| fail_install | installing | failed | Agent reports error |
| mark_reboot_required | installed | reboot_required | Agent needs restart |
| retry | failed | installing | Retry failed install |

### 6.3 Agent State Machine

```
  pending → online → offline
               ↕
           degraded
               ↓
           uninstalled
```

| Transition | From States | To State | Trigger |
|-----------|-------------|----------|---------|
| check_in | pending, offline, degraded | online | Heartbeat/checkin received |
| mark_offline | online | offline | 90s heartbeat TTL exceeded |
| mark_degraded | online | degraded | Consistent check failures |
| recover | degraded | online | Healthy check streak |
| uninstall | any | uninstalled | Uninstall command |

### 6.4 ScriptResult State Machine

```
  pending → running → success
                   → error
                   → timeout
     ↓
  cancelled
```

| Transition | From States | To State | Trigger |
|-----------|-----------|----------|---------|
| start | pending | running | Agent begins execution |
| complete | running | success | Exit code 0 |
| fail | running | error | Non-zero exit code |
| timeout | running | timeout | Exceeded timeout_seconds |
| cancel | pending, running | cancelled | User cancels |

### 6.5 RemoteSession State Machine

```
  requested → pending_agent → active → transferring → closed
                                ↓
                              failed
                             timeout
```

| Transition | From States | To State | Trigger |
|-----------|-----------|----------|---------|
| agent_ack | pending_agent | active | Agent confirms session |
| transfer | active | transferring | File transfer in progress |
| close | active, transferring | closed | User/agent closes |
| fail | pending_agent, active | failed | Connection error |
| timeout | active | timeout | Inactivity timeout |

---

## 7. Services (10)

| # | Service | File | Responsibility | Key Methods |
|---|---------|------|----------------|-------------|
| 1 | CheckEngine | `services/check_engine.py` | Scheduling, dispatch, result processing, alert evaluation | `get_due_checks()`, `dispatch_check()`, `process_result()`, `evaluate_thresholds()` |
| 2 | PolicyEngine | `services/policy_engine.py` | Hierarchical resolution, enforcement/exclusion, propagation | `resolve_effective_policy()`, `propagate_changes()`, `evaluate_exclusions()` |
| 3 | AlertEngine | `services/alert_engine.py` | Dedup key generation, state machine driving, notification dispatch | `fire_alert()`, `deduplicate()`, `drive_state()`, `send_notifications()` |
| 4 | PatchEngine | `services/patch_engine.py` | Scan orchestration, approval workflow, batch deployment, reboot coordination, CVE correlation | `scan_agent()`, `approve_batch()`, `deploy()`, `coordinate_reboot()`, `correlate_cve()` |
| 5 | ScriptEngine | `services/script_engine.py` | Script library management, dispatch, streaming output relay, result storage | `dispatch_script()`, `stream_output()`, `store_result()`, `cancel_execution()` |
| 6 | InventoryCollector | `services/inventory_collector.py` | Agent inventory ingest, software catalog, delta detection, change events | `ingest_inventory()`, `detect_changes()`, `emit_change_event()` |
| 7 | CheckinHandler | `services/checkin_handler.py` | Heartbeat processing, full check-in merges, online/offline transitions | `handle_heartbeat()`, `handle_full_checkin()`, `transition_status()` |
| 8 | Propagation | `services/propagation.py` | Policy-to-agent push, delta computation, NATS publish | `compute_delta()`, `push_to_agent()`, `publish_policy_update()` |
| 9 | Enforcement | `services/enforcement.py` | Policy scope evaluation, exclusion enforcement, conflict resolution | `evaluate_scope()`, `enforce_exclusions()`, `resolve_conflicts()` |
| 10 | RemoteAccess | `services/remote_access.py` | Session establishment, relay coordination, recording, audit events | `establish_session()`, `coordinate_relay()`, `start_recording()`, `emit_audit_event()` |

---

## 8. Celery Tasks (7 Packages)

| Package | Tasks | Schedule |
|---------|-------|----------|
| `check_tasks` | `run_due_checks`, `evaluate_check_result`, `escalate_failing_check` | Beat: every 30s |
| `patch_tasks` | `scan_agent_patches`, `approve_patch_batch`, `deploy_patches`, `verify_patch_install` | Beat: daily 02:00 |
| `alert_tasks` | `process_alert`, `send_alert_notifications`, `expire_snoozed_alerts`, `escalate_unresolved` | Beat: every 60s |
| `policy_tasks` | `propagate_policy_changes`, `enforce_policy_exclusions`, `sync_agent_policies` | Beat: on change |
| `inventory_tasks` | `collect_agent_inventory`, `detect_software_changes`, `purge_stale_inventory` | Beat: hourly |
| `script_tasks` | `dispatch_script_run`, `process_script_output`, `handle_script_timeout` | Beat: every 30s |
| `celery` (registration) | `register_periodic_tasks` | 15+ beat schedule entries |

---

## 9. Key Design Decisions

| Decision | Rationale | Validated By |
|----------|-----------|-------------|
| **Flat-table polymorphism for checks** | All check types on one table with discriminator + nullable type-specific fields. Avoids JOIN complexity at 100K+ results. Simpler queries, faster scans. | Tactical RMM `checks/models.py` — production-proven at scale |
| **Bitmask scheduling for tasks** | 21-bit field encodes day-of-week, hour, day-of-month, month. Compact, queryable with bitwise ops, avoids cron string parsing entirely. | Tactical RMM `autotasks/models.py` — avoids crontab complexity |
| **Client > Site > Agent hierarchy** | Mirrors MSP organizational model. Policies propagate top-down with enforcement/exclusion. Provides natural multi-tenancy boundaries. | Standard RMM industry model |
| **msgpack for agent + JSON for server** | Binary efficiency on constrained endpoints (Go msgpack is fast+small). JSON for server-to-server (debuggable, no parsing overhead on server side). | Tactical RMM agent binary pattern |
| **Separate CheckResult from Check** | Check results are time-series (high write, time-range read). Keeping them separate from check definitions allows different retention & pruning strategies. | RMM monitoring best practice |
| **NATS over Kafka** | Lighter operational overhead at RMM scale (<100K endpoints). Built-in persistence via JetStream. Go client is first-class. No ZooKeeper/KRaft overhead. | Operational simplicity |
| **Dual-transport (REST + NATS)** | REST for CRUD/query (standard, cacheable, paginated). NATS for real-time (sub-second command dispatch, streaming, events). No single transport can serve both patterns well. | Tactical RMM + MeshCentral both validate this |

---

## 10. Implementation Steps (28 Ordered, Each Verifiable)

| Step | Produces | Depends On |
|------|----------|------------|
| 1. Create Django app `apps/rmm/` with `apps.py`, `__init__.py`, `enums.py`, `constants.py` | App scaffold | - |
| 2. Implement `models/base.py`: `UUIDPrimaryKeyMixin`, `TimestampedMixin`, `OrgScopedMixin`, `SoftDeleteMixin` | Base mixin classes | Step 1 |
| 3. Implement `models/agent.py`: full Agent model + `test_models_agent.py` | Agent model + tests | Step 2 |
| 4. Implement `models/check.py`: Check, AgentCheck, CheckResult + `test_models_check.py` | Check models + tests | Step 2 |
| 5. Implement `models/policy.py`: Policy, PolicyScope + `test_models_policy.py` | Policy models + tests | Step 2 |
| 6. Implement `models/win_update.py`: WinUpdate + `test_models_win_update.py` | WinUpdate model + tests | Step 2 |
| 7. Implement `models/automated_task.py`: AutomatedTask + `test_models_automated_task.py` | Task model + tests | Step 2 |
| 8. Implement `models/installed_software.py`, `models/alert.py`, `models/script_result.py`, `models/remote_session.py` | Remaining models | Step 2 |
| 9. Create migrations 0001-0007 and run `python manage.py migrate` | Database tables | Steps 3-8 |
| 10. Implement state machines in `state_machines/`: alert, win_update, agent, script_result, remote_session | State machine classes | Steps 3-8 |
| 11. Write `test_state_machines.py` covering all transitions and invalid transition rejections | State machine tests | Step 10 |
| 12. Implement `nats/subjects.py` and `nats/client.py` (async singleton, stream creation) | NATS client | Step 1 |
| 13. Implement `nats/publishers.py` (CommandPublisher with all send_* methods) | NATS publishers | Step 12 |
| 14. Implement `nats/serializers.py` (msgpack for agent, orjson for server) | Serializers | Step 12 |
| 15. Implement `nats/consumers.py`: 8 consumer coroutines registered in `apps.ready()` | NATS consumers | Steps 13-14 |
| 16. Write `test_nats_publishers.py` and `test_nats_consumers.py` with mock NATS | NATS tests | Steps 13-15 |
| 17. Implement `services/checkin_handler.py`: heartbeat and full check-in | Checkin service | Steps 9, 15 |
| 18. Implement `services/check_engine.py`: scheduling, dispatch, result processing | Check service | Steps 9, 15 |
| 19. Implement `services/policy_engine.py`: hierarchical resolution, propagation | Policy service | Steps 9, 15 |
| 20. Implement `services/alert_engine.py`: dedup, state machine, notification | Alert service | Steps 9, 10 |
| 21. Implement `services/patch_engine.py`: scan, approval, deployment, CVE | Patch service | Steps 9, 15 |
| 22. Implement `services/script_engine.py`: library, dispatch, streaming | Script service | Steps 9, 15 |
| 23. Implement `services/inventory_collector.py`, `services/propagation.py`, `services/enforcement.py`, `services/remote_access.py` | Remaining services | Steps 9, 15 |
| 24. Write service tests: `test_services_*.py` for all services | Service tests | Steps 17-23 |
| 25. Implement Celery tasks in `tasks/`: all 7 packages with registration | Background tasks | Steps 17-23 |
| 26. Write task tests: `test_tasks_*.py` | Task tests | Step 25 |
| 27. Implement API serializers, viewsets, URLs, permissions, pagination, filters | REST API | Steps 3-8 |
| 28. Write API tests: `test_api_*.py` for all endpoints | API tests | Step 27 |
