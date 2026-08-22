# Pattern Scanning

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `scripts/pattern-scan.sh`, `.guardrails/prevention-rules/pattern-rules.json` (v3.0.0)
> **App Path:** `scripts/pattern-scan.sh`

---

## Description

Pattern scanning is the platform's regex-level anti-pattern gate. It reads a
declarative rule set from `.guardrails/prevention-rules/pattern-rules.json` and
scans tracked source files for known-bad constructs — SQL built by string
concatenation, hardcoded credentials, `eval()`, empty catch blocks, `os.Exit` in
library code, `yaml.load` without `SafeLoader`, `subprocess(shell=True)`.

The design separates *rules* (data, version-controlled JSON with a schema) from
*mechanism* (a small shell scanner). Adding a rule is a data change requiring no
script edit, which keeps the guardrail set reviewable as a diff.

Severity is tiered so the gate is strict without being unusable: `critical` and
`error` block the commit, while `warning` rules report and let the commit
through. This lets the project encode aspirational standards (no `any`, no
`time.Sleep` in tests, TODOs need tickets) alongside genuine defects without
blocking work on pre-existing debt.

## User Story

**As** a developer about to commit,
**I want** known-dangerous code patterns caught locally with the rule ID,
message and a concrete suggested fix,
**so that** a security anti-pattern is corrected at authoring time rather than
found in review or in production.

---

## Requirements

### 1. Interface

1.1. The scanner MUST support:

| Invocation | Behaviour |
|-----------|-----------|
| `./scripts/pattern-scan.sh` | Scan all tracked files |
| `./scripts/pattern-scan.sh --staged` | Scan staged files only (pre-commit) |
| `./scripts/pattern-scan.sh --help` | Print usage, exit 0 |

1.2. Exit codes MUST be `0` = clean, `1` = blocking violations, `2` = usage
error or missing/unparseable rules file.

1.3. The scanner MUST exit `2` with an explicit error when
`.guardrails/prevention-rules/pattern-rules.json` is absent. Rationale: a
missing rules file must never be silently interpreted as "no violations".

1.4. Rule parsing MUST work with either `node` or `python3`, preferring `node`
and falling back to `python3`, and MUST exit `2` if neither is available.

### 2. Rule Schema

2.1. Each rule MUST declare these fields, validated against
`pattern-rules.schema.json`:

| Field | Purpose |
|-------|---------|
| `rule_id` | Stable identifier, `PREVENT-NNN` |
| `name` | Human-readable rule name |
| `enabled` | Boolean; disabled rules MUST be skipped |
| `pattern` | Regex matched against file contents |
| `forbidden_context` | Regex that, when matched, SUPPRESSES the finding (may be `null`) |
| `message` | What is wrong |
| `severity` | `critical`, `error`, or `warning` |
| `file_glob` | Array of globs the rule applies to |
| `suggestion` | Concrete remediation |

2.2. `forbidden_context` MUST act as an exclusion/suppression predicate — it
names the contexts in which the pattern is acceptable (tests, mocks, `cmd/`,
examples, vendored code), not a required context.

2.3. Rules MUST be scoped by `file_glob`. A glob MAY be path-qualified
(`web/src/**/*.ts`) to restrict a rule to one tree.

### 3. Rule Inventory

3.1. The rule set at v3.0.0 contains **23 enabled rules** — 5 `critical`,
9 `error`, 9 `warning`. IDs are `PREVENT-001`–`003` and `PREVENT-010`–`029`
(004–009 are intentionally unallocated).

3.2. **Critical rules (5)** — MUST block:

| ID | Name | Languages |
|----|------|-----------|
| PREVENT-002 | SQL injection risk via string concatenation | py, js, ts, rb, java, go |
| PREVENT-003 | Hardcoded credentials | all (`*`) |
| PREVENT-026 | SQL string concatenation in Go | go |
| PREVENT-028 | PyYAML unsafe `load` | py |
| PREVENT-029 | `subprocess` with `shell=True` | py |

3.3. **Error rules (9)** — MUST block:

