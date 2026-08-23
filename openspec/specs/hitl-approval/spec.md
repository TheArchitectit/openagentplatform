# Human-in-the-Loop Approval (HITL)

> **Phase:** 2 | **Status:** COMPLETE | **Spec:** openspec/specs/hitl-approval/spec.md

---

## Description

Approval workflow for agent actions that require human authorization before execution. Covers request creation, notification delivery, approval/rejection flow, timeout escalation, and full audit trail.

---

## User Story

As an **admin**, I want to review and approve agent-initiated actions before they execute, so that I maintain control over high-impact operations (secret access, patch deployment, policy changes, external API calls).

---

## Requirements

### R1: Approval Request API

- **R1.1:** `POST /a2a/v1/approvals` — create approval request (action_type, payload, requester_agent_id, urgency)
- **R1.2:** `GET /a2a/v1/approvals` — list pending approvals (filterable by status, urgency, requester)
- **R1.3:** `GET /a2a/v1/approvals/{id}` — get approval detail (full payload, decision history, comments)
- **R1.4:** `POST /a2a/v1/approvals/{id}/approve` — approve (with optional comment, scope限定)
- **R1.5:** `POST /a2a/v1/approvals/{id}/reject` — reject (with required reason)
- **R1.6:** Approval states: `pending` → `approved` | `rejected` | `expired` | `escalated`

### R2: Notification Delivery

- **R2.1:** On approval request creation, notify configured channels (email, Slack, webhook)
- **R2.2:** Notification includes: action description, requester agent, urgency level, approval URL
- **R2.3:** Notification templates are configurable per approval type
- **R2.4:** Re-notification after configurable delay if still pending (max 3 re-notifications)

### R3: Timeout + Escalation

- **R3.1:** Configurable timeout per approval type (default: 4 hours)
- **R3.2:** On timeout: auto-reject OR auto-escalate (configurable per action_type)
- **R3.3:** Escalation routes to a higher-privilege approval group
- **R3.4:** Maximum escalation depth: 3 levels
- **R3.5:** After max escalation: auto-reject with alert to admin

### R4: Audit Trail

- **R4.1:** Every approval action (create, approve, reject, escalate, timeout) is logged with actor + timestamp
- **R4.2:** Approval decisions include justification (required for reject, optional for approve)
- **R4.3:** Audit log is immutable and queryable by approval_id, actor, date range
- **R4.4:** Integration with `internal/audit` package

### R5: Agent Integration

- **R5.1:** Agent task can declare `requires_approval: true` with approval_config
- **R5.2:** Task enters `input-required` state while approval is pending
- **R5.3:** On approval, task resumes with approval context in message parts
- **R5.4:** On rejection, task transitions to `failed` with rejection reason
- **R5.5:** On timeout/expiry, task follows configured timeout action

### R6: Frontend Approval Queue

- **R6.1:** Approval queue page (list of pending approvals with urgency badges)
- **R6.2:** Approval detail view (full payload, agent info, history)
- **R6.3:** One-click approve/reject with comment field
- **R6.4:** Batch approve/reject for bulk operations
- **R6.5:** Real-time updates (SSE/WebSocket) for new approval requests

---

## Approval Types

| Type | Default Timeout | Default Action | Description |
|------|----------------|----------------|-------------|
| `secret_access` | 1 hour | reject | Agent requests secret retrieval |
| `patch_deploy` | 4 hours | escalate | Agent requests patch installation |
| `policy_change` | 8 hours | reject | Agent requests policy modification |
| `external_api` | 2 hours | reject | Agent calls external API |
| `script_execute` | 30 minutes | reject | Agent requests script execution on endpoint |
| `config_change` | 4 hours | escalate | Agent requests system configuration change |

---

## References

- **Roadmap:** [docs/architecture/ROADMAP_AND_SPRINTS.md](../../docs/architecture/ROADMAP_AND_SPRINTS.md) — Sprint 2.7
- **A2A Task Manager:** [openspec/specs/a2a-task-manager/spec.md](../a2a-task-manager/spec.md)
- **Auth & RBAC:** [openspec/specs/auth-rbac/spec.md](../auth-rbac/spec.md)
