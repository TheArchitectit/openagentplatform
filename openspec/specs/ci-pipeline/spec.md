# CI Pipeline

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `.github/workflows/*.yml` (8 workflows)
> **App Path:** `.github/workflows/`

---

## Description

The CI Pipeline is the set of GitHub Actions workflows that validate every push
and pull request to OpenAgentPlatform. It splits into two families:

- **Language workflows** (`go.yml`, `web.yml`, `python.yml`) — build, lint and
  test each language stack of the monorepo on a version/OS matrix. Each is
  path-filtered so a change confined to one stack does not spend runner minutes
  on the others.
- **Governance workflows** (`secret-validation.yml`, `regression-guard.yml`,
  `guardrails-lint.yml`, `documentation-check.yml`, `team-validation.yml`) —
  enforce the project's cross-cutting rules: no leaked credentials, no
  file-size regressions, bounded PR scope, the 500-line documentation limit,
  and team-configuration validity.

CI is the *final* authority on gates that can only run in a clean hosted
environment (gitleaks with full history, multi-OS matrices). It is deliberately
**not** the first line of defence for secrets: a public repository cannot
un-publish a credential, so the local pre-commit hook and `deploy.sh` run their
own scans before anything reaches GitHub. See `secret-scanning`.

## User Story

**As** a contributor opening a pull request,
**I want** every language stack and governance rule checked automatically, with
only the relevant jobs running for my change,
**so that** I get fast, trustworthy feedback and cannot merge a change that
breaks a build, leaks a secret, or violates a project standard.

---

## Requirements

### 1. Trigger and Branch Model

1.1. Language workflows MUST trigger on both `push` and `pull_request` for the
branch set `[main, 'sprint/**']`.

1.2. Governance workflows MUST use the trigger appropriate to their scope:

| Workflow | Trigger | Branches |
|----------|---------|----------|
| `go.yml` | push + PR, path-filtered | `main`, `sprint/**` |
| `web.yml` | push + PR, path-filtered | `main`, `sprint/**` |
| `python.yml` | push + PR, path-filtered | `main`, `sprint/**` |
| `secret-validation.yml` | push + PR, no path filter | `main`, `develop`, `sprint/**` |
| `regression-guard.yml` | push + PR (`opened`/`synchronize`/`reopened`) | `main`, `master`, `sprint/**` |
| `guardrails-lint.yml` | PR only | `main`, `develop`, `sprint/**` |
| `documentation-check.yml` | PR only, path-filtered to docs/markdown | any |
| `team-validation.yml` | push + PR + `workflow_dispatch` | `main`, `master` |

1.3. `secret-validation.yml` MUST NOT be path-filtered — a secret may be
introduced in any file type, so the scan MUST run on every push.

1.4. The branch sets are inconsistent across workflows (`develop` and `master`
appear in some governance workflows but no language workflow). Any new workflow
SHOULD standardise on `[main, 'sprint/**']` unless it has an explicit reason to
differ.

### 2. Path Filtering

2.1. Each language workflow MUST restrict itself to the paths it owns, and MUST
include its own workflow file so changes to the CI definition are self-testing:

| Workflow | Paths |
|----------|-------|
| `go.yml` | `cmd/**`, `internal/**`, `pkg/**`, `a2a/**`, `go.mod`, `go.sum`, `.github/workflows/go.yml` |
| `web.yml` | `web/**`, `.github/workflows/web.yml` |
| `python.yml` | `py/**`, `.github/workflows/python.yml` |

2.2. `documentation-check.yml` MUST filter to `docs/**`, `*.md`, and
`.github/**/*.md`.

### 3. Concurrency and Permissions

3.1. Language workflows MUST declare a concurrency group keyed by workflow and
ref, with `cancel-in-progress: true`, so a force-push supersedes in-flight runs:

