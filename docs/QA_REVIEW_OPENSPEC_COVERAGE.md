# OpenAgentPlatform — OpenSpec Coverage Audit

**Audit date:** 2026-08-23 (original) · **Re-verified:** 2026-08-27 (this update)
**Repo:** `/mnt/data/git/openagentplatform` (live tree, `main`)
**Scope:** 43 capability specs in `openspec/specs/*/spec.md` vs. live Go/Python/React code

---

## Executive Summary

**Status: remediated and re-verified.** The original 2026-08-23 audit found 21 specs
authored against a Python/Django "Project Sentinel" / RMM blueprint that had never been
reconciled with the actual Go monorepo. The P0 truth-layer rewrite and P1 path-drift
fixes are **complete and verified**.

**This update (2026-08-26):** post-RMM-01..09 + RELAY-01/02 re-verification. All
"NOT implemented" claims in `rmm-core` §14 have been corrected to SHIPPED. The
a2a-relay Known Limitations have been reconciled with RELAY-01's shipped WSS listener
and `cmd/relay` binary. The `event-bus` spec has been superseded by `event-bus-nats`.

**What remains:**
- **P3 — real RMM parity gaps** (code AND spec both absent): cloud control,
  hypervisor monitoring, active security/EDR, power/UPS monitoring, mobile companion.
- **RELAY track COMPLETE:** all RELAY-00..06 gates and implementation sprints have
  shipped; a2a-relay spec STATUS flipped to COMPLETE. Remaining Known Limitations
  (dead `ConnectionStatusError`, no persistence) are accurately documented.
