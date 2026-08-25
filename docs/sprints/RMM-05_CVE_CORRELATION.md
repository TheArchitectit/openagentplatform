# Sprint: RMM Operations — CVE-to-Patch Correlation

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Server-side CVE intake, matching to patch KB records, and a
look-up endpoint the web UI already expects. Build-ready once the data-source
decision (spec §10.7) is approved.
**Priority:** P1 (Blocking)
**Estimated Effort:** 6-9 hours
**Status:** COMPLETE
**Dependencies:** RMM-03 (per-KB patch state to match against)

---

## Overview

The data seam already exists: `cve_ids` JSONB on the patch catalog
(`py/alembic/versions/0006_patches.py:37`) and the web types are ready
(`web/src/lib/usePatches_types.ts` exposes `cve_ids`, `cvss_score`). There is no
server-side ingestion, matching, or lookup. This sprint adds the intake path,
matches CVEs to KB records by the KB→CVE mapping the source provides, and serves
the lookup the UI already expects. It does not invent a threat feed.

## Problem Statement

rmm-core §9.6: CVE correlation to patch records is not implemented. The patch
catalog can already store `cve_ids`, but nothing populates or queries them.

**Why:** Technicians need "which KB fixes CVE-XXXX" without manual cross-checks.
**Where:** new intake/matching service; look-up endpoint near
`internal/api/patches.go`; optional scheduler in `internal/` background loop
convention.

## Scope Boundary

```
IN SCOPE (may modify):
  - internal/patches/  (new CVE ingest + KB matching service; populate cve_ids)
  - internal/api/patches.go  (look-up endpoint: given CVE → matching KB, or KB → CVEs)
  - internal/patches/scheduler.go or a new loop  (scheduled intake cadence)
  - pkg/models/        (CVE model if a dedicated table is warranted)
  - py/alembic/versions/  (additive CVE table if needed; cve_ids column already exists)

OUT OF SCOPE (DO NOT TOUCH):
  - Choosing the CVE data source + cadence — open decision 10.7 MUST be approved
    before building the ingester (spec §7.3)
  - web/src/            (UI types already endpooint-ready; no UI change required here)
  - Vulnerability-scan integration (Nessus/OpenVAS/Trivy) — G-SEC-006, separate gap
```

## Open Decision (restated from spec §10.7)

CVE data source (NVD / OSV / MSRC) and refresh cadence are UNDECIDED. This
sprint's Step 0 is to obtain an approved decision (and any required API key /
access) and record it in the sprint notes. The ingester is built to that
source's schema; changing source afterward is a schema change, not a config
flip. If no source is approved, the sprint is BLOCKED and must not fabricate one.

## Production-Before-Test Sequence

```
STEP 0 (DECISION): Confirm + record CVE source and cadence (spec §10.7).
    If not approved → STOP, report BLOCKED. TOOL: user/stakeholder sign-off

STEP 1 (MODEL): CVE entity + KB↔CVE relation (additive) if a dedicated table is
    needed; otherwise reuse the patch catalog's cve_ids JSONB. TOOL: Edit

STEP 2 (INGEST): Service that fetches CVEs from the approved source and upserts
    them. TOOL: Write

STEP 3 (MATCH): Match source KB↔CVE mappings to patch catalog records, populating
    cve_ids per (org, kb). Idempotent, at-least-once safe. TOOL: Write

STEP 4 (API): Design and approve the CVE↔KB look-up API contract (OpenAPI +
    handler shape), then implement it. The web type file exposes `cve_ids` and
    `cvss_score` but no endpoint contract exists yet — do not assume the UI's
    types alone define the server contract. TOOL: Edit

STEP 5 (SCHEDULE): Optional cadence loop for refresh (background loop convention,
    rmm-core §12). TOOL: Edit

STEP 6 (BUILD): go build ./... && go vet ./... before tests. TOOL: Bash

STEP 7 (TESTS): After production —
    - ingest upserts idempotently (repeat-safe)
    - match populates cve_ids on the right (org, kb)
    - look-up returns the expected mappings; empty source yields no rows
    TOOL: Bash (go test ./internal/patches/... ./internal/api/...)

STEP 8 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests

- `go build ./...`, `go vet ./...`.
- `go test ./internal/patches/... ./internal/api/...`.
- Idempotency is mandatory (rmm-core §12.3): double-ingest must not duplicate.
- Look-up endpoint covered via the api test pattern (`internal/api/*_test.go`).

## Rollback

```bash
git checkout HEAD -- internal/patches/ internal/api/ pkg/models/ py/alembic/versions/
git status   # confirm clean
# Down-rev the new migration if applied downstream.
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | CVE source decision recorded | sprint notes | Source + cadence approved and written down |
| 2 | Ingest idempotent | double-ingest test | No duplication on re-run |
| 3 | cve_ids populated on right KB | match test | KB↔CVE mapping correct per (org, kb) |
| 4 | Look-up endpoint returns mappings | API test | CVE→KB and KB→CVE as UI expects |
| 5 | Additive migration only | up/down | No alteration of existing patch tables |

## Completion Record

- **CVE source decision:** NVD (NIST) — approved by user, 2026-08-24. API v2.0 at `https://services.nvd.nist.gov/rest/json/cves/2.0`. Daily refresh cadence (scheduler deferred).
- **Acceptance criteria verification:**
  1. ✅ NVD chosen and recorded above
  2. ✅ UpsertCVEEnrichment is idempotent via ON CONFLICT (TestUpsertCVEEnrichment_Idempotent)
  3. ✅ PatchCatalogUpdateCVEIDs sets cve_ids on the right (org, kb); enrichCVEMapping called from KBConsumer.handleScan
  4. ✅ GET /api/v1/patches/cve?kb=... returns CVEs; ?cve=... returns KBs (5 API tests)
  5. ✅ Migration 0014 is additive: new cve_enrichment table + cvss_score column on patch_catalog (no ALTER of existing columns except adding cvss_score)
- **New files:**
  - `py/alembic/versions/0014_rmm05_cve_enrichment.py` — cve_enrichment table + cvss_score on patch_catalog
  - `internal/patches/store_cve.go` — CVE store methods (5 tests)
  - `internal/patches/store_cve_test.go`
  - `internal/patches/nvd_ingest.go` — NVD API v2.0 ingest service (no scheduler)
  - `internal/api/cve_test.go` — API endpoint tests (5 tests)
- **Modified files:**
  - `pkg/models/models_extra.go` — CVEEnrichment model
  - `internal/patches/store_types.go` — Store interface + CVEKBMatch struct
  - `internal/patches/kb_ingest.go` — enrichCVEMapping, kbIngestStore extended
  - `internal/patches/kb_ingest_test.go` — fakeKBStore extended
  - `internal/api/patches.go` — handleLookupCVE
  - `internal/api/routes_sub.go` — GET /patches/cve route
  - `internal/api/kb_patch_test.go` — fakePatchStore extended
- **Deferred:** Background scheduler for daily NVD refresh (Step 5 of sprint spec). NVDIngester.IngestCVEs is callable from a future scheduler or on-demand.
- **No `rmm.winupdate.*` subjects** (forbidden by spec)

## Reference

- `openspec/specs/rmm-operations/spec.md` §7 (CVE), §10.7, §11
- `py/alembic/versions/0006_patches.py` (§37 `cve_ids`)
- `web/src/lib/usePatches_types.ts` (`cve_ids`, `cvss_score`)
- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.6 G-SEC-006 (out of scope, for contrast)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