```yaml
concurrency:
  group: go-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

3.2. Every workflow SHOULD declare least-privilege `permissions`. The language
workflows and `secret-validation.yml`, `guardrails-lint.yml` and
`documentation-check.yml` declare `contents: read`.

3.3. `regression-guard.yml` and `team-validation.yml` currently declare no
`permissions` block and therefore inherit the repository default. They SHOULD be
tightened to `contents: read` (plus any write scope they genuinely need).

### 4. Go Workflow

4.1. `go.yml` MUST run two jobs: `lint` and `test`.

4.2. The `lint` job MUST run `golangci-lint` pinned to `v1.64.8` via
`golangci/golangci-lint-action@v6` with `--timeout=5m`, on Go 1.23.

4.3. The `test` job MUST run a matrix of `os × go-version` with
`fail-fast: false`:

| Axis | Values |
|------|--------|
| `os` | `ubuntu-latest`, `macos-latest`, `windows-latest` |
| `go-version` | `1.22`, `1.23` |

4.4. Each matrix cell MUST run, in order: `go mod tidy`, `go vet ./...`,
`go build ./...`, then
`go test -race -coverprofile=coverage.out -covermode=atomic ./...`.

4.5. Coverage MUST be uploaded as an artifact named
`go-coverage-${{ matrix.os }}-${{ matrix.go-version }}` with `if: always()`, so
coverage survives a failing test run.

4.6. `go vet ./...` in CI covers the **root module only**. The standalone
submodules (`mcp-server`, `cmd/team-cli`, `examples/go`) are outside the root
`go.work` and are NOT gated by this workflow. `deploy.sh` mirrors this scope
deliberately.

### 5. Web Workflow

5.1. `web.yml` MUST run `lint` and `test` jobs on Node 22 (lint) and a
`node-version: ["20", "22"]` matrix (test) with `fail-fast: false`.

5.2. Dependency installation MUST use pnpm 9 via `pnpm/action-setup@v4` with
`pnpm install --frozen-lockfile`, so a lockfile drift fails CI rather than
silently resolving new versions.

5.3. The pnpm store MUST be cached via `actions/cache@v4` keyed on
`hashFiles('web/pnpm-lock.yaml')`.

### 6. Python Workflow

6.1. `python.yml` MUST run `lint` and `test` jobs.

6.2. The `lint` job MUST run both `ruff check .` and `ruff format --check .` on
Python 3.12, so formatting drift is a CI failure, not a review comment.

6.3. Dependencies MUST be managed with `uv` via `astral-sh/setup-uv@v5` with
caching keyed on `py/uv.lock`, installed with `uv sync --all-extras`.

6.4. The `test` job MUST run a matrix of `os: [ubuntu-latest, macos-latest]` ×
`python-version: ["3.11", "3.12"]` with `fail-fast: false`. Windows is
intentionally excluded from the Python matrix.

### 7. Governance Workflows

7.1. `secret-validation.yml` MUST run three independent jobs — gitleaks (with
`fetch-depth: 0`), a `.env`-file check, and a hardcoded-secret pattern sweep.
Its normative behaviour is specified in `secret-scanning` §4.

7.2. `regression-guard.yml` MUST check out with `fetch-depth: 0`, run
`regression_check.py --all` in both JSON and human form, and additionally
detect bug-fix commits (`^(fix|bugfix|hotfix)(\(.+\))?:`) in a PR to prompt for
accompanying regression tests. Its `regression_check.py` invocations are
suffixed `|| true`, making the report **advisory in CI**; the blocking
enforcement lives in the pre-commit hook and `deploy.sh`.

7.3. `guardrails-lint.yml` MUST analyse PR scope — categorising changed files
into documentation, code, config and workflow counts — and warn when a PR
touches more than 20 files. It MUST also run a forbidden-files check.

7.4. `documentation-check.yml` MUST enforce the 500-line documentation limit as
a hard failure. See `documentation-standards` §2.

7.5. `team-validation.yml` MUST validate team sizes and status via
`scripts/team_manager.py`, and on `refs/heads/main` MUST export the team
configuration as a `teams-export` artifact.

### 8. Action Pinning

8.1. All third-party actions MUST be referenced by major version tag at minimum
(`actions/checkout@v4`, `actions/setup-go@v5`, `actions/upload-artifact@v4`).

8.2. Tools whose output gates the build MUST be pinned to an exact version so a
CI result is reproducible — `golangci-lint` is pinned to `v1.64.8`.

8.3. Workflows that inspect git history (`secret-validation.yml`,
`regression-guard.yml`, `guardrails-lint.yml`) MUST check out with
`fetch-depth: 0`.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | Branch sets differ (`main`/`sprint/**` vs `+develop` vs `+master`) | A push to `develop` runs secret + guardrails checks but no language tests |
| 2 | `regression-guard.yml` suffixes its checks with `|| true` | File-size regressions do not fail CI; only local gates block |
| 3 | `regression-guard.yml` / `team-validation.yml` declare no `permissions` | Inherit broader default token scope than needed |
| 4 | CI tests Go `1.22`/`1.23`; `deploy/Dockerfile.server` builds on `golang:1.26.3-alpine` | The released artifact is built on a toolchain no CI job exercises |
| 5 | `team-validation.yml` installs from root `requirements.txt` while `python.yml` uses `py/uv.lock` | Two different dependency sources for Python in CI |

---

## Verification

```bash
# List workflows and their triggers
grep -A4 '^on:' .github/workflows/*.yml

# Confirm the Go matrix
grep -A8 'strategy:' .github/workflows/go.yml

# Confirm pinned linter version
grep -A3 'golangci-lint-action' .github/workflows/go.yml
```

---

## Related Specifications

- `secret-scanning` — the local + CI secret gates
- `deploy-pipeline` — the release gate that mirrors CI locally
- `documentation-standards` — the 500-line limit CI enforces
