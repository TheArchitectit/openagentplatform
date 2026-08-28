# Sprint RELAY-04: Metering & Observability

**Sprint Date:** 2026-08-23 (Saturday)
**Sprint Focus:** Approve and expose a secure operator observability surface over
existing relay accounting, without inventing billing metrics or tenant access.
**Priority:** P2 (Normal)
**Status:** COMPLETE

Spec reference: `openspec/specs/a2a-relay/spec.md` §4, §6, and §7.1 R.4.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `GetMetrics`, logging contract, and approved RELAY-03 metering semantics | [ ] |
| **DECIDE BEFORE CODE** | Freeze API auth, authorization, binding, names, and units | [ ] |
| **NO TENANT LEAKAGE** | Never expose all tenants by default | [ ] |
| **NO BILLING INVENTION** | No new billing metric names or units | [ ] |
| **NO PUBLIC ADMIN DEFAULT** | Listener must fail closed until approved | [ ] |

---

## PROBLEM STATEMENT

`GetMetrics` provides in-memory accounting, but there is no approved external
operator surface. A route such as unauthenticated `GET /metrics?tenant=...` would
let callers select another tenant and could expose all tenants. Route names,
formats, authentication, authorization, listener binding, metric units, and
billing ownership have not been approved.

---

## SCOPE BOUNDARY

```
DECISION SCOPE:
  - Admin listener bind/default exposure and TLS requirements
  - Authentication and operator authorization
  - Tenant context source; whether all-tenant access exists and for which role
  - Route names, response format, metric names, and exact units
  - Health/readiness semantics and sensitive logging rules
  - Billing/export ownership and persistence boundary

CONTINGENT IMPLEMENTATION SCOPE:
  - A secured operator server and tests
  - cmd/relay configuration and bounded shutdown wiring

OUT OF SCOPE:
  - No unauthenticated tenant selector or all-tenant endpoint
  - No new accounting state, persistence, or billing export
  - No discovery or E2E encryption work
```

---

## EXECUTION DIRECTIONS

### STEP 1: Approve operator API contract

Obtain explicit approval for every decision above. Tenant scope MUST derive from
verified operator authorization, not solely from a caller-provided query string.
If authentication or authorization remains unresolved, HALT without opening a
listener.

### STEP 2: Implement secured surface (contingent)

Expose only approved health and metric routes. Bind to the approved interface,
apply authentication/authorization before tenant data access, and use existing
`GetMetrics` semantics. Do not expose an all-tenant list unless an approved role
explicitly permits it.

### STEP 3: Test (contingent)

Cover unauthenticated rejection, unauthorized cross-tenant rejection, authorized
tenant output, approved elevated visibility if any, unknown-tenant behavior,
content type, listener binding, and shutdown. Verify no credential or tenant data
is written to logs.

```bash
go build ./internal/relay/ ./cmd/relay/
go test -race ./internal/relay/
go test ./cmd/relay/
```

---

## ACCEPTANCE CRITERIA

| # | Criterion | Pass Condition |
|---|-----------|----------------|
| 1 | Security contract approved | Authn/authz/bind/tenant visibility explicit |
| 2 | Metric contract approved | Names, units, format, and persistence boundary explicit |
| 3 | No tenant leakage | Cross-tenant and unauthenticated tests reject |
| 4 | Existing accounting reused | No unapproved counters or billing export |
| 5 | Shutdown and logs safe | Tests pass; no sensitive material logged |

---

## ROLLBACK PROCEDURE

Remove every newly created operator-server/test file with `git rm -f`; restore
only pre-existing `cmd/relay` files modified in the contingent implementation.
Remove deployed listener configuration before source rollback.

---

## BLOCKERS / DEFERRED

- ~~RELAY-03 must freeze byte-accounting ownership and units.~~ RESOLVED —
  RELAY-03 shipped; `RecordBytes` per-frame accounting is frozen.
- ~~Operator authentication/authorization and tenant visibility need approval.~~ RESOLVED —
  mTLS + role SANs + tiered visibility shipped in this sprint.
- Durable billing export remains a separate decision (out of scope for RELAY-04).

---

**Created:** 2026-08-23
**Version:** 1.2

---

## Closeout Note

RELAY-04 shipped. All three execution steps completed:

1. **Operator API contract approved** — `docs/design/RELAY_04_OPERATOR_API_ADR.md`
   - D.1: Separate admin listener, loopback-only default (`127.0.0.1:9090`)
   - D.2: mTLS with operator role SAN, reusing Ed25519 CA
   - D.3: Tiered visibility — `relay-admin` sees all tenants; `relay-operator` sees only bound tenants
2. **Secured surface implemented** — `internal/relay/admin.go`
   - `/admin/health` — liveness, uptime, active connections, pending legs
   - `/admin/metrics` — tenant-scoped metrics with role-based visibility enforcement
   - `operatorIdentity()` — extracts role/tenant SANs from client cert
   - `AdminTLSConfig()` — fail-closed mTLS config builder
   - `AllMetrics()` on RelayService — read-only snapshot of all tenant accounting
   - `PendingLegCount()` on MatchEngine — pending leg gauge for health
3. **Tests pass** — 10 admin-specific tests covering:
   - Unauthenticated rejection (health + metrics)
   - Unrecognized role → 403
   - Admin sees all tenants
   - Admin filtered by ?tenant= query
   - Operator sees only bound tenants
   - Operator cross-tenant query → 403
   - Config tests: admin-addr default, adminTLSConfig fail-closed, mTLS enforcement

Acceptance criteria verified:
- [x] Security contract approved (mTLS + role SAN + tiered visibility)
- [x] Metric contract approved (existing RelayMetrics, no new counters)
- [x] No tenant leakage (cross-tenant and unauthenticated tests reject)
- [x] Existing accounting reused (AllMetrics reads from RelayMetrics)
- [x] Shutdown and logs safe (no sensitive material logged)
