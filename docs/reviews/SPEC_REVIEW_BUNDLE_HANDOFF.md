# Spec Review Bundle Handoff — Publication Runbook

**Version:** 2.0.0
**Status:** BLOCKED_DECISION — awaiting user-supplied `REMOTE_URL`
**Date:** 2026-08-24
**Baseline:** v1.2.0 (`4194dac`)
**Role:** Operational runbook for publishing the **existing** spec-review repository
to a user-designated remote. Companion to the
[master deferred-work handoff](../plans/DEFERRED_WORK_HANDOFF.md), which lists this
as deferred `BLOCKED_DECISION`.

---

## 1. Purpose and Role

The reviewed spec bundle already exists in a **standalone repository**. This runbook
publishes that repository as-is to a user-supplied remote. It does **not** create,
rehearse, re-publish, or rewrite any repository or history.

Operator role: run the steps in order, verify each precondition, and stop on the
first failure. The **goal is an eventual, normal (non-force) push** of the existing
`main` to the designated remote.

### Relevant files (the bundle being published)

- `OVERVIEW.md` — bundle overview and the three findings that matter most.
- `openagentplatform/specs/` — 35 OAP capability specs (ChatGPT rewrite of the
  live `openspec/specs/`).
- `openagentplatform/proposed-additions/managed-backup/` — OAP integration contract
  for the backup product (batch 1).
- `managed-backup/specs/` — 29 greenfield backup-product specs (batch 2).
- Per-folder `RECOMMENDATIONS.md` files alongside most specs.

65 distinct spec documents total across 132 tracked files.

---

## 2. The Existing Repository (what you are publishing)

| Property | Value |
|----------|-------|
| Path | `/mnt/data/git/spec-review-2026-08-24` |
| Kind | standalone git repo, own `.git` |
| Branch | `main` |
| Working tree | clean |
| Remote | none (no `origin` yet) |
| Commit count | 5 |

Existing five commits (in history order). **Do not amend, rewrite, rebase, or
re-author these commits or their historical authors:**

| SHA (short) | Subject |
|-------------|---------|
| `ec30bb4` | Spec review bundle: 65 ChatGPT-generated specs (OAP + Managed Backup) with per-folder RECOMMENDATIONS.md + OVERVIEW.md |
| `28df1a3` | docs(backup): freeze cross-service seam contracts |
| `2f87655` | docs(review): correct OAP rewrite fidelity findings |
| `ddf1c9d` | docs(backup): clarify canonical event catalog |
| `d84375c` | docs(review): correct seam attribution across backup pack |

---

## 3. Verify the Repository Facts

Confirm the preconditions before proceeding. If any check fails, stop and report;
do not continue.

```bash
cd /mnt/data/git/spec-review-2026-08-24

git rev-list --count HEAD        # expect: 5
git log --oneline                # expect: exactly the five commits in section 2
git branch                       # expect: * main
git status --porcelain           # expect: empty (clean tree)
git remote -v                    # expect: no remotes listed
```

Only when all match, continue.

---

## 4. Require the User-Supplied Remote URL

This runbook has **no remote of its own**. You need the destination URL from the
user before any network step. Do not guess or invent one.

1. Request `<REMOTE_URL>` from the user.
2. If none is supplied, **stop here** — the handoff remains
   `BLOCKED_DECISION` (see the master handoff).
3. Confirm the URL is a git remote endpoint you are authorized to push to.

You are provided a URL by the user in the form:

```
<REMOTE_URL>
```

---

## 5. Inspect the Destination

Before wiring or pushing, inspect the destination the user named:

- Fetch its metadata (for a GitHub-style remote, view the repository page / API).
- Confirm it is empty or that the intent to publish youra existing `main` there is
  correct (`git push` to a non-empty destination with unrelated history will be
  **rejected** by git — that is expected and safe).
- Confirm you are authorized to push (credentials / SSH access present).

Record your assessment; if the destination is unexpected or you cannot confirm
authorization, stop before touching git config or network.

---

## 6. Add the Remote

Wire the user-supplied remote as `origin`:

```bash
cd /mnt/data/git/spec-review-2026-08-24
git remote add origin <REMOTE_URL>
```

Verify:

```bash
git remote -v
```

`origin` must now point exactly at the user-supplied `<REMOTE_URL>`.

---

## 7. Fetch the Remote

Fetch the destination into the local repo so you can assess divergence before
pushing:

```bash
git fetch origin
```

Note: `git fetch` does not touch `main` (use `git fetch origin`, do not merge/pull).
If the fetch fails (e.g., destination empty or unreachable), stop and report before
pushing.

---

## 8. Assess Divergence / Unrelated History — HALT if Unsafe

After the fetch, check whether the destination already has history:

```bash
git rev-list --count origin/main 2>/dev/null || echo "no origin/main (empty destination)"
git merge-base HEAD origin/main 2>/dev/null || echo "no common ancestor (unrelated history)"
```

Assess:

- **Empty destination (no `origin/main`):** safe to proceed with a normal push of
  `main`.
- **Destination history is an ancestor of local `main` (fast-forwardable):** safe.
- **Destination and local share history but local is behind / diverged:** halt and
  report — do not force-push; determine the correct action with the user.
- **Destination and local have no common ancestor (unrelated histories):** a plain
  `git push` will be **rejected by git**; that is the safe outcome. **HALT** and
  report rather than `--force` or rewriting history to force it.

The rule: never do anything that would overwrite or rewrite remote or local history
to force a push. If git's normal refusal is the result, that is the desired safe
state.

---

## 9. Secret Scan

Before publishing, run a secret scan or a targeted equivalent over the repository
content:

```bash
cd /mnt/data/git/spec-review-2026-08-24
# use the repo's configured scanner, or a targeted equivalent, e.g.:
#   gitleaks detect          (or)
#   trufflehog git file://.  (or)
# project-specific secret-gate script
```

If the scan finds hard-coded credentials, tokens, or keys, **halt**, do not push,
and report — secrets must be removed by the user before publication. Do not "fix" by
rewriting history; surface it and stop.

---

## 10. Push main (Non-Force)

Publish the existing `main` to the remote, tracking it:

```bash
cd /mnt/data/git/spec-review-2026-08-24
git push -u origin main
```

Constraints:

- **Non-force only.** No `--force`, no `--force-with-lease`.
- Push only the existing `main`; do not push other refs or tags unless the user
  asks.
- If git refuses the push (remote has diverged/unrelated history), that is the
  expected safe outcome — **halt and report** (see section 8). Do not over-ride it.

---

## 11. Verify the Published Remote

Confirm the publication landed (same SHA, correct URL):

```bash
cd /mnt/data/git/spec-review-2026-08-24
git fetch origin
git rev-parse HEAD            # local main SHA
git rev-parse origin/main     # remote main SHA (must match HEAD)
git remote -v                 # origin must equal <REMOTE_URL>
```

Report to the user:

- Local `main` SHA.
- `origin/main` SHA (must be identical — non-force push of an accepted fast-forward
  or empty destination).
- The `origin` URL, as confirmed.

---

## 12. Notes

- No repository is created or re-published by this runbook; it operates on the
  existing `/mnt/data/git/spec-review-2026-08-24` repo.
- Historical commits and their authors are never amended or rewritten.
- Until the user supplies `<REMOTE_URL>`, this handoff stands at
  `BLOCKED_DECISION` in the master deferred-work tracker.

---

**End of runbook.** Master tracker:
[../plans/DEFERRED_WORK_HANDOFF.md](../plans/DEFERRED_WORK_HANDOFF.md).
