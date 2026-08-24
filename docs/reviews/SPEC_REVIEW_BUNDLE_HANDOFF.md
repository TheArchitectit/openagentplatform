# Spec Review Bundle Handoff — Review Repository

**Version:** 1.0.0
**Status:** IN_REVIEW
**Date:** 2026-08-23
**Baseline:** v1.2.0 (`4194dac`)
**Role:** Review handoff for the isolated spec-review repository. Companion to the
[master deferred-work handoff](../plans/DEFERRED_WORK_HANDOFF.md).

---

## 1. Purpose

This document specifies how the deferred spec work (RMM-00..08, RELAY-00..06) is
reviewed inside an **isolated review repository** so that no review activity ever
touches the real `origin`
(`git@github.com:TheArchitectit/openagentplatform.git`). It defines the
repository snapshot, the audit role that owns review, the seam-contract standard of
truth, the placeholder-remote procedure, and the absolute no-push/no-amend rules.

---

## 2. Local Clean Five-Commit Repository

Review happens in a local, isolated mirror of the worktree — never in a branch that
shares history with the real `origin`.

### 2.1 Snapshot

| Property | Value |
|----------|-------|
| Role | read-only review ground for deferred-work contracts |
| Commit count | exactly **5** (`git rev-list --count HEAD`) |
| Working tree | clean (`git status --porcelain` empty) |
| First commit | v1.2.0 baseline snapshot (`4194dac` tree) |
| Remaining four commits | the four handoff/contract marker commits that bound the bundle |
| Branches | local review branch only; no tracking of `origin/main` |

The five-commit shape is the agreed **snapshot contract**: any commit count other
than five means the snapshot has drifted and the review must stop (master stop rule
S2). The repository is re-created (not amended or force-pushed) if a reset is ever
needed.

### 2.2 Re-Creation, Not Mutation

If the snapshot is ever out of shape, delete and re-create it from the baseline.
Never `reset`, `push -f`, `amend`, or `rebase`. Re-creating the local review repo is
cheap and preserves the snapshot contract; mutating history violates it.

---

## 3. Audit Role

A single **audit role** owns all review in this repository. Only the audit role may
move an item to `APPROVED` or `COMPLETE`. Other agents (authors, lower-capability
agents) may author handoffs and set `OPEN`/`IN_REVIEW`, but never approve.

| Responsibility | Held by |
|----------------|---------|
| Author handoff documents (RMM-00..08, RELAY-00..06) | Author agents |
| Read-only verification against baseline | Audit role |
| `SEAM_VERIFIED` / `APPROVED` / `COMPLETE` transitions | Audit role only |
| Joint closeout authorization (F9) | Audit role (+ master) |
| Escalation on any stop rule | Audit role |

The audit role:

1. Reads the bundle against the v1.2.0 baseline, not against author prose.
2. Re-runs every validation rule in the master handoff (section 7) on each item.
3. Files one finding per violated rule with the exact file:line that contradicts
   the baseline (the "seam claim").
4. Never edits author documents; it returns them to the author with findings.
5. Never touches the real `origin`; it works entirely in the local review repo.

---

## 4. Seam-Contract Truth

### 4.1 Definition

A **seam** is a stable boundary where two components, two handoff stages, or a
component and the release baseline connect (for example, a function signature, an
NATS subject, an HTTP route contract, or an openspec interface). The **seam
contract** is the authoritative, baseline-cited description of that boundary.

**Seam-contract truth** is the rule that a deferred-work claim is true if and only
if it matches the seam contract **as it exists in the v1.2.0 baseline** — not as
described in prose, memory, or an author's interpretation. The emitter of truth is
the baseline itself (`file:line`).

### 4.2 How It Is Applied

- Every handoff must name the exact seam it touches and cite it as
  `<path>:<line>` in the `4194dac` tree.
- The audit role verifies the cited seam exists in the baseline and that the
  claim's behavior matches it exactly.
- A claim that disagrees with the cited seam is false by definition
  (stop rule S4). It is never "close enough."
- The seam contract may be changed only by an explicit, separately reviewed
  contract change — never silently by a handoff.

---

