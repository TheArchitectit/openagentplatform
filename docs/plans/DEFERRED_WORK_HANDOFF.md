# Deferred Work Handoff — Master

**Version:** 1.2.0
**Status:** SEAM_VERIFIED — baseline pinned, dependency order fixed, vocabulary final; RMM-00 COMPLETE
**Date:** 2026-08-24
**Baseline:** v1.2.0 (`git rev-parse --short HEAD` = `4194dac`)
**Role:** Master / coordinator for deferred work left out of v1.2.0. This is the
single authoritative entry point for the deferred RMM and RELAY work and the
deferred spec-review publication handoff.
**Related (operational):**
- [SPEC_REVIEW_BUNDLE_HANDOFF.md](../reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md) — the
  spec-review publication runbook (deferred `BLOCKED_DECISION` until a remote URL
  is supplied).
- [SPRINT_WIRING_REMEDIATION_PLAN.md](../SPRINT_WIRING_REMEDIATION_PLAN.md) — W1-W8
  wiring remediation, the prior completed wave this document builds upon.
- [GAP_ANALYSIS_RMM_PLATFORM.md](../GAP_ANALYSIS_RMM_PLATFORM.md) — RMM parity gaps
  this document schedules as deferred work.

---

## 1. Purpose and Inventory

v1.2.0 completed the W1-W8 wiring remediation and the OpenSpec P0-P2 audit pass.
Two categories of deferred work remain, tracked here:

1. **RMM parity gaps** — Windows patch automation, maintenance windows,
   agent auto-update, offline-SLA alerting, scheduled report digests, and the
   remaining RMM capacity checks identified against the shipped Go implementation.
2. **RELAY subsystem** — the relay service is shipped as a parked library pending a
   binary wiring decision; idle-reap is only partially refined; tenant isolation,
   observability, and Python-contract reconciliation remain open.

In addition there is one **operational deferred item**:

3. **Spec-review bundle publication** — publishing the existing standalone
   spec-review repository to a user-designated remote. Deferred as `BLOCKED_DECISION`
   until the user supplies a remote URL (section 5.3, runbook reference above).

No deferred item changes production code on `main` until it is resolved.

---

## 2. Baseline

### 2.1 v1.2.0 Is the Only Valid Baseline

Every deferred-work contract is validated **against v1.2.0**, not against prose or
later uncommitted changes.

| Baseline fact | Value |
|---------------|-------|
| Release | v1.2.0 (Wiring Remediation & OpenSpec Reconciliation) |
| Baseline commit | `4194dac` (`docs(release): complete v1.2.0 change coverage`) |
| Source of truth for statuses | `docs/SPRINT_WIRING_REMEDIATION_PLAN.md`, openspec `rmm-core` |
| Prior wave | W1-W8 all wired and pushed on `main` (2026-08-23) |

### 2.2 What Is NOT Deferred

Items already shipped in v1.2.0 are **out of scope** and must not be re-opened:

- Heartbeat persistence (W1), single check-result owner (W2).
- Notifier registry wiring (W3), reporting store/scheduler (W4), remote shell (W5).
- Tenancy wiring (W6), A2A adapter proxy contract (W7), W8 correctness items.
- OpenSpec P0-P2 audit completion (all committed on `main`).

---

## 3. Dependency Ordering

Work orders strictly. An item may not leave the queue until its order position is
reached. Two parallel tracks (RMM, RELAY) share the same foundation.

```
F0. Baseline (v1.2.0) — established
      ├──► RMM TRACK          RELAY TRACK
      │     RMM-00            RELAY-00
      │     RMM-01            RELAY-01
      │     RMM-02            RELAY-02
      │     RMM-03            RELAY-03
      │     RMM-04            RELAY-04
      │     RMM-05            RELAY-05
      │     ...               RELAY-06
      └────────────► F9. Joint closeout ◄───┘
```

### 3.1 RMM Track Order

