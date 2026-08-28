# RELAY-04: Operator API Architecture Decision Record

**Date:** 2026-08-24
**Status:** APPROVED

---

## Decisions

### D.1 Admin Listener Binding

**Choice:** Separate admin listener on a distinct port, loopback-only by default.

- Default bind: `127.0.0.1:9090`
- Configurable via `--admin-addr` flag
- TLS required (same cert/key pair as WSS listener)
- No admin traffic on the WSS port; no path-prefix mixing

**Rationale:** Isolates operator traffic from data-plane WSS traffic. Loopback
default prevents accidental exposure. Separate port allows independent firewall
rules and monitoring.

---

### D.2 Operator Authentication

**Choice:** mTLS with operator role SAN, reusing the Ed25519 CA from RELAY-02.

Cert SAN conventions:
- `oap:role:relay-admin` — elevated access, all tenants
- `oap:role:relay-operator` — scoped access, tenant-bound
- `oap:tenant:<tenantID>` — tenant membership (required for operators)

The admin server extracts the client cert, validates the role SAN, and derives
tenant scope from SAN membership. No static bearer tokens.

**Rationale:** Reuses the existing PKI and rotation infrastructure. No additional
secret distribution. Role SAN is auditable and revocable via cert expiry.

---

### D.3 Tenant Visibility Scope

**Choice:** Tiered visibility based on cert role and tenant SANs.

| Role SAN | Tenant Visibility |
|----------|-------------------|
| `oap:role:relay-admin` | All tenants (no tenant SAN required) |
| `oap:role:relay-operator` | Only tenants listed in `oap:tenant:*` SANs |
| No recognized role | 403 Forbidden |

Mechanism:
1. Extract client cert from TLS handshake
2. Parse SANs for role and tenant entries
3. Admin role → allow all-tenant queries
4. Operator role → allow only queries for tenants in their SAN list
5. Cross-tenant query by operator → 403 Forbidden

**Rationale:** Prevents tenant leakage by default. Admin escalation is explicit
and auditable via cert. Operator scope is cryptographically bound.

---

## API Contract

### Routes

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/admin/health` | Any mTLS client | Liveness + readiness |
| GET | `/admin/metrics` | Role required | Metrics for permitted tenants |

### GET /admin/health

Response (200):
```json
{
  "status": "ok",
  "uptime_seconds": 3600,
  "active_connections": 42,
  "pending_legs": 3
}
```

No tenant data. Available to any mTLS-authenticated client (valid cert
from the relay CA is sufficient; no role required).

### GET /admin/metrics

Query parameters:
- `tenant` (optional): filter to a specific tenant ID

Behavior:
- No `tenant` param + admin role → return all tenants
- No `tenant` param + operator role → return only operator's permitted tenants
- `tenant=X` + admin role → return tenant X
- `tenant=X` + operator role → return tenant X only if X is in operator's SAN list
- `tenant=X` + operator role + X not permitted → 403 Forbidden

Response (200):
```json
{
  "tenants": [
    {
      "tenant_id": "acme",
      "connection_count": 10,
      "total_connections": 150,
      "bytes_relayed": 1048576
    }
  ]
}
```

All numeric values come from existing `RelayMetrics` and `RelayConnection`
accounting. No new counters or billing metrics are created.

### Error Responses

| Code | Condition |
|------|-----------|
| 401 | No valid client cert |
| 403 | No recognized role, or operator querying unpermitted tenant |
| 404 | Specified tenant has no metrics |

---

## Configuration

New flags in `cmd/relay`:

| Flag | Default | Description |
|------|---------|-------------|
| `--admin-addr` | `127.0.0.1:9090` | Admin listener bind address |
| `--admin-tls-cert` | (same as WSS) | TLS cert for admin listener |
| `--admin-tls-key` | (same as WSS) | TLS key for admin listener |

If admin cert/key flags are omitted, the WSS cert/key is reused.

---

## Security Invariants

1. Admin listener requires TLS + client cert (mTLS) on every route
2. Health endpoint requires a valid cert but no specific role
3. Metrics endpoint requires a recognized role SAN
4. Tenant data never crosses role boundaries
5. No credential or tenant data written to logs
6. No unauthenticated route exists on the admin listener