## 5. Safe Placeholder Remote Procedure

The review repository's `origin` points at a **placeholder** remote — a local
bare/shared path (not GitHub) selected so that every `git push` / `git fetch`
against it is inert or explicitly denied. This guarantees accidental pushes cannot
reach the real repository.

Procedure to set up or re-create the review repository safely:

```bash
REVIEW_DIR="$REVIEW_REPO_ROOT"
CONTROL_BARE="$REVIEW_CONTROL_BARE"   # a loopback/local bare path, NOT GitHub

git init "$REVIEW_DIR" && cd "$REVIEW_DIR"
git remote add origin "$CONTROL_BARE"          # placeholder / loopback only
git config receive.denyCurrentBranch ignore    # optional; keep pushes local & inert
```

After setup, verify:

```bash
git remote -v        # must print the placeholder path, never git@github.com:...
git config --get remote.origin.url             # placeholder only
git rev-list --count HEAD                      # must be 5
git status --porcelain                         # must be empty
```

Constraints:

- The placeholder remote URL must never be a GitHub or any externally routable
  endpoint.
- `git push` against the placeholder must be inert (loopback bare, or refused); it
  is for **reference only**, not for transmission.
- No credential, no `git@github.com:` string, and no SSH alias for the real remote
  may exist anywhere in this repo's `config`, `remotes/`, or hooks.
- If anything external is detected, treat it as a broken placeholder contract
  (stop rule S3) and halt.

---

## 6. No Remote / Push / Amend

These are absolute prohibitions for **everyone** operating in the review
repository:

1. **No push to any real origin.** The real repository
   (`git@github.com:TheArchitectit/openagentplatform.git`) is never a target.
2. **No push at all is required.** Delivery is by local snapshot; transmission is
   out of scope for review. Pushing is not how work leaves this repo.
3. **No `git push --force`, no `git reset --hard`, no `git rebase`, no
   `git commit --amend`.** The five-commit snapshot and its history are immutable
   during a review.
4. **No fetches from the real origin.** Outside information arrives via the
   baseline snapshot only.
5. **No uncommitted drift.** The tree stays clean; every produced document is
   committed locally as part of the five-commit snapshot (or a re-created snapshot).

If any of the above is attempted or observed, stop immediately, log the affected
stop rule (S3 for remote/push, S1/S2 for tree/commit drift), and hand the report to
the audit role. Do not "clean up" by rewriting history to make it look un-done.

---

## 7. Handoff Entrance Criteria

A deferred item may enter this review repository when all are true:

1. Its handoff document exists under `docs/reviews/deferred/` and carries a legal
   status from the master vocabulary (section 4 of the master handoff).
2. It names the exact seam contract, cited as `<path>:<line>` in `4194dac`.
3. It is under 500 lines and its relative links resolve.
4. It touches no forbidden file (maps / status / project plan / openspec specs).
5. It is committed in the five-commit snapshot; the tree is clean.

---

## 8. Handoff Exit (Return to Master)

When the audit role has verified an item, it files the result to the master
(`OPEN -> SEAM_VERIFIED -> APPROVED -> COMPLETE` as applicable). The master
aggregates results and, at joint closeout (F9), reconciles the anticipated document
table and authorizes any separate map/status/spec refresh. Until closeout, this
repository holds the authoritative review state.

---

## 9. Summary of Hard Rules

| # | Rule | Applies to |
|---|------|-----------|
| R1 | Exactly 5 commits, clean tree | Everyone |
| R2 | Placeholder origin only; no real-origin URL | Everyone |
| R3 | No push, no force, no reset --hard, no rebase, no amend | Everyone |
| R4 | Immutable snapshot; re-create, never mutate | Everyone |
| R5 | Truth = v1.2.0 seam contract at cited file:line | Everyone |
| R6 | `APPROVED`/`COMPLETE` only by audit role | Audit role |
| R7 | Stop on any S1..S8 rule; report, don't fix-forward | Everyone |

---

**End of review handoff.** Master procedure:
[../plans/DEFERRED_WORK_HANDOFF.md](../plans/DEFERRED_WORK_HANDOFF.md).
