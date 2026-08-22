# Semantic Scanning

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `scripts/semantic-scan.sh`, `.githooks/pre-commit`
> **App Path:** `scripts/semantic-scan.sh`

---

## Description

Semantic scanning is the compiler-level gate that sits above regex pattern
scanning. Where `pattern-scan.sh` matches text, `semantic-scan.sh` invokes the
real toolchains — `go vet`, `tsc --noEmit`, Python's `py_compile` — so it catches
whole classes of defect that no regex can see: type errors, malformed format
strings, unreachable code, syntax errors, broken builds.

It runs five checks across the monorepo's three languages, all blocking:

1. `go vet ./...` — structural bugs, format-string mismatches, unreachable code
2. `tsc --noEmit` — TypeScript type errors
3. Python AST parse via `py_compile` — syntax errors
4. Unused Go imports — heuristic check
5. `go build ./...` — the code actually compiles

Every check degrades gracefully: if a toolchain is absent the step is skipped
with a `⊘` marker rather than failing, so the gate works on a partial dev
environment. Each check also honours `--staged` to narrow its scope to the
packages and files a commit actually touches.

## User Story

**As** a developer committing across a Go/TypeScript/Python monorepo,
**I want** each language's real compiler and vetter run against my change before
the commit lands,
**so that** a type error or a broken build is caught on my machine in seconds
instead of failing the CI matrix minutes after I push.

---

## Requirements

### 1. Interface

1.1. The scanner MUST support:

| Invocation | Behaviour |
|-----------|-----------|
| `./scripts/semantic-scan.sh` | Scan everything |
| `./scripts/semantic-scan.sh --staged` | Scope to packages/files with staged changes |
| `./scripts/semantic-scan.sh --help` | Print usage, exit 0 |

1.2. Exit codes MUST be `0` = clean, `1` = violations found, `2` = usage error.

1.3. The scanner MUST run under `set -euo pipefail` and `cd` to the repository
root resolved from `BASH_SOURCE`.

1.4. It MUST accumulate failures in a single `FAILED` flag and run **all five
checks regardless of intermediate failures**, only then exiting non-zero.
Rationale: a developer should see every problem in one pass, not fix-and-rerun
five times.

1.5. Each check MUST be labelled `[N/5]` in output and MUST print `✓ clean`,
`✗ … FAILED`, or `⊘ skipped (<reason>)`.

### 2. Toolchain Degradation

2.1. Every check MUST be guarded by a `command -v` probe (or a file-existence
probe for `tsc`), and MUST skip rather than fail when its toolchain is absent.

2.2. A skipped check MUST NOT set the failure flag.

2.3. Skip conditions MUST be:

| Check | Skipped when |
|-------|--------------|
| `go vet` | `go` not on PATH |
| `tsc --noEmit` | `web/` or `web/tsconfig.json` absent |
| Python AST | `python3` not on PATH |
| Unused imports | `go` not on PATH |
| `go build` | `go` not on PATH |

### 3. Check 1 — `go vet`

3.1. In full mode it MUST run `go vet ./...`.

3.2. In `--staged` mode it MUST derive the package set from staged `.go` files
and vet only those directories:

```bash
GO_FILES="$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$')"
GO_DIRS=$(echo "$GO_FILES" | xargs -I{} dirname {} | sort -u | sed 's|^|./|')
echo "$GO_DIRS" | xargs go vet
```

3.3. When no `.go` files are staged in `--staged` mode it MUST report
`⊘ no Go files staged`.

### 4. Check 2 — TypeScript Type-Check

4.1. It MUST run `npx tsc --noEmit` from the `web/` directory.

4.2. Staged-file discovery MUST exclude declaration and generated files:
`grep -v '\.d\.ts$'` and `grep -v '\.gen\.'`.

4.3. `tsc` is whole-project by nature: in `--staged` mode the scanner MUST still
type-check the entire `web` project when any TS file is staged. Rationale: a
change in one module can break types in an untouched consumer; per-file
type-checking would be unsound.

