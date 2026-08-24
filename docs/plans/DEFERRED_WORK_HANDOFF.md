# Deferred Work Handoff — Master (Review-Repository Documentation)

**Version:** 1.0.0
**Status:** SEAM_VERIFIED — baseline pinned, dependency order fixed, vocabulary final
**Date:** 2026-08-23
**Baseline:** v1.2.0 (`git rev-parse --short HEAD` = `4194dac`)
**Role:** Master / coordinator for the review-repository handoff of deferred RMM and
RELAY work. This is the single authoritative entry point for reviewers and
lower-capability agents working inside the isolated review repository.
**Related (operational):**
- [SPEC_REVIEW_BUNDLE_HANDOFF.md](../reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md) — the
  companion review handoff (repo layout, audit role, seam-contract truth,
  placeholder remote, no push/amend rules).
- [SPRINT_WIRING_REMEDIATION_PLAN.md](../SPRINT_WIRING_REMEDIATION_PLAN.md) — W1-W8
  wiring remediation, the prior completed wave this document builds upon.
- [GAP_ANALYSIS_RMM_PLATFORM.md](../GAP_ANALYSIS_RMM_PLATFORM.md) — RMM parity gaps
  this document schedules as deferred work.

---

## 1. Purpose and Inventory

v1.2.0 completed the W1-W8 wiring remediation and the OpenSpec P0-P2 audit pass.
Two categories of work remain and are intentionally **deferred** rather than
folded into v1.2.0:

1. **RMM parity gaps** — Windows patch automation, maintenance windows,
   agent auto-update, offline-SLA alerting, scheduled report digests, and the
   remaining RMM capacity checks identified against the shipped Go implementation.
2. **RELAY subsystem** — the relay service is shipped as a parked library pending a
   binary wiring decision; idle-reap is only partially refined; tenant isolation,
   observability, and Python-contract reconciliation remain open.

These deferred items are handed off to a dedicated, isolated **review repository**
where each item is authored as a spec/contract document, reviewed against the
**v1.2.0 baseline**, and returned as a verified seam contract. No deferred item
changes production code until it is SEAM_VERIFIED and APPROVED in that repository.

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
| Seam anchor | The five-commit review repo snapshot (see section 6) |

### 2.2 What Is NOT Deferred

Items already shipped in v1.2.0 are **out of scope** for this handoff and must not
be re-opened:

- Heartbeat persistence (W1), single check-result owner (W2).
- Notifier registry wiring (W3), reporting store/scheduler (W4), remote shell (W5).
- Tenancy wiring (W6), A2A adapter proxy contract (W7), W8 correctness items.
- OpenSpec P0-P2 audit completion (all committed on `main`).

See [SPEC_REVIEW_BUNDLE_HANDOFF.md](../reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md) for
the exact five-commit baseline snapshot that encodes this boundary.

---

## 3. Dependency Ordering

Work orders strictly. An item may not start until every upstream dependency is
`SEAM_VERIFIED`. Two parallel tracks (RMM, RELAY) share the same foundation.

```
F0. Baseline & Seam Wire Contract (done in v1.2.0 / pinned in review repo)
      │
      ├──► RMM TRACK          RELAY TRACK ◄──┤
      │       RMM-00          RELAY-00        │
      │       RMM-01          RELAY-01        │
      │       RMM-02          RELAY-02        │
      │       RMM-03          RELAY-03        │
      │       RMM-04          RELAY-04        │
      │       RMM-05          RELAY-05        │
      │       ...             RELAY-06        │
      └──────────────► F9. Joint closeout ◄───┘
```

### 3.1 RMM Track Order

| Order | Item | Depends on |
|------:|------|------------|
| 1 | RMM-00 — RMM baseline & seam wire contract | F0 (established) |
| 2 | RMM-01 — Windows patch automation (WinUpdate) | RMM-00 |
| 3 | RMM-02 — Maintenance / silence windows | RMM-00 |
| 4 | RMM-03 — Agent auto-update channel | RMM-02 (windowed maintenance) |
| 5 | RMM-04 — Offline-agent SLA / alerting | RMM-03 (agent connectivity surface) |
| 6 | RMM-05 — Scheduled report / digest cadence | RMM-00 |
| 7 | RMM-06+ — Remaining RMM capacity checks | RMM-00 |
| 8 | RMM-08 — RMM seam-contract verification & closeout | RMM-01..RMM-07 |

### 3.2 RELAY Track Order

