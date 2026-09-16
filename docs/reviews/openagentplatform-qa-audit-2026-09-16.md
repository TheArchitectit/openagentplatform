# OpenAgentPlatform — QA Audit & Digest Follow-up Review

**Date:** 2026-09-16 (UTC)
**Auditor:** Forge (Roger's build agent)
**Target:** `TheArchitectit/openagentplatform`, `main` @ `cbeb9d2`
**For:** Collaborator `drwhofan2k18-pixel` (drwhofan2k18-pixel) — action owner

---

## Purpose

This is a **full QA review collated from the ArchitectIT daily digest notes** (2026-08-27 through 2026-09-16) plus a fresh automated/hands-on audit of the current tree. Every finding below was **re-verified against live repo state this session** — nothing is secondhand. Where a digest note has already been resolved in your own work (#62, #65), I mark it resolved and cross-reference so we don't double-count.

---

## Executive Summary

| Signal | State on `main` @ `cbeb9d2` |
|--------|------------------------------|
| Root module build (`go build ./...`) | ✅ passes |
| `mcp-server` module build (`GOWORK=off go build ./...`) | ❌ **broken — #66** (RuleParser/ParsedRule/RuleSyncResult undefined, + unused-import errors in `updates`, `validation`) |
| Race detector (`go test -race`) | ❌ **fails — #63** (`TestQueueSubmit` data race, `internal/security/ingest`) |
| Migration sanity | ❌ **fails — #64** (version gaps 006–012; `.down.sql` missing on 013/014/015/016) |
| CI on `main` | ❌ **red** — `Coverage + Quality Gate` and `Team Validation` both failing post-#65 |
| Open PRs | #62 open (security remediation), #65 merged (CI repair) |
| Open issues | #63, #64, #66 — all three normalize to your #62-test surface |

**Net:** three build/CI-blocking defects are open, all filed by you (collaborator) and all reproduciable on pristine `main`. The digest has been flagging most of them for 2–3 weeks. The single highest-priority item is **#66 (RuleParser)** because it leaves `mcp-server` (the `guardrail-mcp` module) unbuildable and — combined with the migration gap — keeps `go test` red in CI.

---

## Part 1 — The three build/CI-blocking defects (must-address, in priority order)

### 🔴 D1 — Issue #66: RuleParser implementation lost in the file-split refactor

- **Reproduced:** `cd mcp-server && GOWORK=off go build ./...` fails.
- **Errors:** `internal/ingest/rule_sync.go` references `RuleParser`, `ParsedRule`, `RuleSyncResult` — all **undefined**. Transitive breakage in `cmd/server`, `internal/adapters`. Plus unrelated `imported and not used` in `internal/updates/checker_status.go` and `internal/validation/engine_cache.go`/`engine_core.go`.
- **Root cause (your note):** commit `c251772` ("split rule_parser.go 530→2") **dropped the `RuleParser` implementation entirely** rather than splitting it. `rule_parser.go` no longer exists; only `rule_sync.go` remains.
- **Why it matters beyond broken build:** the `mcp-server` module is a **separate Go module** (`github.com/thearchitectit/guardrail-mcp`, not a `go.work` member), so the root CI `go build ./...` never touches it — this class of breakage is invisible to the root gate. See G1.
- **Action:** restore the `RuleParser` + `ParsedRule` + `RuleSyncResult` types/impl (recover from git history at `c251772^`), then remove the unused imports flagged above.

### 🔴 D2 — Issue #64: Migration version gap (006–012) + missing `.down.sql`

- **Reproduced:** `go test -count=1 -run TestEmbeddedMigrationsSanity ./internal/db/` fails.
- **Failures:** version gaps for migrations **006, 007, 008, 009, 010, 011, 012**; plus `.down.sql` missing on **013_cloud_control, 014_eve_monitoring, 015_active_security, 016_power_monitoring** (roll-forward no-ops still require the file).
- **Why it matters:** `status` reporting assumes contiguous numbering; the sanity test is the only guard against a silently non-contiguous embedded set in the single-schema golang-migrate runner.
- **Action:** decide — (a) renumber to contiguous, or (b) relax the sanity assertion *deliberately* with a comment. Either way, add the missing `.down.sql` files. Don't let the test go away as a "fix": the gap is real drift, not a test bug.

### 🔴 D3 — Issue #63: Data race in security ingest queue

- **Reproduced:** `go test -race -count=1 ./internal/security/ingest/` fails deterministically (`TestQueueSubmit`, ~1.0s). Race is in the callback contract — worker goroutine invokes the submit callback concurrently with the test goroutine reading state with no synchronization.
- **Why it matters:** if the race is in the callback contract (handler invoked concurrently with caller observing state), **production consumers can hit it too**, not just the test.
- **Action:** synchronize the considered case; if the contract is "handler may be called concurrently," document it and make the test correctly mutex-gated rather than asserting a stably-ordered `handled=5`.

---

## Part 2 — Digest note-to-repo trace (all 11 notes, current status)

I read the full `ai-news/nextday-actions.md` + the `2026-09-06` weekly digest section + `docs/DIGEST-FOLLOWUPS.md` (your NEXT-* bridge). Cross-walk:

| # | Digest date | Note (paraphrased) | Digest ID | Current status (verified) |
|---|---|---|---|---|
| 1 | 08-27 | Pin frozen relay identity/entitlement contract with conformance test | NEXT-006 / NEXT-039 | ⚠️ **Partial** — impl exists (`internal/relay/trust.go` `CheckEntitlement`, `handleWSS` in `ws.go:143`) + `e2_private_relay_test.go` proves non-entitled rejection. But no explicit "frozen contract" conformance file; see finding R1. |
| 2 | 08-27 | relay-03 negative conformance test (unentitled → deterministically refused + visible) | NEXT-011 / NEXT-042 | ✅ **Resolved** — `TestE2_PrivateRelay_RejectsNonEntitled` exists (`e2_private_relay_test.go:72`) and asserts 0 registrations on default-deny. |
| 3 | 08-28 | Prune local-only agent worktree + per-PR branches | (08-28) | ⚠️ **Partial** — 2 `worktree-agent-*` branches remain local-only (but both merged into main). |
| 4 | 09-02 | Set relay `-store-dsn` durable-state flag in deploy compose + verify restart survival | NEXT-064 | ✅ **Resolved** — `deploy/docker-compose.yml:186` wires `-store-dsn ${RELAY_STORE_DSN:-}`; live-verified on ai04 (§8.7). |
| 5 | 09-03 | Per-provider contract tests + fake-cloud drift fixture | NEXT-069 | ❌ **Open** — only `*ImplementsInterface` + `PeriodRange` tests in cloud providers; **no fake-cloud drift fixture** anywhere in `internal/cloud`. |
| 6 | 09-04 | Reconcile-twice idempotency test for cloud reconciler | NEXT-080 | ❌ **Open** — `internal/cloud/reconciler_test.go` has 6 tests (ResourceFilter, NewReconciler, AutoEnroll*, Drift*) but **no reconcile-twice idempotency test**. |
| 7 | 09-05 | PR-or-archive local-only sprint + worktree-agent branches; add repo-status README | NEXT-085 | ⚠️ **Partial** — `STATUS.md` exists (good, serves as the status README). But 11 branches are still local-only with no upstream. |
| 8 | 09-06 | GC the ~23 worktree-agent-* branches | (09-06) | ✅ **Mostly resolved** — only 2 remain (down from ~23); both already merged. |
| 9 | 09-07 | Unmerged local-only sprint branches (secret-backends/references/rotation/scanning + dashboard/monitoring) — merge vs ticket | NEXT-100 | ⚠️ **Partial** — these are now **merged into main** (verified `--merged main`), but still *locally* present with no upstream. Merge decision effectively made; just needs remote hygiene. |
| 10 | 09-08 | Prune merged worktree-agent-* branches (~27) | NEXT-108 | ✅ **Resolved in effect** — only 2 remain, both merged. |
| 11 | 09-16 | Restore RuleParser (#66); wire build/migration-sanity/race as blocking gates | — | ❌ **Open** — #66 open; gates exist (`go.yml` has `-race`, `coverage.yml` has threshold) but see G1 (mcp-server out-of-scope) + the CI-red reality. |

---

## Part 3 — Findings (new, from this audit)

### 🔴 F1 — CI is currently RED on `main` (not just the pre-existing failures)

After your #65 merged (`fix/ci-infra`), two workflows fail on `main`:
- **Coverage + Quality Gate** (run `35050409817`) — fails at the Quality Gate step (`Process completed with exit code 1`).
- **Team Validation** (run `35050409813`) — fails.

Your own #65 summary was candid that "Go test legs stay red until #62 lands (ingest race + migration gaps)." That's understood and expected — but it means **`main` has zero green CI** right now, and the coverage gate's exit-1 is a *separate* failure from the known-red test legs. Worth confirming the Quality Gate fail isn't masking a new regression independent of #62's items.

### 🟠 F2 — `mcp-server` is invisible to the root CI gate (the structural reason #66 escaped)

The root `go build ./...` and `go test -race ./...` do **not** build/test `mcp-server` because it's a separate module (`github.com/thearchitectit/guardrail-mcp`) and — per #65's own note — the nested-module `./...` prefix fails under `go.work`, so it's been run with `GOWORK=off` ad hoc. **Any breakage in that module (like #66) sails past CI.** This is the highest-leverage structural gap: a `mcp-server` build + test gate (or making it a `go.work` member) would have caught #66 the day it landed.

### 🟠 F3 — Cloud layer shipped without contract-test coverage or drift fixture

The digest asked for per-provider contract tests (09-03) and a fake-cloud drift fixture (09-03) and reconcile-twice idempotency (09-04). Current state: `internal/cloud/{aws,azure,gcp}/client_test.go` have only `*ImplementsInterface` + one range test each; `reconciler_test.go` has no idempotency test; **no fake/stub cloud driver exists** anywhere (`internal/cloud` has zero fake providers). Auto-enroll remains **process-flag gated and disabled-by-default** (`reconciler.go:34/66`, `SetAutoEnroll`), which is the right safety posture — but the digest's concern ("before pointing real cloud credentials at auto-enrollment") is exactly why the drift fixture + idempotency test should land *before* anyone flips that flag.

### 🟡 F4 — Branch hygiene: 11 local-only branches, remote has 0 worktree-agent-* but 1 `worktree-rmm09-step1`

- **Local-only (no upstream):** `chore/digest-followups` (10 commits), `sprint/3.1-secret-backends`, `3.2-secret-references`, `3.3-secret-rotation`, `3.4-scanning-gates`, `4.1-dashboard-ws`, `4.2-monitoring-ui`, `4.3-remote-access`, `worktree-agent-a6a18f...`, `worktree-agent-ac96ee...`, `infra/devgate-gates`.
- Of these, the `sprint/3.x`, `sprint/4.x`, and both `worktree-agent-*` branches are **already merged into `main`** — so they're safe to delete locally (keep archive refs only, per digest). `chore/digest-followups` (10 commits, all appending `NEXT-*` lines to `docs/DIGEST-FOLLOWUPS.md`) and `sprint/5.3-resilience-docs` (3 commits) and `infra/devgate-gates` are the only ones with unmerged content — decide merge vs archive.
- **Remote hygiene:** `origin/worktree-rmm09-step1` is the lone worktree branch on the remote; everything else is `dependabot/*` or `sprint/*`. The "~23 worktree-agent-*" the digest flagged are **gone** — good.

### 🟡 F5 — `infra/devgate-gates` and `sprint/2.2-a2a-grpc` exist on origin but were never PR'd

These have origin counterparts yet no open PR. Low priority, but they're the "security roadmap is drive-only" category the digest keeps raising — if the secret-backends/rotation work was merged to `main` without a PR, the merge decision is unrecorded.

### ⚪ F6 — `docs/reviews/` exists but is underused

There's a `docs/reviews/` directory (with `PLATFORM_REVIEW_2026-06-14.md` + `SPEC_REVIEW_BUNDLE_HANDOFF.md`). This audit + future digest-driven QA should land there so it's discoverable. (I'll file this audit there.)

---

## Part 4 — Recommended action list for the collaborator

**Blocking (do first, in order):**
1. **#66 RuleParser** — restore the dropped impl from git history; remove unused imports in `mcp-server` `updates` + `validation`.
2. **#64 migrations** — add missing `.down.sql`; resolve 006–012 gap (renumber or deliberate assertion).
3. **#63 race** — fix or properly gate the ingest callback synchronization.
4. **Add a `mcp-server` CI gate** (build + test) so #66-class breakage can't recur — this is the root-cause structural fix behind all three.

**Then (correctness debt the digest has flagged repeatedly):**
5. Cloud contract tests + fake-cloud drift fixture + reconcile-twice idempotency (#5/#6 in the trace table).
6. Verify the CI `Coverage + Quality Gate` exit-1 is the *known* coverage shortfall (P1 < 80%), not a new regression — and record which leg is red so `main` isn't silently "all red" without a triage note.
7. Branch hygiene: archive/delete the 8 already-merged local-only branches; PR or archive `chore/digest-followups` + `sprint/5.3-resilience-docs`.

**Nice-to-have:**
8. Write down what "VERIFIED" / "conformance" means for the relay contract (it's implemented + tested, but the "frozen contract" isn't codenamed as a frozen spec — R1).

---

## Appendix A — Verification commands run (for reproduceability)

```bash
cd mcp-server && GOWORK=off go build ./...          # → undefined RuleParser/... (D1)
go test -race -count=1 ./internal/security/ingest/  # → race in TestQueueSubmit (D3)
go test -count=1 -run TestEmbeddedMigrationsSanity ./internal/db/  # → gap 006-012 + .down.sql (D2)
go build ./...                                       # → passes (root module only)
git for-each-ref --format='%(refname:short) %(upstream:short)' refs/heads | awk '$2==""'  # → 11 local-only
git branch --merged main                             # → sprint/3.x, 4.x, worktree-agent-* all merged
```

## Appendix B — Live evidence sources (same-turn reads)

- Issues: `gh issue view 63/64/66` (all OPEN, author `drwhofan2k18-pixel`).
- PRs: `gh pr view 62` (OPEN, 2020+/263-), `gh pr view 65` (MERGED, 47+/26-).
- CI: `gh run list --branch main --limit 5` → two failing runs post-#65.
- Branch map: `git branch -a`, `git for-each-ref ... upstream`.
- Relay: `internal/relay/trust.go`, `ws.go:143`, `e2_private_relay_test.go:72`.
- Cloud: `internal/cloud/reconciler.go`, `internal/cloud/reconciler_test.go`, provider `client_test.go`.
