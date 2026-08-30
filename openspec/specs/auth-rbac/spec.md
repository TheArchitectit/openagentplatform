# Auth & RBAC

> **Phase:** 0 (Foundation) / 2 (A2A + Agents)
> **STATUS: COMPLETE** (§14 first-boot bootstrap implemented 2026-08-24 and
> verified green — see App Path and §14 for the surface)
> **Source:** `docs/architecture/ENDPOINT_API.md` §2, §9; `docs/architecture/A2A_PROTOCOL.md` §7;
> §14 from AI04_DEPLOY_NOTES §5.1 (first-admin gap)
> **App Path:** `internal/auth/middleware.go`, `internal/auth/oidc.go`;
> §14: `internal/api/routes_bootstrap.go`, `internal/bootstrap/store.go`,
> `internal/db/migrations/004_first_boot_bootstrap.up.sql`

---

## Description

Auth & RBAC is the identity and authorization boundary for every entrance to the
platform: the REST API, the gRPC services, the A2A gateway, the NATS bus, and
the endpoint agents themselves.

Four credential types coexist because they serve genuinely different callers.
Humans log in through OIDC. Automation uses API keys. Services authenticate to
each other with mTLS and SPIFFE identities. Endpoint agents carry
short-lived JWTs issued at enrollment. Each has a different lifetime, rotation
story, and revocation path.

Authorization is enforced server-side on every request. The UI hiding a button
is a usability affordance, never a security control — a caller with a valid token
and insufficient scope must be rejected by the API regardless of what client
they use.

Rate limiting is included here because it defends the auth surface itself:
login and enrollment endpoints get the tightest buckets, since those are what
attackers actually target.

## User Story

**As** a security-conscious platform administrator,
**I want** users authenticating through my corporate IdP, automation using
scoped revocable API keys, services proving identity with certificates, and
every request authorized server-side,
**so that** access is traceable to a principal, revocation is immediate, and a
compromised credential has a bounded blast radius.

---

## Requirements

### 1. OIDC Authentication

1.1. OIDC login MUST be supported, with Dex as the test IdP.

1.2. The flow MUST be: user redirected to IdP → authenticates → redirected back
with a code → platform exchanges code for tokens → platform issues its own JWT.

1.3. The API MUST return the authenticated user's identity for a valid session.

1.4. ID token validation MUST verify signature, issuer, audience, expiry, and
nonce.

1.5. OIDC MUST be configurable per deployment, and MUST NOT require the test IdP
in production.

### 2. Bearer Token (JWT) Authentication

2.1. Access tokens MUST be JWTs signed with RS256 and validated against JWKS.

2.2. Validation MUST verify signature, expiry, issuer, and audience. A token
failing any check MUST be rejected with `401`.

2.3. Validated claims MUST be placed in the request context: subject, scopes,
and tenant ID.

2.4. JWKS key material MUST be cached with refresh, so key rotation does not
require a restart and JWKS availability is not a per-request dependency.

2.5. Tokens MUST NOT be accepted from query parameters or request bodies —
only the `Authorization: Bearer` header (and gRPC `authorization` metadata).

### 3. Token Lifecycle Endpoints

3.1. Three auth endpoints MUST be provided:

| Method | Path | Purpose | Auth |
|--------|------|---------|------|
| POST | `/api/v1/auth/login` | Exchange credentials for access + refresh tokens | None |
| POST | `/api/v1/auth/refresh` | Exchange refresh token for new access token | Refresh JWT |
| POST | `/api/v1/auth/logout` | Invalidate the current session | Access JWT |

3.2. The login response MUST include `access_token`, `refresh_token`,
`token_type`, `expires_in`, `refresh_expires_in`, and granted `scope`.

3.3. Default lifetimes MUST be 3600s for access tokens and 86400s for refresh
tokens.

3.4. Logout MUST invalidate the session server-side. Client-side token discard
alone MUST NOT be considered logout.

3.5. Login MUST NOT reveal whether a username exists; invalid credentials and
unknown users MUST produce identical responses.

### 4. API Keys

4.1. API keys MUST be supported for automation and CLI access.

4.2. Keys MUST be stored hashed, never in plaintext, and MUST be displayed to
the user exactly once at creation.

