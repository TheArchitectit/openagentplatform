# Spec Review Bundle Handoff — Separate Spec-Review Repository

**Version:** 1.1.0
**Status:** SEAM_VERIFIED
**Date:** 2026-08-24
**Baseline:** v1.2.0 (`4194dac`)
**Role:** Publication runbook for the dedicated **separate spec-review repository**
to which the deferred spec bundle (RMM-00..08, RELAY-00..06) is published for
review. Companion to the
[master deferred-work handoff](../plans/DEFERRED_WORK_HANDOFF.md).

---

## 1. Purpose

The deferred spec work is reviewed in a **separate spec-review repository** — a
distinct git repository, with its own directory and history, that is **not** the
openagentplatform worktree and **never** shares a remote or push path with
`git@github.com:TheArchitectit/openagentplatform.git`. This document is the
**publication runbook**: the exact, ordered procedure for publishing the reviewed
spec bundle into that separate repository as a clean five-commit snapshot, and the
rules that keep publication from ever touching the live OAP repository.

- **Output of this runbook:** the separate spec-review repository holding the
  published five-commit bundle, ready for audit.
- **Out of scope:** authoring the contracts themselves (done by the sibling handoff
  agents) and code changes to `main` (done only after joint closeout F9).

---

## 2. The Separate Spec-Review Repository

The spec-review repository is a **separate repository**, not a mirror of the OAP
worktree and not a branch of the OAP repo. It is created and tracked on its own:

| Property | Value |
|----------|-------|
| Kind | separate, dedicated git repository (own `.git`, own history) |
| Directory | distinct from the openagentplatform worktree |
| Commit count | exactly **5** (`git rev-list --count HEAD`) |
| Working tree | clean (`git status --porcelain` empty) |
| First commit | v1.2.0 baseline snapshot (`4194dac` tree) |
| Following commits | the reviewed handoff/contract commits that form the bundle |
| Relation to OAP remote | none — never shares `origin` with the OAP repo |

The five-commit shape is the **snapshot contract** for publication: the bundle is
accepted only as exactly five commits on a clean tree. Any other count means the
published bundle has drifted and the runbook stops (master stop rule S2).

### 2.1 Re-Publish, Not Mutate

If a published bundle is ever out of shape, **re-publish** the separate repository
from the baseline (recreate it fresh). Never `reset`, `push -f`, `rebase`, or
`commit --amend` inside the separate review repository. The published snapshot is
immutable once accepted; a fresh publication is cheap and preserves the contract.

---

## 3. Audit Role

A single **audit role** owns review and publication acceptance in the separate
spec-review repository. Only the audit role may accept a published bundle or move an
item to `APPROVED` / `COMPLETE`. Authors and lower-capability agents may stage
handoffs (`OPEN` / `IN_REVIEW`) but never accept.

| Responsibility | Held by |
|----------------|---------|
| Author handoff documents (RMM-00..08, RELAY-00..06) | Author agents |
| Stage into the bundle (`OPEN` / `IN_REVIEW`) | Author / lower-capability agents |
| Verify against the baseline before acceptance | Audit role |
| Accept published bundle; `SEAM_VERIFIED` / `APPROVED` / `COMPLETE` | Audit role only |
| Authorize joint closeout (F9) | Audit role (+ master) |
| Escalate on any stop rule | Audit role |

The audit role:

1. Reads each published contract against the v1.2.0 baseline, not against author
   prose.
2. Re-runs every validation rule in the master handoff (section 7) on the bundle.
3. Files one finding per violated rule, citing the exact `file:line` that
   contradicts the baseline (the "seam claim").
4. Never edits author documents — it returns them with findings for re-publication.
5. Never publishes to, pushes to, or reads from the live OAP repository.

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

- Every contract in the published bundle names the exact seam it touches and cites
  it as `<path>:<line>` in the `4194dac` tree.
- The audit role verifies the cited seam exists in the baseline and that the
  claim's behavior matches it exactly.
- A claim that disagrees with the cited seam is false by definition (stop rule S4).
  It is never "close enough."
- The seam contract may change only through an explicit, separately reviewed
  contract change — never silently via a publication.

---

## 5. Safe Placeholder-Remote Publication Procedure

Publication targets the **separate** spec-review repository, which has its own
`origin`. To ensure no accidental transmission to the live OAP upstream, the
separate repository is first prepared against a **placeholder origin** for a safe
dry-run, then published locally:

