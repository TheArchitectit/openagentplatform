# Phase 7: Test Coverage + Quality Gate

> **Phase:** 7 (post-v1.2.0)
> **Status:** DRAFT
> **Source:** STATUS.md §1.3 success metrics (≥90% lines, ≥85% branches), coverage measurement 2026-09-07 (21.0% overall)
> **App Path:** `internal/`, `pkg/`, `cmd/`, `.github/workflows/`
> **Depends on:** All prior phases (0–6) complete

---

## Description

STATUS.md §1.3 defines success metrics that the codebase does not yet meet. The
most measurable gap is test coverage: as of 2026-09-07, the overall line
coverage is **21.0%** against a target of **≥90% lines, ≥85% branches**. 1835 of
2083 files have 0% coverage; the 248 files that are tested average 70.3%.

This spec does not invent targets — it closes the gap between the existing
§1.3 metrics and the measured reality, in priority order.

Anti-scope: re-architecting for testability, rewriting working code, adding new
features "while we're at it."

---

## User Story

**As** an MSP operator,
**I want** confidence that changes to OAP don't break existing behavior,
**so that** I can trust the platform for production workloads without manually
regressing every feature after each release.

---

## Requirements

### 1. Coverage Gate

1.1. The project MUST measure line coverage via `go test -cover` across all
Go workspace modules (`go.work`: root, `./a2a`, `./secrets`).

1.2. The coverage gate MUST enforce **≥90% line coverage** on new PRs and
**≥85% branch coverage** (via `-covermode=atomic` and a branch-coverage tool).

1.3. The gate MUST be wired into CI (`.github/workflows/`) so that PRs failing
the gate cannot merge.

1.4. The gate MUST produce a coverage report artifact (HTML or Cobertura XML)
visible in CI.

### 2. Priority-Ordered Coverage Improvement

Coverage improvement MUST proceed in priority order, highest business value
first. The following packages are ranked by (a) business criticality and (b)
current coverage gap:

| Priority | Package | Current | Target |
|----------|---------|---------|--------|
| P1 | internal/api | ~15% | ≥85% |
| P1 | internal/alerts | ~20% | ≥85% |
| P1 | internal/patches | ~10% | ≥85% |
| P2 | internal/policy | ~12% | ≥80% |
| P2 | internal/remote | ~18% | ≥80% |
| P2 | internal/relay | ~35% | ≥80% |
| P3 | pkg/agent | ~8% | ≥70% |
| P3 | internal/scheduled | ~5% | ≥70% |
| P3 | internal/billing | ~10% | ≥70% |
| P4 | cmd/ | ~5% | ≥50% (cmd is wiring, not logic) |

2.1. Each priority tier MUST be a separate implementation cycle: measure →
write tests → verify gate passes → move to next tier.

2.2. Within each tier, focus on the highest-risk code paths first: admission,
state machines, error handling, concurrency (the relay audit found double-unlock
bugs; the cloud audit found SDK drift; these patterns recur).

2.3. Mocking strategy: use `httptest.Server` for HTTP clients (cloud, EDR,
SIEM), `pgxpool` + test container or in-memory fake for stores, `interface`-based
dependency injection for service seams (the existing `CloudProviderClient` and
`Store` interfaces are the pattern to follow).

### 3. Quality Gate

3.1. Beyond coverage, a quality gate MUST run on every PR:

- `go vet ./...` — static analysis
- `staticcheck ./...` — deeper static analysis (golangci-lint)
- `gofmt -l .` — formatting

3.2. The quality gate MUST block merge on any finding.

### 4. Branch Coverage

4.1. Branch coverage MUST be measured separately from line coverage (line
coverage alone hides untested `if`/`else`, error paths, and concurrency
branches).

4.2. The ≥85% branch coverage target applies to packages in P1 and P2 tiers.

---

## Cross-References

- STATUS.md §1.3 (success metrics)
- `docs/plans/MASTER_IMPLEMENTATION_PLAN.md` §1.3 (targets)
- openspec/specs/phase7-test-coverage/spec.md (this spec)
- `.github/workflows/` (CI wiring)
- `go.work` (workspace modules: root, a2a, secrets)