| Order | Item | Depends on |
|------:|------|------------|
| 1 | RMM-00 — Operations foundation and decision register | F0 (established) |
| 2 | RMM-01 — Offline-agent SLA alerting | RMM-00 + threshold/cooldown decision | **COMPLETE** (shipped `6ff3a17`) |
| 3 | RMM-02 — Fleet alert-suppression maintenance windows | RMM-00 + window/precedence decisions | **COMPLETE** (shipped `4cf3602`) |
| 4 | RMM-03 — Windows Update per-KB lifecycle | RMM-00 + state/identity decisions | **COMPLETE** (implemented and verified in-tree) |
| 5 | RMM-04 — Reboot coordination | RMM-00 + approved reboot command contract | **COMPLETE** (implemented and verified in-tree) |
| 6 | RMM-05 — CVE-to-patch correlation | RMM-00 + source/provenance decisions |
| 7 | RMM-06 — Scheduled automation decision gate | RMM-00 + approved scheduling grammar |
| 8 | RMM-07 — Agent self-update decision gate | RMM-00 + trust/rollout/transport decisions |
| 9 | RMM-08 — VNC/RDP decision gate | RMM-00 + binary transport/consent/security decisions |

### 3.2 RELAY Track Order

| Order | Item | Depends on |
|------:|------|------------|
| 1 | RELAY-00 — Architecture and security decisions | F0 (established) |
| 2 | RELAY-01 — Binary/config/deployment foundation; fail-closed WSS | RELAY-00 |
| 3 | RELAY-02 — Issued identity and entitlement decision gate | RELAY-00; blocks admission |
| 4 | RELAY-03 — WSS rendezvous decision + contingent forwarding | RELAY-01 + approved/implemented RELAY-02 |
| 5 | RELAY-04 — Secure metering/observability contract | RELAY-03 + operator API decisions |
| 6 | RELAY-05 — Discovery federation decision gate | RELAY-01..04 + D.2 approval |
| 7 | RELAY-06 — E2E/private/load acceptance | RELAY-01..05 + I.3/D.2/E.4 approval |

### 3.3 Cross-Track Rule

RMM and RELAY are independent after F0 and may run concurrently. A shared
dependency (e.g., a data-model change touching `pkg/models`) must be raised here as
its own deferred item **before** either track proceeds with it. Do not let two
tracks edit the same seam silently.

### 3.4 Operational Item Ordering

The spec-review bundle publication (section 5.3) is independent of the RMM/RELAY
tracks and may proceed as soon as the user supplies a remote URL.

---

## 4. Status Vocabulary

Every deferred item carries exactly one status. Do not invent new statuses. Some
transitions are strictly forward; others depend on external input.

| Status | Meaning | Exit condition |
|--------|---------|----------------|
| `OPEN` | Not started; tracked, no doc authored yet | Doc created |
| `IN_REVIEW` | Handoff doc authored; not yet validated | Validation run and passed |
| `SEAM_VERIFIED` | Contract validated against v1.2.0 baseline | All validation checks pass (section 7) |
| `APPROVED` | Work authorized | Closeout in master |
| `COMPLETE` | Authorized work implemented and closed | Closeout with evidence |
| `BLOCKED` | A dependency or validation check failed | Stop; resolve dependency |
| `BLOCKED_DECISION` | External input required (e.g., a URL, a decision) | External input supplied |
| `REJECTED` | Work fails review | Returned to author with reasons |

**Severity (unchanged from repo convention):** `P0` critical, `P1` high,
`P2` medium, `P3` low, `P4` polish. **Effort (unchanged):** `S` 1-3 days,
`M` ~1 week, `L` 2+ weeks.

Silently skipping a transition is a protocol violation.

---

## 5. Authored Handoff Documents

### 5.1 RMM-00..08