**Step 1 — Create the separate repository with a placeholder origin (safe rehearsal):**

```bash
REVIEW_DIR="$SPEC_REVIEW_DIR"                 # a SEPARATE directory, NOT the OAP tree
CONTROL_BARE="$REVIEW_CONTROL_BARE"           # loopback / local bare path only

git init "$REVIEW_DIR" && cd "$REVIEW_DIR"
git remote add origin "$CONTROL_BARE"         # placeholder / local, never the OAP remote
git config receive.denyCurrentBranch ignore   # keep any rehearsal push local & inert
```

**Step 2 — Rehearse the five-commit bundle** locally (branch, commits, clean tree)
against the placeholder origin. Any push here is inert by construction.

**Step 3 — Verify the placeholder is correct before any real publication:**

```bash
git remote -v        # prints the placeholder/local path, never git@github.com:...
git config --get remote.origin.url            # placeholder only
git rev-list --count HEAD                     # must be 5
git status --porcelain                        # must be empty
```

**Step 4 — Publish the accepted bundle** as the separate repository's five-commit
snapshot. Because review lives entirely in the separate repository, no OAP remote is
ever touched; the placeholder guarantees even a scripting mistake cannot reach the
live upstream.

Constraints:

- The placeholder origin must never be GitHub or any externally routable endpoint.
- No credential, no `git@github.com:` string, and no SSH alias for the OAP remote
  may exist anywhere in the separate repository's `config`, `remotes/`, or hooks.
- If an OAP remote endpoint is ever detected, the placeholder contract is broken
  (stop rule S3) and publication halts.

---

## 6. No Push / Amend Rules

These are absolute prohibitions for **everyone** operating the runbook and the
separate repository:

1. **No push to the live OAP repository**
   (`git@github.com:TheArchitectit/openagentplatform.git`) — ever.
2. **No push is required to deliver.** Delivery is by publication of the local
   five-commit snapshot into the separate repository; transmission to OAP is out of
   scope for review.
3. **No `git push --force`, no `git reset --hard`, no `git rebase`, no
   `git commit --amend`** — in the separate repository or against the OAP history.
   The five-commit snapshot is immutable once published.
4. **No fetch from the live OAP remote.** The baseline is the only source of truth
   and arrives via the snapshot, not a live fetch.
5. **No uncommitted drift.** The separate repository's tree stays clean; every
   published document is part of the five-commit bundle.

If any of the above is attempted or observed, stop immediately, log the stop rule
(S3 for remote/push; S1/S2 for tree/commit drift), and hand the report to the audit
role. Do not rewrite history to make it look un-done.

---

## 7. Entrance Criteria (before a bundle is published)

A deferred item may be published into the separate spec-review repository when all
are true:

1. Its handoff document exists under `docs/reviews/deferred/` and carries a legal
   status from the master vocabulary (master handoff section 4).
2. It names the exact seam contract, cited as `<path>:<line>` in `4194dac`.
3. It is under 500 lines and its relative links resolve.
4. It touches no forbidden file (maps / status / project plan / openspec specs).
5. The separate repository holds exactly five commits on a clean tree.

---

## 8. Exit (Return to Master)

When the audit role accepts the published bundle, it files results to the master
(`OPEN -> SEAM_VERIFIED -> APPROVED -> COMPLETE` as applicable). The master
aggregates results and, at joint closeout (F9), reconciles the anticipated document
table and authorizes any separate map/status/spec refresh. Until closeout, the
separate spec-review repository holds the authoritative review state.

---

## 9. Summary of Hard Rules

| # | Rule | Applies to |
|---|------|-----------|
| R1 | Separate repo; exactly 5 commits; clean tree | Everyone |
| R2 | Placeholder origin only; no OAP remote URL | Everyone |
| R3 | No push, no force, no reset --hard, no rebase, no amend | Everyone |
| R4 | Immutable published snapshot; re-publish, never mutate | Everyone |
| R5 | Truth = v1.2.0 seam contract at cited file:line | Everyone |
| R6 | `APPROVED`/`COMPLETE` and bundle acceptance only by audit role | Audit role |
| R7 | Stop on any S1..S8 rule; report, don't fix-forward | Everyone |

---

**End of review publication runbook.** Master procedure:
[../plans/DEFERRED_WORK_HANDOFF.md](../plans/DEFERRED_WORK_HANDOFF.md).