4.3. Each key MUST carry explicit scopes, and MUST be independently revocable.

4.4. Keys MUST support optional expiry.

4.5. Key usage MUST be audited, and last-used time MUST be recorded so stale
keys can be identified.

### 5. mTLS and Service Identity

5.1. Service-to-service calls and NATS connections MUST authenticate via mTLS.

5.2. The SPIFFE ID MUST be extracted from the client certificate and the trust
domain MUST be validated.

5.3. A structurally valid certificate from an untrusted trust domain MUST be
rejected.

5.4. Certificate validation MUST include expiry and revocation checking.

### 6. OAuth 2.1 for Third Parties

6.1. Third-party integrations MUST authenticate via OAuth 2.1.

6.2. The authorization code flow with PKCE MUST be required.

6.3. DPoP sender-constrained tokens MUST be supported.

6.4. RFC 8707 resource indicators MUST be supported, so a token minted for one
resource cannot be replayed against another.

### 7. Agent Enrollment Credentials

7.1. Endpoint agents MUST enroll using a single-purpose enrollment token.

7.2. On successful registration, the agent MUST receive a JWT with a bounded
expiry.

7.3. Agent JWTs MUST be refreshable via the heartbeat response, so rotation does
not require re-enrollment.

7.4. An agent JWT MUST authorize actions only for that agent — an agent
presenting a valid token MUST NOT be able to act on another agent's behalf.

### 8. Role-Based Access Control

8.1. Permissions MUST be scope-based, expressed as
`resource:action` (for example `agents:read`, `checks:write`,
`scripts:execute`, `events:read`).

8.2. Roles MUST be assignable to users, and effective permissions MUST be the
union of assigned roles' scopes.

8.3. Every endpoint MUST declare its required scope, and the requirement MUST be
enforced server-side.

8.4. A request with valid authentication but insufficient scope MUST return
`403`, distinct from the `401` returned for failed authentication.

8.5. Authorization MUST be enforced in the API layer. UI-level hiding of
controls MUST NOT be treated as an access control.

8.6. Denials MUST NOT leak resource existence — an unauthorized caller MUST NOT
be able to distinguish "forbidden" from "not found" for resources outside their
scope.

### 9. Multi-Tenant Isolation

9.1. The tenant ID MUST be carried in token claims and applied to every
data-access path.

9.2. Cross-tenant access MUST be impossible via the API, including through
filter, sort, and ID-guessing parameters.

9.3. Every query path MUST have an integration test asserting tenant isolation,
per risk R7 (multi-tenant data leak via query isolation failure).

### 10. Middleware Chain

10.1. HTTP middleware MUST execute in this order: Recovery → RequestID →
StructuredLog → CORS → RateLimit → Auth → Handler.

10.2. Rate limiting MUST precede authentication, so unauthenticated floods are
shed before signature verification cost is incurred.

10.3. Recovery MUST be outermost, returning `500` with a trace ID and never
leaking a stack trace to the client.

10.4. gRPC interceptors MUST run: Auth → Logging → Recovery.

### 11. Rate Limiting

11.1. Rate limiting MUST use a token bucket algorithm keyed on the JWT `sub`
claim; unauthenticated requests MUST be keyed by IP.

11.2. Bucket state MUST be replicated to Redis for multi-instance deployments,
so a caller cannot exceed limits by spreading requests across instances.

11.3. These limits MUST be enforced on the auth and enrollment surface:

| Endpoint | Limit (req/min) | Burst | Rationale |
|----------|-----------------|-------|-----------|
| `POST /api/v1/auth/login` | 10 | 3 | Prevent brute force |
| `POST /api/v1/auth/refresh` | 60 | 10 | Frequent, low cost |
| `POST /api/v1/auth/logout` | 30 | 5 | Infrequent |
| `POST /api/v1/agents/register` | 10 | 5 | Prevent enrollment storms |
| `POST /api/v1/scripts/{id}/execute` | 30 | 5 | Most expensive operation |
| Default | 100 | 20 | Conservative |

11.4. Responses MUST include `X-RateLimit-Limit`, `X-RateLimit-Remaining`, and
`X-RateLimit-Reset`.