| Order | Item | Depends on |
|------:|------|------------|
| 1 | RELAY-00 — Relay baseline & seam wire contract | F0 (established) |
| 2 | RELAY-01 — Wire RelayService into a binary (parked-library decision) | RELAY-00 |
| 3 | RELAY-02 — Idle-reap finalized to last-activity semantics | RELAY-01 |
| 4 | RELAY-03 — Tenant isolation across relay sessions | RELAY-01 |
| 5 | RELAY-04 — Relay metrics / observability | RELAY-01 |
| 6 | RELAY-05 — Relay contract reconciliation (Go lib vs Python) | RELAY-01 |
| 7 | RELAY-06 — Relay seam-contract verification & closeout | RELAY-01..RELAY-05 |

### 3.3 Cross-Track Rule

RMM and RELAY are independent after F0 and may run concurrently. A shared
dependency (e.g., a data-model change touching `pkg/models`) must be raised to the
master as its own seam contract **before** either track proceeds with it. Do not
let two tracks edit the same seam silently.

---

## 4. Status Vocabulary

Every deferred item and its handoff document carries exactly one status. Do not
invent new statuses. Transitions are strictly forward.

| Status | Meaning | Exit condition |
|--------|---------|----------------|
| `OPEN` | Not started; tracked, no doc authored yet | Doc created and pushed to review repo |
| `IN_REVIEW` | Handoff doc authored; awaiting review | Reviewer (audit role) starts |
| `SEAM_VERIFIED` | Seam contract validated against v1.2.0 baseline | All seam checks pass (see section 7) |
| `APPROVED` | Review passed; contract accepted | Closeout in master |
| `COMPLETE` | Authorized work implemented and closed | Closeout with evidence |
| `BLOCKED` | A dependency or seam check failed | Stop; do not proceed (see section 8) |
| `REJECTED` | Handoff doc fails review | Returned to author with reasons |

**Severity (unchanged from repo convention):** `P0` critical, `P1` high,
`P2` medium, `P3` low, `P4` polish. **Effort (unchanged):** `S` 1-3 days,
`M` ~1 week, `L` 2+ weeks.

Statuses appear in each anticipated RMM/RELAY document's header block and are
aggregated in the review-repository state. Silently skipping a transition is a
protocol violation.

---

## 5. Anticipated Handoff Documents

The RMM-00..08 and RELAY-00..06 documents are authored by the sibling handoff
agents and land in the review repository under `docs/reviews/deferred/`. They are
**anticipated here** (master is written ahead of their landing); each must carry a
header status per section 4, `RMM-00` / `RELAY-00` being the track baseline that
anchors all others.

### 5.1 RMM-00..08

| ID | Anticipated document | Focus |
|----|----------------------|-------|
| RMM-00 | `RMM-00_baseline_seam.md` | RMM deferred-work baseline + seam wire contract |
| RMM-01 | `RMM-01_winupdate.md` | Windows patch automation (WinUpdate / AutomatedTask) |
| RMM-02 | `RMM-02_maintenance_windows.md` | Maintenance / silence windows |
| RMM-03 | `RMM-03_agent_autoupdate.md` | Agent auto-update channel (G-RMM-003) |
| RMM-04 | `RMM-04_offline_sla.md` | Offline-agent SLA / alerting (G-RMM-004) |
| RMM-05 | `RMM-05_report_digest.md` | Scheduled report / email digest (G-RMM-001) |
| RMM-06 | `RMM-06_capacity_checks.md` | Remaining RMM capacity / parity checks |
| RMM-07 | `RMM-07_policy_parity.md` | Policy-collector / model parity (if surfaced) |
| RMM-08 | `RMM-08_closeout.md` | RMM seam-contract verification & closeout |

### 5.2 RELAY-00..06

| ID | Anticipated document | Focus |
|----|----------------------|-------|
| RELAY-00 | `RELAY-00_baseline_seam.md` | Relay baseline + seam wire contract |
| RELAY-01 | `RELAY-01_wire_binary.md` | RelayService wire-into-binary decision (parked today) |
| RELAY-02 | `RELAY-02_idle_reap.md` | Idle-reap by last activity (finalize) |
| RELAY-03 | `RELAY-03_tenant_isolation.md` | Tenant isolation across relay sessions |
| RELAY-04 | `RELAY-04_observability.md` | Relay metrics / tracing |
| RELAY-05 | `RELAY-05_python_parity.md` | Go-lib vs Python contract reconciliation |
| RELAY-06 | `RELAY-06_closeout.md` | Relay seam-contract verification & closeout |

These two sections are **planning references only**. Where the authored document
differs from this anticipation, the authored document is authoritative; this master
is updated at joint closeout (F9), not piecemeal.

---

## 6. Lower-Capability-Agent Checklist

A lower-capability agent (one that cannot complete a full review) uses this
checklist to hand work off safely without corrupting the review repo or the
baseline. Run in order; **stop at the first failure** (section 8).

1. **Confirm location.** Verify you are inside the isolated review repository
   (five-commit snapshot, placeholder origin). If you are in the real `origin`
   tree, stop immediately and do nothing.
