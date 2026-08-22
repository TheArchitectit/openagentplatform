# Deploy Pipeline

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `deploy.sh`, `deploy/docker-compose.yml`, `deploy/Dockerfile.*`
> **App Path:** `deploy.sh`, `deploy/`

---

## Description

`deploy.sh` is the authoritative release pipeline for OpenAgentPlatform. It is a
single-argument script (`./deploy.sh 0.2.0`) that takes the repository from a
clean working tree to a pushed, annotated, version-tagged release — refusing to
proceed at the first sign of trouble.

Its defining constraint is that **this repository is fully public**. Anything
pushed is world-readable permanently and cannot be un-published; automated
secret harvesters scrape public repos within minutes. CI's gitleaks job catches
leaks *after* a push, which is too late. `deploy.sh` therefore runs the local
secret scan as a hard gate **twice** — once before any commit, and again
immediately before `git push` to catch anything introduced during the gate
itself.

The script is ordered so that the cheapest and most catastrophic checks run
first: a dirty tree aborts before the secret scan, the secret scan aborts before
the multi-minute build gate, and the build gate aborts before any history is
mutated. Nothing is committed, tagged, or pushed unless every prior step passed.

## User Story

**As** the release publisher,
**I want** one command that refuses to publish unless the tree is clean, free of
secrets, fully built, tested, linted, regression-checked and Docker-buildable,
**so that** I cannot irreversibly leak a credential or ship a broken image to a
public repository through a moment of inattention.

---

## Requirements

### 1. Invocation and Version Validation

1.1. The script MUST accept exactly one argument and exit `2` on any other
count, printing usage.

1.2. It MUST accept a `v`-prefixed version and normalise it by stripping the
leading `v`.

1.3. It MUST validate the version against semver and exit `2` if it does not
match:

```
^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$
```

1.4. It MUST run under `set -euo pipefail` and `cd` to the repository root
derived from `BASH_SOURCE`, so it behaves identically regardless of the caller's
working directory.

### 2. Gate Order (Normative)

2.1. The pipeline MUST execute these stages in exactly this order. Any non-zero
exit aborts the release:

| # | Stage | Failure mode |
|---|-------|--------------|
| 1 | Clean git tree (unstaged, staged, untracked) | exit 1 |
| 2 | **Secret scan #1** (pre-commit gate) | exit 1 |
| 2b | Toolchain pre-flight (`go`, `pnpm`, `python3`, `docker`) | exit 1 |
| 3 | Full gate: build + test + lint + regression check | exit 1 |
| 4 | Version bump (`web/package.json`) | — |
| 5 | Docker image build (server + web) | exit 1 |
| 6 | Commit bump, create annotated tag | — |
| 6b | **Secret scan #2** (pre-push gate) | exit 1 |
| 6c | `git push --follow-tags` | exit 1 |
| 7 | Print post-release instructions | — |

2.2. The two secret scans are non-negotiable and MUST NOT be reordered,
short-circuited, or made conditional. Rationale: gate #1 protects against
publishing an existing secret; gate #2 protects against a secret introduced by
the gate itself (generated config, build artifacts, version-bump side effects).

### 3. Clean-Tree Requirement

3.1. The script MUST reject **all three** forms of dirty state, each with a
distinct message:

- unstaged changes — `git diff --quiet`
- staged-but-uncommitted changes — `git diff --cached --quiet`
- untracked non-ignored files — `git ls-files --others --exclude-standard`

3.2. Untracked files MUST be rejected, not ignored. Rationale: a release must
ship only tracked, reviewed files; stray runtime state (`.pi/`, ad-hoc scripts)
must never be baked into a release.

### 4. Toolchain Pre-flight

4.1. Before the expensive gate, the script MUST verify `go`, `pnpm`, `python3`
and `docker` are all on `PATH`, failing fast with the missing tool named.
Rationale: a missing tool should surface in seconds, not as a confusing
mid-gate error minutes in.

### 5. Full Gate

5.1. The gate MUST run these checks, each aborting on failure:

| Check | Command |
|-------|---------|
| Server builds | `go build -o /tmp/oap-server-check ./cmd/server` |
| Go static analysis | `go vet ./...` (root module only) |
| Web build | `cd web && pnpm install --frozen-lockfile && pnpm build` |
| Web lint | `cd web && pnpm lint` |
| Python lint | `cd py && ruff check .` (skipped if `ruff` absent) |
| Regression check | `regression_check.py --all --soft-as-hard [...] --pre-commit` |

