# Schema Health Check

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `scripts/schema-health-check.sh`, `.githooks/pre-commit`
> **App Path:** `scripts/schema-health-check.sh`

---

## Description

The schema health check validates the integrity of SQL migration sets before
they reach a database. Migrations are uniquely unforgiving: an empty migration
silently does nothing, a missing `.down.sql` makes a release unrollbackable, and
a numbering gap means two developers have unknowingly claimed the same slot.
None of these are syntax errors, so no compiler or linter catches them.

The checker validates five properties across every migration directory it finds:
presence and sequential numbering, up/down pairing, no numbering gaps, no empty
files, and basic SQL well-formedness.

Its severity model is deliberately asymmetric. Most findings are **advisory
warnings** — a numbering gap is often legitimate (a squashed or reverted
migration), so blocking on it would be noise. Two findings are **hard errors**:
an empty `.up.sql` (a migration that claims to change the schema but does not,
producing silent drift between the recorded version and reality) and an
`.up.sql` with no matching `.down.sql` (an unrollbackable deploy).

## User Story

**As** a developer adding a database migration,
**I want** pairing, numbering and emptiness validated before I commit,
**so that** I never ship a migration that cannot be rolled back or that silently
does nothing to the schema.

---

## Requirements

### 1. Interface

1.1. The checker MUST be invocable as `./scripts/schema-health-check.sh` with no
arguments.

1.2. Exit codes MUST be `0` = clean or warnings only, `1` = blocking error,
`2` = usage error.

1.3. It MUST run under `set -euo pipefail` and `cd` to the repository root
resolved from `BASH_SOURCE`.

1.4. It MUST maintain separate `ERRORS` and `WARNINGS` counters and MUST exit
non-zero **only** when `ERRORS > 0`.

1.5. Terminal output MUST distinguish the three outcomes:

| Outcome | Message |
|---------|---------|
| `ERRORS > 0` | `✗ SCHEMA HEALTH CHECK FAILED — N error(s), M warning(s)` |
| `WARNINGS > 0`, no errors | `⚠ SCHEMA HEALTH CHECK PASSED with M warning(s)` |
| Clean | `✓ SCHEMA HEALTH CHECK PASSED` |

### 2. Migration Directory Discovery

2.1. The checker MUST probe this fixed list and check every directory that
exists:

```
mcp-server/migrations
mcp-server/internal/database/migrations
```

2.2. When no migration directory exists it MUST print
`⊘ no migration directories found — skipping`, report
`✓ SCHEMA HEALTH CHECK PASSED (no migrations)` and exit `0`.

2.3. Each directory MUST be reported under a `checking: <dir>` heading, followed
by a `N up, M down migrations checked` summary.

2.4. The directory list is hardcoded. A migration set added under any other path
is silently unchecked — see Known Divergences.

### 3. Naming Convention

3.1. Migrations MUST follow the golang-migrate style
`<number>_<description>.up.sql` / `<number>_<description>.down.sql`.

3.2. The migration number MUST be the leading digit run of the basename,
extracted with `grep -oE '^[0-9]+'`.

3.3. Numbers MUST be compared in base 10 via `$((10#$num))`. Rationale:
zero-padded numbers such as `008` would otherwise be parsed as octal, and `008`
is not a valid octal literal — an arithmetic error.

### 4. Validation Rules

4.1. The checker MUST implement these five rules with the stated severity:

| # | Rule | Severity | Detection |
|---|------|----------|-----------|
| 1 | Empty `.up.sql` | **ERROR** (blocking) | `[ ! -s "$f" ]` |
| 2 | `.up.sql` with no matching `.down.sql` | **ERROR** (blocking) | `${up%.up.sql}.down.sql` absent |
| 3 | Empty `.down.sql` | WARNING | `[ ! -s "$f" ]` |
| 4 | Gap in numbering | WARNING | sorted-unique adjacent difference > 1 |
| 5 | Sequential numbering / basic SQL validity | WARNING | numbering sweep |

4.2. An empty `.up.sql` MUST block. Rationale: it records a schema version
without changing the schema, producing permanent silent drift between the
migration table and the real database.

4.3. A missing `.down.sql` MUST block. Rationale: the deploy becomes
unrollbackable, which is only discovered during an incident.

4.4. An empty `.down.sql` MUST warn with the explicit consequence
`rollback will be a no-op`, not block. Rationale: some migrations are genuinely
irreversible (data backfills) and an intentionally empty down-file is a valid
declaration of that.

4.5. Numbering gaps MUST warn and MUST name the missing range:

```
[WARNING] migration gap: 7 → 10 (missing 8..9)
```

4.6. Gap detection MUST sort numerically and deduplicate
(`sort -un`) before comparing adjacent entries, and MUST only run when more than
one migration number is present.

### 5. Pre-commit Integration Status

5.1. The pre-commit hook's header documents this check as step 5 of 5, described
as "migration validation (advisory, blocks on empty)".

5.2. The hook does **not** invoke `schema-health-check.sh`. Its implemented step
5 is an inline `cd web && npx tsc --noEmit`. The checker is currently a
manually-run tool. See Known Divergences.

5.3. When integrated, the hook SHOULD run it as the final step and MUST respect
its exit code, so an empty or unpaired migration blocks the commit while gaps
and empty down-files only warn.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | Pre-commit hook documents this as step 5/5 but implements `tsc` there instead | The check never runs automatically; an empty or unpaired migration can be committed |
| 2 | No CI workflow invokes `schema-health-check.sh` | Unenforced on both sides of the push |
| 3 | Migration directories are hardcoded to two `mcp-server/` paths | Migrations added elsewhere in the monorepo are silently unchecked |
| 4 | Header comment claims "SQL syntax is basic-valid (no obvious unclosed blocks)" | No unclosed-block detection is implemented; rule 5 is numbering only |
| 5 | Header says "Blocking only for: empty migration files"; missing `.down.sql` also blocks | Documented blocking set is narrower than the implemented one |
| 6 | A `.down.sql` with no matching `.up.sql` is not detected | Orphan down-migrations pass silently |

---

## Verification

```bash
# Run the checker
./scripts/schema-health-check.sh; echo "exit=$?"

# Confirm discovery paths and that the hook does not call it
grep -n 'mcp-server/migrations' scripts/schema-health-check.sh
grep -n 'schema-health' .githooks/pre-commit || echo "not invoked by hook"

# Inspect an existing migration set
ls mcp-server/migrations 2>/dev/null || echo "no migrations dir"
```

---

## Related Specifications

- `semantic-scanning` — the other script named-but-not-invoked by the hook
- `ci-pipeline` — where a future migration gate would run
- `infrastructure-standards` — Postgres/TimescaleDB service definition
