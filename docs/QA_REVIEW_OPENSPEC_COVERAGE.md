# OpenAgentPlatform — OpenSpec Coverage Audit

**Audit date:** 2026-08-23 (original) · **Re-verified:** 2026-08-26 (this update)
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
- **RELAY track PARTIAL:** a2a-relay spec STATUS correctly reflects PARTIAL (forwarding,
  discovery, and identity code not yet built; decisions approved).

---

## 1. Spec Inventory (43 specs, re-verified 2026-08-26)

All 43 spec dirs contain a real `spec.md`. Verdicts are against the live tree on `main`.

| Spec | Status (2026-08-26) | Notes |
|------|---------------------|-------|
| rmm-core | PARTIAL → reconciled | §14 drift fixed; all 8 extensions now SHIPPED |
| rmm-operations | PARTIAL → reconciled | RMM-01..09 COMPLETE; RMM-06 COMPLETE (cron); open decisions resolved |
| endpoint-agent | COMPLETE | `cmd/agent` + `pkg/agent/` including mesh/tunnel |
| check-library | PARTIAL | Server-side catalog; accurate vs code |
| remote-access | COMPLETE | NATS-shell + RMM-09 tunnel fabric noted |
| a2a-relay | PARTIAL | Accounting core + RELAY-01 WSS listener + RELAY-02 ADR; forwarding RELAY-03 |
| a2a-gateway | COMPLETE | |
| a2a-agent-registry | COMPLETE | |
| a2a-framework-adapters | COMPLETE | |
| a2a-task-manager | COMPLETE | |
| event-task-bridge | COMPLETE | |
| event-bus | SUPERSEDED | Replaced by event-bus-nats |
| event-bus-nats | COMPLETE | NATS client, heartbeat, dispatch, tracing |
| hitl-approval | COMPLETE | |
| process-pool | COMPLETE | |
| auth-rbac | COMPLETE | |
| secret-management | COMPLETE | |
| commercial-licensing | COMPLETE | |
| frontend-react | PARTIAL | UI not fully wired |
| adapter-service | PARTIAL | Standalone service; not integrated with gateway routing |
| reporting | COMPLETE | |
| resilience | COMPLETE | |
| audit-log | COMPLETE | |
| billing-stripe | COMPLETE | |
| ci-pipeline | COMPLETE | |
| deploy-pipeline | COMPLETE | |
| documentation-standards | COMPLETE | |
| infrastructure-standards | COMPLETE | |
| multi-tenancy | PARTIAL | Isolation/quota wired (W6); store layer still unwired |
| data-model | PARTIAL | Go struct layer complete; no checked-in DDL |
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

**Counts:** 31 COMPLETE · 7 PARTIAL · 1 SUPERSEDED · 1 DRAFT · 3 COMPLETE (new/untracked)

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
| RELAY-02 | APPROVED | Identity + entitlement ADR frozen; no code (decision-only) |
| RELAY-03 | PENDING | Forwarding/matching (next build sprint) |
| RELAY-04 | PENDING | Metering/observability |
| RELAY-05 | PENDING | gRPC discovery service (D.2 resolved) |
| RELAY-06 | PENDING | E2E/private/load (I.3/D.2/E.4 resolved) |

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
| adapter-service | PARTIAL | Not integrated with gateway routing |
| multi-tenancy store layer | PARTIAL | TenantStore/TenantConfigStore unwired |
| data-model DDL | PARTIAL | No checked-in DDL for platform tables |
| check-library | PARTIAL | Server-side catalog; accurate vs code |
| Cloud inventory sync | Not started | P1 gap from RMM_2026_GAP_ANALYSIS |
| EDR ingest pipeline | Not started | P1 gap |
| Mobile companion app | Not started | P1 gap |
| File transfer (SCP over tunnel) | Not started | P2 gap |
| SNMP device monitoring | Not started | P2 gap |
| SIEM forwarding | Not started | P3 gap |

---

**Audit performed by:** Claude (Agent-GDUI-2026)
**Last verified:** 2026-08-26 against `main` at current HEAD
