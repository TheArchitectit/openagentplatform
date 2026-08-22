# Documentation Standards

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `CLAUDE.md` §0/§4, `.github/workflows/documentation-check.yml`, `INDEX_MAP.md`, `HEADER_MAP.md`, `TOC.md`
> **App Path:** `docs/`, `openspec/specs/`

---

## Description

Documentation standards in OpenAgentPlatform exist to solve a specific problem:
**agent context budget**. The repository is designed to be worked on by LLM
agents, and an agent that must read a 3,000-line document to answer one question
has spent its context on navigation rather than reasoning.

The response is a three-layer navigation system plus a hard size cap:

- **`INDEX_MAP.md`** — keyword → document lookup table. Read *first*; claims
  60–80% token savings on targeted lookups.
- **`HEADER_MAP.md`** — `file:line` → header index, enabling a targeted
  `Read(offset=N, limit=50)` instead of a full-file read.
- **`TOC.md`** — complete file listing and organisation structure.
- **500-line maximum** per document, enforced in CI as a hard failure.

The prescribed navigation flow is `INDEX_MAP → identify doc → HEADER_MAP → read
specific section with offset`. Capability specifications additionally follow the
OpenSpec format under `openspec/specs/<capability>/spec.md`.

## User Story

**As** an agent or developer needing one specific fact from the documentation,
**I want** a keyword index and a line-level header map over documents that are
all small enough to read whole,
**so that** I can retrieve exactly the section I need without spending my context
budget on documents I did not need.

---

## Requirements

### 1. Navigation Maps

1.1. `INDEX_MAP.md` MUST be read before any documentation lookup, and MUST
provide a quick-lookup table of `Keyword | Document | Path | Purpose`.

1.2. `HEADER_MAP.md` MUST index headers as `file_path:line_number → Header`,
grouped per document, so a reader can jump directly to a section.

1.3. `TOC.md` MUST provide the complete file listing and organisation structure.

1.4. The prescribed lookup flow MUST be:

```
INDEX_MAP.md → identify document → HEADER_MAP.md → Read(file, offset, limit)
```

1.5. `INDEX_MAP.md` and `HEADER_MAP.md` MUST be updated whenever a document is
added, moved, renamed or substantially restructured (`CLAUDE.md` §4).

### 2. 500-Line Maximum

2.1. No document may exceed **500 lines** (`CLAUDE.md` §4).

2.2. `documentation-check.yml` MUST enforce this as a **hard CI failure** on any
pull request touching `docs/**`, `*.md`, or `.github/**/*.md`.

2.3. The workflow MUST scan every `*.md` file in the repository — excluding
`node_modules` and `.git` — not only the files changed in the PR:

```bash
for file in $(find . -name "*.md" -type f | grep -v node_modules | grep -v .git); do
  lines=$(wc -l < "$file")
  if [ "$lines" -gt 500 ]; then ... exit 1; fi
done
```

2.4. On violation the workflow MUST list every offending file with its line count
and direct the author to `docs/standards/MODULAR_DOCUMENTATION.md`.

2.5. Documents approaching the limit SHOULD be split by concern into a directory
with an index, rather than truncated.

### 3. OpenSpec Format

3.1. Each capability MUST occupy its own directory:
`openspec/specs/<capability>/spec.md`.

3.2. Capability names MUST be lowercase, hyphen-separated, and name a capability
rather than a file or component (`secret-scanning`, not `scan_secrets_sh`).

3.3. Each spec MUST open with a metadata block:

```markdown
> **Phase:** <phase or "Infrastructure">
> **STATUS: <COMPLETE | PARTIAL | PLANNED>**
> **Source:** <authoritative files/sections>
> **App Path:** <implementation path>
```

3.4. Each spec MUST contain these sections in order:

| Section | Contents |
|---------|----------|
| Description | What the capability is and why it exists |
| User Story | `As … I want … so that …` |
| Requirements | Numbered, with nested `N.M` sub-requirements |
| Known Divergences | Where implementation differs from spec (when any exist) |
| Verification | Commands that check the spec's claims |
| Related Specifications | Cross-links to sibling capabilities |

3.5. Requirements MUST use RFC 2119 keywords (MUST, MUST NOT, SHOULD, MAY) and
MUST be numbered hierarchically (`4.2` under section `4`) so they are citable.

3.6. Requirements MUST be verifiable. A requirement that cannot be checked by
reading code or running a command SHOULD be moved to Description.

3.7. Specs MUST record **actual implemented behaviour**, not intent. Where the
code diverges from the documented design, the divergence MUST be recorded in
Known Divergences rather than silently described as working.

3.8. Specs are documents and are therefore bound by the 500-line limit (§2).

### 4. Writing Standards

4.1. Documents MUST state *why* a constraint exists, not only what it is. A rule
whose rationale is unrecorded is a rule that gets removed by the next person who
finds it inconvenient.

4.2. Structured, scannable data (rule inventories, gate orders, trigger
matrices) SHOULD be presented as tables.

4.3. Code, commands and file paths MUST be in backticks or fenced blocks, and
fenced blocks SHOULD declare a language.

4.4. Cross-references MUST use relative links so they survive a clone.

4.5. Documents MUST NOT embed credentials, tokens or real connection strings,
including in examples. Placeholders MUST use the documented safe values
(`secret-scanning` §3.3) so the secret scanner does not flag them.

### 5. Agent Context Discipline

5.1. `CLAUDE.md` §2 defines token-saving rules that documentation MUST support:
no filesystem exploration, no re-reading, targeted reads only.

5.2. Documentation structure MUST make targeted reads possible — which is the
direct purpose of the 500-line cap and `HEADER_MAP.md`.

5.3. New documentation MUST be registered in `INDEX_MAP.md` with a keyword, so
it is discoverable without a filesystem scan.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | **38 of 230** tracked `*.md` files exceed the 500-line limit | `documentation-check.yml` scans all files, so it fails on *any* docs PR regardless of what the PR changed |
| 2 | `HEADER_MAP.md` (1,576 lines) itself violates the rule it indexes | The navigation aid is too large to read whole — the problem it exists to solve |
| 3 | Worst offenders: `docs/plans/A2A_PLAN.md` (3,217), `docs/plans/MCP_SERVER_PLAN.md` (2,093), `docs/architecture/ENDPOINT_API.md` (2,061) | Each is 4–6× the cap |
| 4 | `documentation-check.yml` gates the whole tree, not the PR diff | No incremental path to compliance: a docs PR cannot pass until all 38 are split |
| 5 | Referenced `docs/standards/MODULAR_DOCUMENTATION.md` is cited by CI as the splitting guide | Must exist and stay authoritative for the failure message to be actionable |

**Recommendation:** scope `documentation-check.yml` to changed files
(`git diff --name-only origin/main...HEAD`) so new documents are held to the
limit while the 38 pre-existing violations are split incrementally. Otherwise the
gate is permanently red and will be routinely bypassed.

---

## Verification

```bash
# Count violations of the 500-line rule
for f in $(git ls-files '*.md'); do
  n=$(wc -l < "$f"); [ "$n" -gt 500 ] && echo "$n $f";
done | sort -rn

# Confirm CI enforces it as a hard failure
grep -n 'MAX_LINES\|exit 1' .github/workflows/documentation-check.yml

# Confirm every spec carries the required metadata block
grep -l 'STATUS:' openspec/specs/*/spec.md
```

---

## Related Specifications

- `ci-pipeline` — `documentation-check.yml` trigger and job definition
- `infrastructure-standards` — operational documentation requirements
- `secret-scanning` — safe placeholder values for documentation examples