| ID | Name | Languages |
|----|------|-----------|
| PREVENT-001 | `JSON.parse` without null check | js, ts, jsx, tsx |
| PREVENT-010 | `fmt.Print*` in library code | go |
| PREVENT-012 | `debugger` statement | js, ts, jsx, tsx |
| PREVENT-013 | `eval()` usage | js, ts, jsx, tsx |
| PREVENT-014 | Empty catch block | js, ts, tsx |
| PREVENT-015 | `os.Exit` in library code | go |
| PREVENT-020 | `import from 'fs'` in frontend | `web/src/**` |
| PREVENT-021 | `process.env` in frontend (Vite uses `import.meta.env`) | `web/src/**` |
| PREVENT-025 | HTTP server without explicit timeouts | go |

3.4. **Warning rules (9)** — report only, MUST NOT block:

| ID | Name | Languages |
|----|------|-----------|
| PREVENT-011 | `console.log` in source | js, ts, jsx, tsx |
| PREVENT-016 | Unchecked error return | go |
| PREVENT-017 | Request context without timeout | go |
| PREVENT-018 | TODO without ticket reference | go, ts, tsx, py, js |
| PREVENT-019 | Explicit `any` type | ts, tsx |
| PREVENT-022 | Goroutine without panic recovery | go |
| PREVENT-023 | `time.Sleep` in tests | test files |
| PREVENT-024 | Hardcoded IP address | go, ts, tsx, py |
| PREVENT-027 | Deprecated `ioutil` usage | go |

3.5. Library-code rules (PREVENT-010, PREVENT-015) MUST exempt
`cmd/`, `_test.go`, `scripts/`, `internal/cli`, `internal/team`, `mcp-server/`,
`examples/` and `vendor/`. Rationale: `fmt.Println` and `os.Exit` are correct in
entrypoints and CLIs; they are defects only in importable library code.

3.6. PREVENT-024 MUST exempt `0.0.0.0`, `127.0.0.1`, `localhost` and
test/mock/fixture/example contexts.

### 4. Severity Enforcement

4.1. The scanner MUST exit non-zero if and only if at least one `critical` or
`error` severity rule matched without suppression.

4.2. `warning` matches MUST be printed but MUST NOT affect the exit code.

4.3. Output for each violation SHOULD include the rule ID, severity, file
location, message and suggestion, so the fix requires no lookup.

### 5. Pre-commit Integration

5.1. The pre-commit hook MUST run pattern scanning as **step 2 of 5**, after the
secret scan and before the regression check.

5.2. The hook MUST prefer `./scripts/pattern-scan.sh` when `node` is available
and the script exists.

5.3. When that precondition fails the hook MUST fall back to an **inline
8-pattern scan** requiring no `node`, covering: SQL string concat, `fmt.Print*`,
`console.log`, `debugger`, TODO without ticket, empty catch, `eval()`, and
`os.Exit` in `internal/`.

5.4. The fallback MUST block the commit on any match. Rationale: the gate
degrades in coverage on a minimal toolchain, but never silently disappears.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | Brief/older docs describe "22 rules"; the file defines **23** enabled rules | Counts in prose docs are stale — the JSON is authoritative |
| 2 | Inline hook fallback covers 8 patterns vs 23 in the JSON | On a `node`-less machine, 15 rules are unenforced at commit time |
| 3 | Fallback blocks on `console.log` and TODO-without-ticket, which are `warning` in the JSON | The fallback is *stricter* than the real rule set for those two |
| 4 | PREVENT-016's `pattern`/`forbidden_context` pair is broad and heuristic | Prone to false positives; mitigated by `warning` severity |
| 5 | Pattern scanning has no dedicated CI workflow | Enforced only at pre-commit; a `--no-verify` commit is never re-checked |

---

## Verification

```bash
# Full scan
./scripts/pattern-scan.sh; echo "exit=$?"

# Rule inventory by severity
python3 -c "
import json,collections
r=json.load(open('.guardrails/prevention-rules/pattern-rules.json'))['rules']
print('total',len(r),'enabled',sum(1 for x in r if x.get('enabled')))
print(collections.Counter(x['severity'] for x in r))"
```

---

## Related Specifications

- `semantic-scanning` — compiler-level checks beyond regex
- `secret-scanning` — dedicated credential gate (overlaps PREVENT-003)
- `ci-pipeline` — where the guardrails workflows run
