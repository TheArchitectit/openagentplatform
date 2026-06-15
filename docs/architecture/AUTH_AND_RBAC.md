# Auth & RBAC Architecture

> **Version:** 1.0.0 | **Last Updated:** 2026-06-15 | **Status:** Authoritative Blueprint

---

## 1. Overview

OpenAgentPlatform supports 6 authentication methods, each serving a different use case:

| Method | Use Case |
|--------|----------|
| JWT Bearer | API access for users and service accounts |
| mTLS/SPIFFE | Service-to-service, NATS, agent connections |
| OAuth 2.1 | Third-party app access |
| SAML 2.0 | Enterprise SSO (Professional tier) |
| OIDC | Enterprise SSO (Professional tier) |
| API Keys | Programmatic access |

---

## 2. JWT Bearer Auth

### Token Structure

**Access Token (15 min TTL):**
```json
{
  "header": { "alg": "RS256", "typ": "JWT", "kid": "oap-key-2026-01" },
  "payload": {
    "sub": "user-uuid",
    "tenant_id": "org-uuid",
    "roles": ["operator"],
    "scopes": ["agent:read", "agent:write", "check:read", "check:execute", "script:run"],
    "session_id": "session-uuid",
    "iat": 1718467200,
    "exp": 1718468100,
    "iss": "openagentplatform"
  }
}
```

**Refresh Token (30 day TTL):**
- Stored hashed (SHA-256) in `sessions` table
- Rotation: each refresh generates a new pair; reuse detection revokes entire token family
- RS256 signing with JWKS endpoint for key rotation

---

## 3. mTLS/SPIFFE

- Service-to-service identity via X.509 SVIDs
- SPIFFE ID format: `spiffe://openagentplatform.io/service/{service_name}`
- NATS connections: agent presents client cert; server validates trust domain
- SPIRE agent on each K8s node issues short-lived SVIDs

---

## 4. OAuth 2.1

- Authorization Code + PKCE (code_challenge_method: S256)
- DPoP binding: client proves possession of key pair via `cnf` claim
- RFC 8707 resource indicators: `resource=https://api.oap.example.com`
- Token introspection endpoint for resource server validation

---

## 5. SAML 2.0 (Professional Tier)

- SP-initiated SSO flow
- JIT provisioning: first login creates user account
- Group-to-role mapping: IdP groups → OAP roles (e.g., `oap-admins` → `super_user`)
- `samael` library for Python/Django SP implementation
- Single Logout (SLO) if IdP supports it

---

## 6. OIDC (Professional Tier)

- RP implementation via `coreos/go-oidc` (Go) / `mozilla-django-oidc` (Python)
- PKCE with nonce validation
- ID Token validation: `iss`, `aud`, `nonce`, `exp` checks
- UserInfo endpoint for additional profile data

---

## 7. API Keys

- SHA-256 hashed storage (never stored in plaintext)
- Scoped: each key has specific permissions
- Rotatable: new key generated, old key marked for expiry
- Last-used tracking: `last_used_at` timestamp for cleanup
- Revocable: instant invalidation via delete

---

## 8. RBAC Model

### 5 Built-In Roles

| Role | Scope | Key Permissions |
|------|-------|-----------------|
| `super_user` | Tenant-wide | All operations, user management, can assign any role |
| `manager` | Organization | User management, policy, deployment; cannot assign super_user |
| `operator` | Site | Agent management, script execution, alert handling |
| `technician` | Site (scoped) | Read + execute; no policy or user management |
| `read_only` | Site (scoped) | Read-only access to all resources |

### Scoping Hierarchy

```
Tenant > Organization > Client > Site > Agent
```

A technician assigned to Site A cannot see agents in Site B. An operator scoped to Client X sees all sites under Client X.

### Policy Decision Point (PDP)

Every API request flows through the PDP:

```
1. Extract subject (user + roles + scopes) from JWT/API key
2. Determine action (HTTP method + resource type)
3. Determine resource scope (tenant/org/client/site/agent)
4. Evaluate: subject roles ∩ action ∩ resource scope
5. Decision: Allow (with matched rules) or Deny
   Deny reasons: tenant_mismatch, no_role_grants, insufficient_scope, cannot_elevate_above_self
```