5.2. `go vet` MUST be scoped to the root module only, mirroring `go.yml`. The
standalone submodules (`mcp-server`, `cmd/team-cli`, `examples/go`) are separate
deployables outside the root `go.work` and are NOT part of the monorepo release
contract.

5.3. The temporary build artifact `/tmp/oap-server-check` MUST be removed after
the build check.

5.4. Python lint MUST be non-fatal when `ruff` is not installed, emitting a
`WARN` that CI enforces it. Rationale: the release gate must not be hostage to
an optional local-only linter.

5.5. The regression check MUST run in release-gate mode with
`--soft-as-hard`, which promotes soft file-size warnings to hard failures. When
a previous tag exists it MUST be passed as the headroom baseline:

```bash
PREV_TAG="$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || true)"
if [ -n "$PREV_TAG" ]; then
  python3 scripts/regression_check.py --all --soft-as-hard --soft-as-hard-base "$PREV_TAG" --pre-commit
else
  python3 scripts/regression_check.py --all --soft-as-hard --pre-commit
fi
```

5.6. The regression check in this mode also covers the settings-surface audit
and `pnpm audit` for runtime HIGH/CRITICAL web dependency vulnerabilities.

### 6. Version Bump

6.1. The current version MUST be read from `web/package.json`, defaulting to
`0.0.0` if unreadable.

6.2. The bump MUST be a no-op when the current version already equals the
target, and MUST rewrite `web/package.json` with 2-space indentation and a
trailing newline when it differs.

6.3. `web/package.json` is the single source of truth for the platform version.

### 7. Docker Build Proof

7.1. The pipeline MUST build the `server` and `web` images before tagging:

```bash
docker compose -f deploy/docker-compose.yml build server web
```

7.2. Build output MUST be captured to `/tmp/oap-docker-build.log`, and on
failure the last 30 lines MUST be echoed to stderr. Rationale: a full Docker
build log is unreadable inline, but a silent failure is undebuggable.

7.3. This stage exists to prove the images *compile* — it does not push images
to a registry. Image distribution is by source pull + rebuild on each runtime
host (§9).

### 8. Commit, Tag and Push

8.1. The version bump MUST be committed only if `web/package.json` actually
changed, with a `chore(release): v<version>` message and the
`Co-Authored-By: openagentplatform deploy.sh <noreply@openagentplatform>`
trailer.

8.2. The tag MUST be annotated (`git tag -a "v<version>"`) and MUST be skipped
if it already exists rather than failing.

8.3. Secret scan #2 MUST run after tagging but strictly before pushing. On
failure it MUST print the exact recovery commands and abort with nothing
pushed:

```
git tag -d $TAG && git reset --soft HEAD~1
```

8.4. The push MUST use `git push --follow-tags`, falling back to
`git push --set-upstream origin <branch> --follow-tags` if the first attempt
fails.

### 9. Post-Release Instructions

9.1. On success the script MUST print the released version, the pushed tag, and
the per-host runtime upgrade sequence:

```bash
git pull && docker compose -f deploy/docker-compose.yml build
docker compose -f deploy/docker-compose.yml up -d
curl -sS http://localhost:8080/health
```

9.2. It MUST state that CI remains the final authority on CI-only gates
(gitleaks, go/python/web test matrices).

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | Post-release text verifies `/health`; compose + Dockerfile healthchecks use `/healthz` | Operator may probe a path the server does not serve |
| 2 | `deploy/Dockerfile.server` builds on `golang:1.26.3-alpine`; CI tests Go 1.22/1.23 | Released binary built on an untested toolchain |
| 3 | Gate runs `pnpm build`/`pnpm lint` but no `pnpm test`; Go tests are not run either | Test execution is delegated entirely to CI, post-push |
| 4 | Images are built as proof only, never pushed to a registry | Each host rebuilds from source; no immutable released artifact |

---

## Verification

```bash
# Confirm gate ordering and the two secret scans
grep -n '^# --- [0-9]' deploy.sh
grep -n 'scan-secrets.sh' deploy.sh   # must appear twice

# Confirm semver validation and release-gate mode
grep -n 'not a valid semver' deploy.sh
grep -n 'soft-as-hard' deploy.sh
```

---

## Related Specifications

- `secret-scanning` — the scanner invoked at both gates
- `ci-pipeline` — the post-push authority
- `infrastructure-standards` — Docker/compose topology being built