| ID | Document | Focus | Status |
|----|----------|-------|--------|
| RMM-00 | `RMM-00_RMM_OPERATIONS_FOUNDATION.md` | Operations foundation and open-decision register | **COMPLETE** (verified 2026-08-24) |
| RMM-01 | `RMM-01_OFFLINE_AGENT_SLA.md` | Offline-agent SLA alerting | **COMPLETE** (shipped commit `6ff3a17`) |
| RMM-02 | `RMM-02_ALERT_MAINTENANCE_WINDOWS.md` | Fleet alert-suppression windows | **COMPLETE** (shipped `4cf3602`) |
| RMM-03 | `RMM-03_WINUPDATE_STATE_MACHINE.md` | Windows Update per-KB lifecycle | **COMPLETE** (implemented and verified in-tree) |
| RMM-04 | `RMM-04_REBOOT_COORDINATION.md` | Reboot coordination | **COMPLETE** (implemented and verified in-tree) |
| RMM-05 | `RMM-05_CVE_CORRELATION.md` | CVE-to-patch correlation | PENDING |
| RMM-06 | `RMM-06_SCHEDULED_AUTOMATION_DECISION.md` | Scheduled automation decision gate | PENDING (blocked: §10.2 grammar) |
| RMM-07 | `RMM-07_AGENT_SELF_UPDATE_DECISION.md` | Agent self-update decision gate | PENDING (blocked: §10.5 trust) |
| RMM-08 | `RMM-08_VNC_RDP_DECISION.md` | VNC/RDP decision gate | PENDING (blocked: §10.8 data plane) |

### 5.2 RELAY-00..06

| ID | Document | Focus | Status |
|----|----------|-------|--------|
| RELAY-00 | `RELAY-00_ARCHITECTURE_SECURITY.md` | Architecture/security outcomes and blockers | PENDING |
| RELAY-01 | `RELAY-01_BINARY_CONFIG_DEPLOYMENT.md` | Fail-closed WSS binary/config/deployment foundation | PENDING |
| RELAY-02 | `RELAY-02_ISSUED_IDENTITY_ENTITLEMENT.md` | Identity/credential/entitlement decision gate | PENDING (blocked: I.3) |
| RELAY-03 | `RELAY-03_WSS_MATCHING_FORWARDING.md` | Rendezvous decisions + contingent forwarding | PENDING (blocked: D.2) |
| RELAY-04 | `RELAY-04_METERING_OBSERVABILITY.md` | Secure operator metering/observability contract | PENDING |
| RELAY-05 | `RELAY-05_DISCOVERY.md` | Discovery federation decision gate | PENDING (blocked: D.2) |
| RELAY-06 | `RELAY-06_E2E_PRIVATE_LOAD.md` | Gated E2E/private/load acceptance | PENDING (blocked: I.3/D.2/E.4) |

### 5.3 Operational Deferred Item — Spec-Review Bundle Publication

Status: **BLOCKED_DECISION** (awaiting user-supplied `<REMOTE_URL>`).

The existing standalone spec-review repo
(`/mnt/data/git/spec-review-2026-08-24`, five commits, `main`, no remote) is to be
published to a user-designated remote. This item is deferred and held in
`BLOCKED_DECISION` until the remote URL is supplied; it is not an RMM/RELAY work
item. Runbook: [SPEC_REVIEW_BUNDLE_HANDOFF.md](../reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md).

Exit to `IN_REVIEW`: user supplies `<REMOTE_URL>` → the runbook proceeds. No
changes to the deferred-work plan are implied by its progress.

---

## 6. Lower-Capability-Agent Checklist

A lower-capability agent (one that cannot complete a full deferred work item) uses
this checklist to hand work off safely. Run in order; **stop at the first failure**
(section 8).

1. **Confirm scope.** Confirm the item you are touching is a tracked deferred item
   and you have the correct repository context.
2. **Never change the baseline.** Do not modify v1.2.0 (`4194dac`) content or the
   W1-W8 delivered code.