### 5. Check 3 — Python AST Parse

5.1. It MUST parse each candidate `.py` file with
`python_compile(..., doraise=True)` and report `[SYNTAX ERROR] <file>` per
failure.

5.2. File discovery MUST be `git diff --cached` in `--staged` mode and
`git ls-files -- '*.py'` in full mode, excluding `__pycache__`, `.venv` and
`node_modules`.

5.3. It MUST fail when the error count exceeds zero, reporting the total.

5.4. This check verifies **syntax only**, not imports, types or runtime
behaviour. `ruff` in CI and `deploy.sh` covers lint; this catches the
unparseable file.

### 6. Check 4 — Unused Go Imports

6.1. It MUST scan non-test Go files (`grep -v '_test.go'`, and `grep -v vendor`
in full mode) for imports whose package name never appears as `pkgname.` in the
file, reporting `[UNUSED] <file>: import "<pkg>"`.

6.2. It MUST fail when any unused import is found.

6.3. This check is explicitly a **heuristic**. It derives the package name from
`basename` of the import path and counts `\bpkgname\.` occurrences. It is
therefore unsound in both directions:

| Case | Behaviour |
|------|-----------|
| Aliased import (`import x "pkg/y"`) | False positive — alias used, basename not |
| Package name ≠ last path segment | False positive |
| Blank/underscore import (`_ "pkg"`) | False positive — intentionally unreferenced |
| Dot import (`. "pkg"`) | False positive |
| Name appears only in a comment or string | False negative |

6.4. Because Go's own compiler treats an unused import as a hard error, check 5
(`go build`) is the authoritative detector. Check 4 exists to name the specific
import for faster remediation.

### 7. Check 5 — `go build`

7.1. It MUST run `go build ./...` in **both** full and `--staged` mode — build
verification is never narrowed. Rationale: a staged change can break a package
that has no staged files.

7.2. It MUST fail on any build error.

### 8. Integration Status

8.1. `deploy.sh` covers the substance of this gate at release time via
`go build ./cmd/server`, `go vet ./...`, `pnpm build`, `pnpm lint` and
`ruff check`.

8.2. `.github/workflows/go.yml` covers `go vet ./...` and `go build ./...` per
matrix cell; `web.yml` covers lint; `python.yml` covers `ruff`.

8.3. The pre-commit hook does **not** invoke `semantic-scan.sh`. Its header
comment names "Semantic scan" as step 4 of 5, but the implemented steps 4 and 5
are an inline `go vet` over staged packages and an inline `cd web && npx tsc
--noEmit`. See Known Divergences.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | Pre-commit hook header documents "semantic scan" + "schema health check" as steps 4–5, but implements inline `go vet` (4/5) and `tsc` (5/5) | `semantic-scan.sh` is never run by the hook; Python AST, unused-import and `go build` checks are unenforced at commit time |
| 2 | Hook's inline step 4 vets only staged packages; `go build ./...` is not run at all | A commit can break the build of an untouched package and still pass the hook |
| 3 | Check 4's unused-import heuristic mishandles aliased, blank, dot and non-basename imports | False positives block commits; `go build` is the real gate |
| 4 | Check 4 uses `grep -c` whose count includes matches inside comments/strings | Under-reports genuinely unused imports |
| 5 | No dedicated CI workflow invokes `semantic-scan.sh` | The script's unique checks run only when invoked manually |

---

## Verification

```bash
# Full semantic scan
./scripts/semantic-scan.sh; echo "exit=$?"

# Staged-scope run (what a hook integration would use)
./scripts/semantic-scan.sh --staged; echo "exit=$?"

# Confirm the hook does NOT call this script
grep -n 'semantic-scan' .githooks/pre-commit || echo "not invoked by hook"
```

---

## Related Specifications

- `pattern-scanning` — regex-level gate this complements
- `schema-health` — the other script named-but-not-invoked by the hook
- `ci-pipeline` — where `go vet` / `tsc` / `ruff` are enforced post-push