11.5. Exceeded limits MUST return `429` with a `Retry-After` header and a
structured error body.

### 12. Audit Logging

12.1. Logins, logouts, API calls, permission denials, and agent actions MUST
produce audit records.

12.2. Audit records MUST capture principal, action, target resource, timestamp,
source IP, and outcome.

12.3. Audit records MUST NOT contain credentials, tokens, or secret values.

12.4. Audit logs MUST be append-only from the application's perspective.

### 13. Security Verification

13.1. Security review MUST specifically cover auth bypass, token revocation
effectiveness, and certificate validation.

13.2. OWASP ZAP MUST run against authenticated and unauthenticated surfaces in
CI.

13.3. Tests MUST assert the negative cases — expired token rejected, wrong
audience rejected, insufficient scope rejected, cross-tenant access rejected —
not only that valid credentials succeed.

### 14. First-Boot Organization Bootstrap

Motivation: the identity layer is external (OIDC/dex) while tenancy is
internal (`tenants`, org-scoped via TEXT `org_id`). On a fresh database no
org exists and no IdP subject is bound to one, so every org-scoped route
returns `400 {"error":"org context required"}` — including for the first
administrator. (Observed on the 2026-08-30 ai04 deploy; AI04_DEPLOY_NOTES
§5.1. dex static users emit no groups/org claims, but the same gap applies
to any IdP whose users carry no org claim.)

14.1. `POST /api/v1/auth/bootstrap` MUST be provided, taking
`{"token", "org_name", "org_slug"}`. A correct token MUST create exactly one
org (a `tenants` row — inserted in the same transaction as the binding and
latch, which rules out calling `tenancy.TenantStore` on its own DB handle;
same column semantics as `TenantStore.Create`), record the caller's IdP subject
(from a valid session cookie/bearer token — the caller MUST be authenticated
first) as the org's admin in a `user_org_bindings` table
(`subject TEXT PRIMARY KEY, org_id TEXT NOT NULL, role TEXT NOT NULL,
created_at` — `org_id` TEXT matching every other org column in 001, no FK,
application-level integrity), set `bootstrap_complete` in a single-row
`app_state` control table, and respond with the created org. An incorrect
token MUST respond `401`; the endpoint MUST be rate-limited as an auth
surface (§11 default bucket applies until a dedicated limit is warranted).

14.2. The bootstrap token MUST come from `BOOTSTRAP_TOKEN` (env). An empty
value MUST disable the endpoint entirely (`404 {"error":"bootstrap_disabled"}`)
— a deployment that manages orgs out-of-band never has it open. The token
MUST be compared in constant time, MUST NOT be logged or returned by any
endpoint, and MUST NOT be committed to the repo (`.env.example` documents it
with an empty placeholder).

14.3. Once `bootstrap_complete` is set, the endpoint MUST respond
`403 {"error":"already_initialized"}` for any further call, permanently,
regardless of token value. Re-running MUST NOT duplicate orgs.

14.4. Org resolution at login (`handleCallback`) MUST follow, in order:
(a) an explicit `org_id` claim on the ID token; (b) a `user_org_bindings`
row for the subject; (c) if exactly one org exists and the subject has no
binding, auto-bind the subject to it with the groups-mapped role — the
single-org beta convenience that makes post-bootstrap logins work for every
IdP user without per-user provisioning; (d) otherwise mint a session with an
empty `OrgID` (org-scoped routes keep returning 400; multi-org deployments
must bind users out-of-band via `user_org_bindings`, e.g. through IdP org
claims in (a)).

14.5. Sessions MUST carry `org_id` (already on `SessionClaims`); the role
MUST remain groups-mapped (`MapGroupsToRole`) but bootstrap and auto-bind
rows may store an explicit role (the first admin gets `admin` even when the
IdP emits no groups — static dex users cannot be in `oap-admins`).

14.6. Both the bootstrap success and every rejection MUST produce audit
events (§12) with the caller principal, outcome, and org slug (never the
token).

14.7. The `user_org_bindings` and `app_state` tables MUST ship as a new
canonical migration (`internal/db/migrations/004_*`, applied at boot like
any other — the runner makes a fresh DB bootstrap-ready with no manual
step).