---

## 9. MFA

- **TOTP enrollment**: QR code generation, secret stored encrypted in `mfa_credentials` table
- **Backup codes**: 10 single-use codes, each stored hashed. Strict single-use enforcement via `last_used` tracking.
- **Enforcement**: Organization policy `require_mfa = true` gates all access until MFA enrolled
- **Recovery**: Admin can reset MFA for a user (audit-logged)

---

## 10. Session Management

| Setting | Default | Description |
|---------|---------|-------------|
| Max concurrent sessions per user | 5 | Oldest session evicted on 6th login |
| Idle timeout | 8 hours | No activity → session expires |
| Absolute timeout | 30 days | Hard expiry regardless of activity |
| Refresh token rotation | Enabled | New pair on each refresh; reuse → family revocation |
| Single Logout (SLO) | Via SAML | If IdP supports SLO |

---

## 11. Audit Log

### Hash-Chained Append-Only Log

Every API call, agent action, A2A task, and secret access is logged to an append-only audit log with Merkle-style hash chaining:

```
Record N: { action, actor, resource, timestamp, prev_hash, this_hash=H(N + prev_hash) }
```

### 13 AuditAction Values

`user.login`, `user.logout`, `user.create`, `user.update`, `user.delete`, `agent.register`, `agent.command`, `check.execute`, `script.run`, `secret.access`, `secret.rotate`, `policy.change`, `a2a.task.invoke`

### PII Redaction

Regex patterns for automatic redaction: passwords, tokens, SSNs, email local parts.

### Integrity Verification

Hourly job verifies hash chain for last 24h. Monthly partitioning for storage management.

---

## 12. SCIM 2.0 (Professional Tier)

Full RFC 7644 compliance for automated user provisioning from identity providers (Okta, Azure AD, etc.):

| Endpoint | Operation |
|----------|-----------|
| `/scim/v2/Users` | CRUD + List + Filter |
| `/scim/v2/Groups` | CRUD + List + Filter |
| `/scim/v2/ServiceProviderConfig` | Read |
| `/scim/v2/ResourceTypes` | Read |
| `/scim/v2/Schemas` | Read |

Filter parsing: `userName eq "alice@example.com"`, `groups.value eq "oap-admins"`. Bearer token auth for SCIM clients.

---

## 13. Database Schema (15 Tables)

Migrations 01-15:

| # | Table | Purpose |
|---|-------|---------|
| 01 | `tenants` | Multi-tenant isolation root |
| 02 | `users` | User accounts with hashed passwords |
| 03 | `organizations` | Org-level grouping within tenant |
| 04 | `clients` | MSP client organizations |
| 05 | `sites` | Sites within clients |
| 06 | `roles` | Role definitions (5 built-in) |
| 07 | `permissions` | Granular per-resource permissions |
| 08 | `role_permissions` | Role → Permission mapping |
| 09 | `user_roles` | User → Role + Scope assignment |
| 10 | `api_keys` | SHA-256 hashed API keys with scopes |
| 11 | `sessions` | Active user sessions |
| 12 | `mfa_credentials` | TOTP secrets (encrypted) + backup codes (hashed) |
| 13 | `sso_connections` | SAML/OIDC provider configurations |
| 14 | `scim_endpoints` | SCIM client registrations |
| 15 | `audit_events` | Hash-chained append-only audit log |

---

## 14. Implementation Steps

1. Tenants + Users models + JWT issuance/validation
2. Organizations + Clients + Sites models + scope resolution
3. Roles + Permissions + role_permissions + user_roles
4. RBAC Policy Decision Point (middleware)
5. API Keys (generation, hashing, scoped validation)
6. Sessions (Redis-backed, concurrent limits, rotation)
7. MFA (TOTP enrollment, backup codes, enforcement)
8. SAML 2.0 SSO (SP-initiated, JIT provisioning)
9. OIDC SSO (RP implementation, PKCE)
10. SCIM 2.0 (user/group provisioning)
11. Audit Log (hash-chained, PII redaction, partitioning)
12. Integration tests for every auth method + RBAC permissions