3. **One concern per handoff.** Write one handoff note per deferred item; do not
   bundle unrelated RMM and RELAY items.
4. **Record status.** Add only the next legal status from section 4 for your item.
5. **Cite the contract.** Every handoff names the exact contract it touches
   (`file:line` in the baseline). Never hand off "general" work.
6. **Keep it small.** Do not rewrite maps, status, project plan, or openspec specs
   (section 9).
7. **Leave the tree clean.** Stage nothing you cannot explain; never leave a dirty
   working tree.
8. **Ask for a qualified reviewer.** A lower-capability agent never marks
   `APPROVED` or `COMPLETE`.

---

## 7. Exact Validation Rules

Validation is deterministic. An item is `SEAM_VERIFIED` if and only if **all** of
the following pass:

1. **Tree clean.** `git status --porcelain` in the working tree returns empty.
2. **Baseline intact.** No change to v1.2.0 baseline content is introduced by the
   handoff.
3. **Link integrity.** Every relative link in newly added docs resolves to an
   existing file.
4. **Length gate.** Every authored document is under 500 lines (`wc -l`).
5. **Contract match.** The cited contract exists at the v1.2.0 baseline and is
   unchanged by the handoff.
6. **Status legal.** The item's status is a legal, forward transition from its prior
   status per section 4.
7. **Scope.** No map, status, project-plan, or openspec file was edited (whitelist
   in section 9).

For the operational spec-review item, additionally validate the facts in the runbook
section 3 (five commits, `main`, clean tree, no remote) before any push.

---

## 8. Exact Stop Rules

A recognized stop condition halts work immediately. When stopped, do not push, do
not amend, do not "fix forward" silently. Report the stop and leave the tree
untouched.

1. **Dirty tree** -> stop.
2. **Baseline changed** by the handoff -> stop; revert that change.
3. **Forbidden-file edit** (map / status / project plan / spec) -> stop; roll back
   that edit before anything else.
4. **Contract mismatch**: the item's claim about a contract disagrees with the
   v1.2.0 baseline at that `file:line` -> stop; the contract is not true.
5. **Broken link or doc over 500 lines** -> stop; fix only the identified doc, then
   resume.
6. **Illegal status jump** (e.g., `OPEN -> APPROVED`, or a lower-capability agent
   marking `COMPLETE`) -> stop; escalate.
7. **Missing external input** (e.g., a deferred item in `BLOCKED_DECISION` without
   its required input) -> stop; do not guess the input.
8. **Ambiguity**: cannot prove a fact against the baseline -> stop and ask, rather
   than guess.

For the operational runbook, the dedicated halt conditions in the runbook (diverged
or unrelated history; secret scan findings) also apply.

---

## 9. Out of Scope for This Handoff (Do Not Edit)

The following are **not** touched by deferred-work handoffs or this master:

- Navigation maps (`docs/INDEX_MAP.md`, `docs/HEADER_MAP.md`, `TOC.md`).
- `STATUS.md`, `PROJECT_PLAN.md`, and repo status/state files.
- openspec specs (any `openspec/specs/*/spec.md`).
- Production source under `cmd/`, `internal/`, `pkg/`, `py/`, `a2a/`, `web/`.

Map/status/spec refresh is a separate, post-review activity driven from joint
closeout (F9).

---

## 10. Joint Closeout (F9)

When every approved implementation item is complete, all decision-gated items are
resolved, RELAY-06 acceptance passes without remaining I.3/D.2/E.4 blockers, and the
spec-review publication item has resolved, the master runs joint closeout: aggregate
every resolved handoff, re-run the section 7 validations over the whole set, and
reconcile the anticipated document tables (sections 5.1/5.2) against what actually
landed. Only then authorize map/status/spec refresh as a separate follow-up.
Nothing is released to `main` until this closeout passes.

---

**End of master handoff.** Operational runbook:
[SPEC_REVIEW_BUNDLE_HANDOFF.md](../reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md).