2. **Never touch the baseline.** Do not modify any committed spec, the seam wire
   contract, or `E000`-style baseline markers.
3. **One concern per handoff.** Write one handoff note per deferred item; do not
   bundle unrelated RMM and RELAY items.
4. **Record status.** Add only the next valid status from section 4 for your item.
5. **Cite the seam.** Every handoff must name the exact seam contract it touches
   (file:line in the baseline, or the contract ID). Never hand off "general"
   work.
6. **Keep it small.** Handoff notes stay under the navigation-map refresh scope;
   do not rewrite maps, status, project plan, or specs (section 9).
7. **Leave the tree clean.** Stage nothing you cannot explain; never leave a dirty
   worktree, never exceed five commits in the review repo.
8. **Ask for a human-seat reviewer.** A lower-capability agent never marks
   `APPROVED` or `COMPLETE`; those transitions require the audit role (see the
   review handoff).

---

## 7. Exact Validation Rules

Validation is deterministic. An item is `SEAM_VERIFIED` if and only if **all** of
the following pass:

1. **Tree clean.** `git status --porcelain` in the review repo returns empty.
2. **Commit count pinned.** `git rev-list --count HEAD` in the review repo equals
   exactly `5`. If the count is not 5, stop (section 8).
3. **No remote contact.** `git remote -v` shows the placeholder remote only. `git
   push` and `git fetch` to the placeholder must be inert/denied. No real-`origin`
   URL anywhere in the review repo's `config` or `remotes`.
4. **Link integrity.** Every relative link in newly added docs resolves to an
   existing file. Broken links fail the review.
5. **Length gate.** Every authored document is under 500 lines (`wc -l`).
6. **Baseline match.** The seam contract cited exists at the v1.2.0 baseline
   (`4194dac`) and is unchanged by the handoff.
7. **Status legal.** The item's status is a legal, forward transition from its
   prior status per section 4.
8. **Vocabulary / scope.** No map, status, project-plan, or openspec file was
   edited (whitelist enforced in section 9).

To validate locally from this worktree (non-destructive, read-only):

```bash
git status --porcelain                     # must be empty
git rev-list --count HEAD                  # must be 5
git remote -v                              # placeholder only
wc -l docs/reviews/deferred/*.md           # each < 500
```

---

## 8. Exact Stop Rules

A recognized stop condition halts work immediately. When stopped, do not push, do
not amend, do not "fix forward" silently. Report the stop to the master/audit role
and leave the review repo untouched.

1. **Dirty tree** in the review repo -> stop.
2. **Commit count != 5** in the review repo -> stop; the snapshot is no longer the
   agreed baseline.
3. **Real-`origin` URL present** or any sign of an intended push/amend -> stop; the
   placeholder-remote contract is broken.
4. **Seam mismatch**: the item's claim about a seam disagrees with the v1.2.0
   baseline at that file:line -> stop; the contract is not true.
5. **Broken link or doc over 500 lines** -> stop; fix only the identified doc, then
   resume.
6. **Forbidden-file edit** (map / status / project plan / spec) -> stop; roll back
   that edit before anything else.
7. **Illegal status jump** (e.g., `OPEN -> APPROVED`, or lower-capability agent
   marking `COMPLETE`) -> stop; escalate to the audit role.
8. **Ambiguity**: cannot prove a fact against the baseline -> stop and ask, rather
   than guess.

Stopping is correct behavior, not failure. Every stop is logged with the rule id
(S1..S8) in the review-repository state.

---

## 9. Out of Scope for This Handoff (Do Not Edit)

Per the approved scope, the following are **not** touched by deferred-work handoffs
or this master:

- Navigation maps (`docs/INDEX_MAP.md`, `docs/HEADER_MAP.md`, `TOC.md`).
- `STATUS.md`, `PROJECT_PLAN.md`, and repo status/state files.
- openspec specs (any `openspec/specs/*/spec.md`).
- Production source under `cmd/`, `internal/`, `pkg/`, `py/`, `a2a/`, `web/`.

Map/status/spec refresh is a separate, post-review activity driven from joint
closeout (F9), not from individual deferred-item handoffs.

---

## 10. Joint Closeout (F9)

When both tracks reach `RELAY-06 COMPLETE` and `RMM-08 COMPLETE`, the master runs
joint closeout: aggregate every `SEAM_VERIFIED`/`APPROVED`/`COMPLETE` handoff,
re-run the section 7 validations once more over the whole bundle, reconcile the
anticipated document table (section 5) against what actually landed, and only then
authorize map/status/spec refresh as a separate follow-up. Nothing is released to
`main` until this closeout passes.

---

**End of master handoff.** Companion procedure: see
[SPEC_REVIEW_BUNDLE_HANDOFF.md](../reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md).