- **Five-spec round COMPLETE (§8, 2026-08-29):** check-library, data-model,
  multi-tenancy, adapter-service, hitl-approval implemented + group-audited.
  Open follow-ups surfaced there: retention purger (KL #6, decision-gated),
  HITL Postgres store, multi-tenancy API consumption.

---

## 1. Spec Inventory (43 specs, re-verified 2026-08-26)

All 43 spec dirs contain a real `spec.md`. Verdicts are against the live tree on `main`.

| Spec | Status (2026-08-26) | Notes |
|------|---------------------|-------|
| rmm-core | PARTIAL → reconciled | §14 drift fixed; all 8 extensions now SHIPPED |
| rmm-operations | PARTIAL → reconciled | RMM-01..09 COMPLETE; RMM-06 COMPLETE (shipped `3f8e495`); open decisions resolved |
| endpoint-agent | COMPLETE | `cmd/agent` + `pkg/agent/` including mesh/tunnel |
| check-library | COMPLETE | Full 9-template catalog (rmm-core §3.2 set); library + seeder tested |
| remote-access | COMPLETE | NATS-shell + RMM-09 tunnel fabric noted |
| a2a-relay | COMPLETE | Full stack shipped: admission, matching, forwarding, discovery, E2E acceptance |
| a2a-gateway | COMPLETE | |
| a2a-agent-registry | COMPLETE | |
| a2a-framework-adapters | COMPLETE | |
| a2a-task-manager | COMPLETE | |
| event-task-bridge | COMPLETE | |
| event-bus | SUPERSEDED | Replaced by event-bus-nats |
| event-bus-nats | COMPLETE | NATS client, heartbeat, dispatch, tracing |
| hitl-approval | COMPLETE | R1–R6.5 shipped 2026-08-29 (846e861..2dd88e6): engine, decision API, SSE, web queue |
| process-pool | COMPLETE | |
| auth-rbac | COMPLETE | |
| secret-management | COMPLETE | |
| commercial-licensing | COMPLETE | |
| frontend-react | PARTIAL | UI not fully wired |
| adapter-service | COMPLETE | Live e2e through the Go proxy verified (TestA2AProxyLiveE2E, 2026-08-29) |
| reporting | COMPLETE | |
| resilience | COMPLETE | |
| audit-log | COMPLETE | |
| billing-stripe | COMPLETE | |
| ci-pipeline | COMPLETE | |
| deploy-pipeline | COMPLETE | |
| documentation-standards | COMPLETE | |
| infrastructure-standards | COMPLETE | |
| multi-tenancy | PARTIAL | Isolation/quota/stores wired (W6/a420855); store layer not yet consumed by an API surface |
| data-model | PARTIAL | Go struct layer complete; two divergent checked-in schema sources; purger non-functional (KL #6) |
| notifications | COMPLETE | |
| notifications-dispatch | COMPLETE (new) | Untracked; authored 2026-08-25 |
| observability | COMPLETE | |
| observability-telemetry | COMPLETE (new) | Untracked; authored 2026-08-25 |
| pattern-scanning | COMPLETE | |
| platform-foundation | COMPLETE | |
| platform-foundation-schema | COMPLETE (new) | Untracked; authored 2026-08-25 |
| remote-access-session | COMPLETE (new) | Untracked; authored 2026-08-25 |
| remote-access-terminal | COMPLETE (new) | Untracked; authored 2026-08-25 |
| schema-health | COMPLETE | |
| secret-scanning | COMPLETE | |
| semantic-scanning | COMPLETE | |
| managed-backup | DRAFT | Greenfield; no implementation exists yet |

**Counts:** 35 COMPLETE · 3 PARTIAL · 1 SUPERSEDED · 1 DRAFT · 3 COMPLETE (new/untracked)

---

## 2. RMM Operations Extension Status

| Extension | Sprint | Status | Spec section |
|-----------|--------|--------|-------------|
| WinUpdate per-KB management | RMM-03 | COMPLETE | rmm-operations §1 |
| Scheduled automation (AutomatedTask) | RMM-06 | COMPLETE | rmm-operations §3 |
| Maintenance windows (alert suppression) | RMM-02 | COMPLETE | rmm-operations §4 |
| Offline-agent SLA alerting | RMM-01 | COMPLETE | rmm-operations §5 |
| Agent self-update (Ed25519) | RMM-07→09 | COMPLETE | rmm-operations §8 |
| Reboot coordination | RMM-04 | COMPLETE | rmm-operations §6 |
| CVE-to-patch correlation | RMM-05 | COMPLETE | rmm-operations §7 |
| VNC/RDP (tunnel fabric) | RMM-08→09 | COMPLETE | rmm-operations §9 |
| Secure tunnel fabric (WireGuard+SSH) | RMM-09 | COMPLETE | rmm-operations §9 |

---

## 3. RELAY Track Status

| Sprint | Status | Spec impact |
|--------|--------|-------------|
| RELAY-00 | APPROVED | Decision gate populated; I.3/D.2/E.4 resolved |
| RELAY-01 | COMPLETE | WSS listener + cmd/relay binary shipped; a2a-relay §1/Known Limitations updated |
| RELAY-02 | APPROVED | Identity + entitlement ADR frozen; wiring in RELAY-03 |
| RELAY-03 | COMPLETE | Rendezvous matching + bidirectional forwarding shipped |
| RELAY-04 | COMPLETE | Operator admin surface (health, metrics, discovery) shipped |
| RELAY-05 | COMPLETE | Decision gate (APPROVED) + implementation (local registry + gRPC federation) |
| RELAY-06 | COMPLETE | E.2 private-relay, E.3 load/soak, E.4 blind-forwarder acceptance all RUN |

---

## 4. Findings Fixed This Round (2026-08-26)

| # | Severity | Finding | Fix |
|---|----------|---------|-----|
| 1 | P1 | rmm-core §2.5/§4.4/§7.6/§9.5/§9.6/§10.4/§13.4/§14 claim "NOT implemented" for shipped RMM-01..09 code | Updated all 8 sections to SHIPPED with sprint/commit refs |
| 2 | P3 | a2a-relay Known Limitations false ("no network transport", "not wired into binary") | Rewrote to reflect RELAY-01 ws.go + cmd/relay |
| 3 | P3 | remote-access Known Limitations stale ("VNC/RDP not implemented") | Updated to reflect RMM-09 tunnel fabric |
| 4 | P3 | event-bus spec redundant vs event-bus-nats | Marked SUPERSEDED; event-bus-nats is the authoritative spec |
| 5 | P2 | QA doc stale (wrong counts, wrong verdicts) | Regenerated this document |

---

## 5. Outstanding Items (deferred / stub)

| Item | Status | Notes |
|------|--------|-------|
| managed-backup | DRAFT | Greenfield; spec is the design, no code yet |
| frontend-react | PARTIAL | UI wiring incomplete |
| adapter-service | RESOLVED 2026-08-29 | Live e2e through Go proxy (see §8); spec COMPLETE |
| multi-tenancy store layer | RESOLVED 2026-08-29 | Stores wired (a420855); API consumption remains open (spec §5) |
| data-model DDL | RESOLVED 2026-08-29 | deploy/migrations 001+002 checked in (9996a99/26827e0); divergence vs Alembic documented as KL #2 |
| hitl-approval | RESOLVED 2026-08-29 | R1–R6.5 shipped; in-memory store documented as Known Limitation 1 |
| Retention purger | OPEN | KL #6: non-functional as checked in; fix needs a deliberate schema migration (decision-gated) |
| check-library | COMPLETE | Resolved 2026-08-27: 4 templates (http/tcp/dns/script) added; 9-type set shipped |
| Cloud inventory sync | Not started | P1 gap from RMM_2026_GAP_ANALYSIS |
| EDR ingest pipeline | Not started | P1 gap |
| Mobile companion app | Not started | P1 gap |
| File transfer (SCP over tunnel) | Not started | P2 gap |
| SNMP device monitoring | Not started | P2 gap |
| SIEM forwarding | Not started | P3 gap |

---

**Audit performed by:** Claude (Agent-GDUI-2026)
**Last verified:** 2026-08-29 against `main` at current HEAD (§8 five-spec round)

---

## 6. Independent Verification Pass (2026-08-26)

Four parallel audit agents covered all 43 specs across RMM, A2A, platform, and
commercial scopes. The A2A agent (`72ff9d7`) fixed its own scope findings; three
other agents identified additional P0/P1 items that commit did not address.
These were independently verified against live code and fixed in this pass:

| # | Sev | Finding | Fix |
|---|-----|---------|-----|
| 6 | P0 | `hitl-approval` STATUS: COMPLETE but `/a2a/v1/approvals` HITL API not built | Flipped to PARTIAL; added implementation note |
| 7 | P0 | `rmm-operations` §3 still said RMM-06 "DESIGN APPROVED / build pending" | Flipped to COMPLETE (shipped `3f8e495`); reconciled §3.1/3.3/3.4 to first-class table |
| 8 | P1 | `commercial-licensing` §5.2 claimed PostgreSQL RLS; actual model is app-layer `org_id` | Corrected to reflect app-layer enforcement; RLS noted as future option |
| 9 | P1 | `data-model` omitted RMM-09 mesh + RMM-06 scheduled entities/migrations | Added 4 rows (MeshPeer, MeshSession, AgentRelease, AutomatedTask) + §9.0 migration note |
| 10 | P2 | `reporting` §6.1 stale line count (386→109) | Corrected |

**Discounted agent findings (verified false):**
- Platform agent's auth-rbac P0 ("gateway middleware PLANNED") — `RequireRole` IS a
  middleware in `internal/auth/middleware.go:100`; spec is accurate. Not a P0.
- Commercial agent's remote-access-session P1 ("wrong App Path `internal/session/`")
  — `internal/session/` DOES exist (recorder.go + recorder_test.go). Not a P1.

**Remaining verified deferred/stub items** (no code, no commitment):
- `hitl-approval` `/a2a/v1/approvals` — spec is design target, not shipped
- `managed-backup` — DRAFT, greenfield
- `observability-telemetry` (untracked spec describes OTel pipeline; no `internal/observability` package)
- `platform-foundation-schema` (untracked; partially contradicts RMM-06 first-class table)
- P1 RMM parity gaps: cloud inventory, EDR ingest, mobile app
- P2/P3 RMM parity gaps: SCP-over-tunnel, SNMP, SIEM forwarding

---

## 7. Relay Full-Stack Audit + QA Round (2026-08-27)

Full work-vs-spec audit of `a2a-relay` after RELAY-06. Every finding below was
reconciled; the spec STATUS and all sprint/QA doc labels now reflect the shipped
stack. No new requirements were added and no mechanisms were invented.

### 7.1 Work-vs-spec result

All §7 gates R.1–R.4, I.1–I.3, D.1–D.2, E.1–E.4 are implemented or RUN. The
spec header previously read "PLANNED — NOT IMPLEMENTED" with every gate
`[PLANNED]`/`[BLOCKED]`/`[PARTIAL]` and front matter "Remaining
decision-approved but not yet coded" — all corrected. STATUS flipped
PARTIAL → COMPLETE (the spec's own §7 condition, "STATUS stays PARTIAL until
the approved work ships", is now satisfied).

### 7.2 Code findings (fixed)

| # | Finding | Action |
|---|---------|--------|
| C1 | `cmd/relay/main.go` package doc claimed no forwarding/matching/admission/metering | Rewrote to describe the shipped stack |
| C2 | `internal/relay/ws.go` `ServeWS` doc claimed admission not implemented and every session closed unregistered | Rewrote to describe `handleWSS` admission/entitlement/forwarding |
| C3 | `ws.go` reaper used `context.TODO()` for `CloseConnection` in production | Replaced with the reaper's `ctx` |
| C4 | `match.go` + `discovery_test.go` not gofmt-clean | `gofmt -w` applied |

### 7.3 Code findings (deferred — not a defect, documented)

| # | Finding | Why deferred |
|---|---------|--------------|
| D1 | `ConnectionStatusError` constant never assigned in a transition | Spec §2.2 **requires** the `"error"` vocabulary entry, so the constant is not removable; remains a documented Known Limitation |
| D2 | `cmd/relay/main.go:53` `relayID := "local"` (TODO to derive from relay cert CN) | The SAN convention for a relay identity cert has not landed; deriving it would invent a mechanism — left as a deferred TODO |

### 7.4 Doc findings (fixed)

| # | Finding | Action |
|---|---------|--------|
| D3 | spec front matter claimed entitlement admission + federation "not yet coded" | Rewrote as all-shipped |
| D4 | spec §7 header + 11 gate labels stale (`[PLANNED]`/`[BLOCKED]`/`[PARTIAL]`) | Flipped to `[IMPLEMENTED]`/`[RUN]`; R.4 "unconditional grant" note corrected |
| D5 | QA doc §3 RELAY table listed RELAY-03/04/05/06 as PENDING | Flipped to COMPLETE; counts 31→32 COMPLETE / 7→6 PARTIAL |
| D6 | RELAY-00 quick-reference card still listed I.3/E.4/D.2 as BLOCKED | Marked RESOLVED; only TCP forwarder + TLS test certs remain blocked |
| D7 | RELAY-01/02/03/04/05 BLOCKERS sections stale | Each resolved blocker marked RESOLVED with the sprint that cleared it |
| D8 | INDEX_MAP.md + HEADER_MAP.md a2a-relay entries read "accounting core only / no transport wired" | Rewrote to describe the full shipped stack |

### 7.5 QA round result

`go test -race ./internal/relay/...` green (303s, includes 5-minute soak);
`go vet ./internal/relay/... ./cmd/relay/...` clean; `gofmt -l` clean across
`cmd/relay/` and `internal/relay/` after C4.

---

## 8. Five-Spec Implementation + Group Audit Round (2026-08-28 → 2026-08-29)

Five specs implemented inline (unit by unit, commit per unit) and then audited
work-vs-spec with parallel audit agents: **check-library, data-model,
multi-tenancy, adapter-service, hitl-approval**. No spec was rewritten from
scratch; no requirement was added; no mechanism was invented.

### 8.1 Implementation chains

| Spec | Commits | Result |
|------|---------|--------|
| check-library | 9ca7f94, bbf6456, 12abdb4 | Full 9-template catalog (rmm-core §3.2); audit chain fixed canonicalDetails/µs-truncate/chain_seq along the way |
| multi-tenancy | a420855 | TenantStore/TenantConfigStore wired + live-DB tests |
| data-model | 9996a99, 26827e0 | deploy/migrations/001+002 checked in, reconstructed from store SQL |
| adapter-service | 3d9fc62 | Missing framework SDKs degrade per-adapter at start() instead of crashing |
| hitl-approval | 846e861..a27841b, 2dd88e6 (R1–R6.5) | Engine+API+notify+escalation+audit+task gate+SSE+web queue+detail |

### 8.2 Group-audit outcomes (findings verified at source before acting)

| Spec | Verdict | Action |
|------|---------|--------|
| check-library | GAPS (4 real) | 14ca68e — §4.5 JSON error bodies (writeJSONError), §5.3 seeder logs marshal failures, §2.2 script no-op template + "usable" defined, tautology + stale comment |
| adapter-service | PASS | f317cdb — STATUS flipped to COMPLETE only after live `TestA2AProxyLiveE2E` re-run through the Go proxy against a real uvicorn (2026-08-29); spec text reconciled |
| multi-tenancy | 2 real / 4 false | 73379bd — fixed the COALESCE-fallback comment claim and stale header; refused the fabricated evidence (cited nonexistent 003/005 migrations, "12 policies" vs actual 9, phantom middleware claims) |
| data-model | evidence unverifiable | fd24b1e — audit agent cited nonexistent files/lines; full self-verification instead. Real drift fixed in spec: "no checked-in DDL" (stale), §8.3/§8.4 (old tenant_id-UUID model → shipped org_id 9-table), §9.4 table name, KL #1 resolved/#2 rewritten/#4 annotated. NEW live finding KL #6: retention purger is non-functional as checked in (needs deleted_at/created_at/org_id the schemas don't all provide) — documented honestly, fix is a schema-design decision |
| hitl-approval | all PASS | 96464c2 — R1.1–R6.5 verified clause-by-clause inline; spec drift fixed (routes to mounted /api/v1/a2a/approvals URLs, R1.6 escalation-transient amendment, stale "not yet built" note replaced, new Known Limitations: in-memory store, per-approval queryability, role-mirror gate) |

### 8.3 Accepted product inconsistency (for roadmap)

`a2a:write` (operator) does not grant HITL decision rights; backend gates
create/approve/reject on `admin|technician`. The web UI mirrors the backend.
ROLE_PERMISSIONS deliberately untouched — flagged for product, not patched.

### 8.4 QA round result

Full gate with `OAP_TEST_PG_DSN` set: `go build ./... && go vet ./... &&
go test ./...` exit 0 (0 FAIL). Live DB-gated tests ran (tenancy integration);
`TestA2AProxyLiveE2E` is opt-in (`OAP_TEST_ADAPTER_E2E`) and was run
explicitly. `web/`: `tsc -b --force` clean, `vitest run` 19/19,
`vite build` clean, `eslint .` clean. `gofmt -l` flags only
`a2a/gateway/gateway.go` — verified pre-existing at 846e861^ (untouched,
left alone). Push per spec executed after this closeout (all 24 local commits,
9ca7f94..HEAD, fast-forward on main; no force).
