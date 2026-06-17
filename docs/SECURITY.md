# Security

Security model, controls, and vulnerability reporting for OpenAgentPlatform.

## Table of contents

1. [Authentication flow](#authentication-flow)
2. [Authorization (RBAC)](#authorization-rbac)
3. [Secret management](#secret-management)
4. [Audit logging](#audit-logging)
5. [Network security](#network-security)
6. [Vulnerability reporting](#vulnerability-reporting)
7. [Security checklist](#security-checklist)

---

## Authentication flow

OpenAgentPlatform uses OIDC (OpenID Connect) for user authentication and
mTLS for agent authentication.

### User authentication (OIDC)

```
┌────────┐     1. Login       ┌────────┐    2. Auth req    ┌──────┐
│ Browser│ ─────────────────▶ │ OAP    │ ────────────────▶│ Dex  │
│        │                    │ Web UI │                   │ OIDC │
│        │◀──── 3. Redirect ──│        │◀── 4. Code ───────│      │
│        │                    │        │                   └──────┘
│        │  5. Exchange code  │        │
│        │ ─────────────────▶ │        │
│        │                    │        │  6. Verify token
│        │                    │        │ ─────────────────▶ Dex
│        │                    │        │◀── 7. ID token ───
│        │  8. Session cookie │        │
│        │◀────────────────── │        │
└────────┘                    └────────┘
```

Steps:

1. User clicks "Sign In" in the web UI.
2. Web UI redirects to the configured OIDC provider (Dex by default;
   can be Auth0, Okta, Google, etc.).
3. User authenticates with the provider.
4. Provider redirects back to the OAP callback with an authorization code.
5. OAP server exchanges the code for ID + access tokens.
6. OAP server verifies the ID token signature and claims.
7. OAP server creates a session (signed JWT cookie).
8. Session cookie is set; subsequent requests carry the cookie.

### Session management

- Session cookies are HTTP-only, `SameSite=Lax` (or `Strict` in production)
- `COOKIE_SECURE=true` in production enforces HTTPS-only cookies
- Sessions expire after `SESSION_TIMEOUT` hours (default 24)
- JWT secret (`JWT_SECRET`) must be a 32+ byte random value:
  `openssl rand -hex 32`

### Agent authentication (mTLS)

Each agent generates a unique mTLS client certificate on first
registration. The certificate:

- Is signed by the OAP internal CA
- Has CN = agent ID
- Is valid for 90 days (auto-renewed at 60 days)
- Is stored in the agent's local keystore with a passphrase

Agents connect to NATS using mTLS. The server validates the client cert
against the OAP CA before allowing subscriptions or publishes.

### Multi-factor authentication

OAP does not implement MFA directly -- MFA is delegated to the OIDC
provider. To enable MFA:

- **Dex**: configure a connector that supports MFA (e.g. LDAP with MFA)
- **Auth0/Okta**: enable MFA in the provider dashboard
- The OAP session inherits the provider's MFA assurance

---

## Authorization (RBAC)

OpenAgentPlatform implements role-based access control (RBAC) with three
built-in roles.

### Roles

| Role     | Description                         | Typical use            |
|----------|-------------------------------------|------------------------|
| `admin`  | Full access; manages users, billing | Org administrators     |
| `operator` | Manage agents, checks, alerts, scripts | Day-to-day operations |
| `viewer`  | Read-only access to dashboards      | Stakeholders, auditors |

### Permission matrix

| Resource            | admin | operator | viewer |
|---------------------|-------|----------|--------|
| Users               | CRUD  | R        | R      |
| Sites               | CRUD  | CRUD     | R      |
| Agents              | CRUD  | CRUD     | R      |
| Checks              | CRUD  | CRUD     | R      |
| Alerts              | CRUD  | CRU      | R      |
| Scripts             | CRUD  | CRUD     | R      |
| Patches             | CRUD  | CRU      | R      |
| Policies            | CRUD  | R        | R      |
| Secrets             | CRUD  | R        | -      |
| Remote Shell        | C     | C        | -      |
| Audit log           | R     | R        | R      |
| Billing             | CRUD  | R        | -      |
| LLM Agents          | CRUD  | C        | -      |

Legend: C=create, R=read, U=update, D=delete.

### Implementation

Authorization is enforced in middleware:

```go
// Pseudocode
func RequireRole(roles ...Role) Middleware {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            user := UserFromContext(r.Context())
            if !user.HasAnyRole(roles...) {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Routes are wrapped with role requirements:

```go
r.With(RequireRole(RoleAdmin)).Post("/api/v1/users", CreateUser)
r.With(RequireRole(RoleOperator, RoleAdmin)).Post("/api/v1/checks", CreateCheck)
r.With(RequireRole(RoleViewer, RoleOperator, RoleAdmin)).Get("/api/v1/agents", ListAgents)
```

### Organization isolation

All resources have an `org_id` column. The OAP server filters all queries
by the user's `org_id`. Cross-org access is rejected at the middleware
level.

### Custom roles (Enterprise)

Enterprise customers can define custom roles with granular permissions.
See [COMMERCIAL.md](COMMERCIAL.md).

---

## Secret management

OAP includes a built-in secret vault for storing credentials, API keys,
and certificates used by agents and integrations.

### Envelope encryption

```
┌─────────────────────────────────────────────────────────────┐
│                     ENVELOPE ENCRYPTION                      │
│                                                              │
│   ┌──────────────┐                                           │
│   │  Plaintext   │                                           │
│   │  secret      │                                           │
│   └──────┬───────┘                                           │
│          │ encrypt with                                      │
│          ▼                                                   │
│   ┌──────────────┐                                           │
│   │  Data Key    │  (random, per-secret)                     │
│   │  (AES-256)   │                                           │
│   └──────┬───────┘                                           │
│          │ wrap with                                         │
│          ▼                                                   │
│   ┌──────────────┐                                           │
│   │ Master Key   │  (from KMS or local secret)               │
│   │              │                                           │
│   └──────────────┘                                           │
└─────────────────────────────────────────────────────────────┘
```

- **Master key**: stored in an environment variable, file, or KMS
  (AWS KMS, GCP KMS, HashiCorp Vault)
- **Data key**: random AES-256 key generated per secret, encrypted by
  the master key and stored alongside the ciphertext
- **Ciphertext**: stored in the `secrets` table

### Secret access

Agents request secrets via NATS:

```
oap.secrets.<agent_id>.get
```

The server:

1. Authenticates the agent (mTLS)
2. Checks the agent's authorized secrets (ACL)
3. Decrypts the secret server-side
4. Sends it over the mTLS channel (never persisted to disk on the agent)

### Secret rotation

Rotate via the API:

```bash
curl -X POST https://oap.example.com/api/v1/secrets/<id>/rotate \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"value":"new-secret-value"}'
```

Rotation can be scheduled (e.g. every 90 days) or triggered manually.
Agents receive the new value on their next request.

### Best practices

- Never commit secrets to git
- Use environment variables for the master key in production
- Enable KMS-backed key management for production deployments
- Rotate secrets on a regular schedule
- Audit all secret access (see Audit logging below)
- Limit secret access to the minimum required agents

---

## Audit logging

Every mutating action and every secret access is recorded in an
append-only audit log.

### What is logged

| Event type            | Fields                                            |
|-----------------------|---------------------------------------------------|
| `user.login`          | user_id, ip, user_agent, timestamp                |
| `user.logout`         | user_id, timestamp                                |
| `agent.register`      | agent_id, hostname, ip, cert_fingerprint          |
| `agent.deregister`    | agent_id, reason                                  |
| `check.create`        | user_id, check_id, config                         |
| `check.update`        | user_id, check_id, changes                        |
| `check.delete`        | user_id, check_id                                 |
| `alert.acknowledge`   | user_id, alert_id                                 |
| `alert.resolve`       | user_id, alert_id, resolution                     |
| `script.create`       | user_id, script_id, content_hash                  |
| `script.execute`      | user_id, script_id, agent_id, exit_code           |
| `patch.approve`       | user_id, patch_id, approval_notes                 |
| `patch.apply`         | user_id, patch_id, agent_id, status               |
| `secret.read`         | user_id, secret_id, agent_id                      |
| `secret.rotate`       | user_id, secret_id                                |
| `shell.session.start` | user_id, agent_id, session_id                     |
| `shell.session.end`   | session_id, duration, bytes_transferred           |
| `policy.violation`    | agent_id, policy_id, details                      |
| `llm.invoke`          | user_id, adapter_id, model, tokens, cost          |

### Storage

Audit events are stored in the `audit_events` table (append-only, no
UPDATE/DELETE permissions granted to the application user).

### Retention

- Default: 365 days
- Configurable via `AUDIT_RETENTION_DAYS` environment variable
- Older events can be exported to cold storage (S3, GCS) for compliance

### Querying

```bash
# All events for a user in the last 7 days
curl "https://oap.example.com/api/v1/audit?user_id=u123&since=2026-06-10" \
  -H "Authorization: Bearer $TOKEN"

# All secret access events
curl "https://oap.example.com/api/v1/audit?event=secret.*" \
  -H "Authorization: Bearer $TOKEN"
```

### SIEM integration

Audit events can be streamed to a SIEM via webhook. Configure:

```bash
AUDIT_WEBHOOK_URL=https://siem.example.com/ingest/oap
AUDIT_WEBHOOK_SECRET=shared-secret
```

Events are POSTed as JSON with an HMAC-SHA256 signature header.

---

## Network security

### TLS

- All HTTP traffic uses TLS 1.2+ (TLS 1.3 recommended)
- Strong cipher suites only (no RC4, 3DES, or export-grade ciphers)
- HSTS enabled with `max-age=31536000; includeSubDomains; preload`
- Certificate transparency monitoring via certspotter or similar

### mTLS for NATS

- Per-agent client certificates signed by the OAP internal CA
- Server certificate signed by the OAP internal CA
- Mutual authentication required for all NATS connections
- Certificate revocation list (CRL) checked on every connection

### Network segmentation

Recommended production topology:

```
┌──────────────────────────────────────────────────────┐
│  Public subnet (DMZ)                                │
│  ┌────────────┐  ┌────────────┐                     │
│  │  Nginx /   │  │  NATS      │                      │
│  │  Caddy     │  │  (public)  │                      │
│  │  (TLS)     │  │            │                      │
│  └────────────┘  └────────────┘                     │
└──────────────────┬───────────────────────────────────┘
                   │ private subnet
┌──────────────────▼───────────────────────────────────┐
│  Private subnet                                      │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐      │
│  │  OAP       │  │  OAP       │  │  Postgres  │      │
│  │  server    │  │  web       │  │  +TSDB     │      │
│  └────────────┘  └────────────┘  └────────────┘      │
└──────────────────────────────────────────────────────┘
```

- Public subnet: reverse proxy, NATS (agents connect from outside)
- Private subnet: OAP server, web UI, Postgres (no public access)
- Security groups / firewall rules restrict inter-service traffic

### Rate limiting

All public endpoints are rate-limited:

- 100 req/min per IP for unauthenticated endpoints
- 1000 req/min per user for authenticated endpoints
- 10 req/min per IP for login attempts
- Configurable via `RATE_LIMIT_*` environment variables

### CORS

CORS is configured via:

```bash
CORS_ALLOWED_ORIGINS=https://your-domain.com
CORS_ALLOWED_METHODS=GET,POST,PATCH,DELETE
CORS_ALLOWED_HEADERS=Authorization,Content-Type
```

Wildcard origins are rejected in production.

### CSRF protection

- SameSite=Lax cookies prevent most CSRF attacks
- Double-submit cookie pattern for state-changing requests
- Origin/Referer header validation on all POST/PATCH/DELETE requests

---

## Vulnerability reporting

We take security vulnerabilities seriously. If you discover a security
issue, please report it responsibly.

### How to report

**Email:** security@openagentplatform.io

**GPG key:** Available at https://openagentplatform.io/.well-known/security.txt

**Response SLA:** Initial acknowledgment within 48 hours.

### What to include

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Potential impact
- Any known workarounds

### What to expect

1. **48 hours**: Initial acknowledgment
2. **7 days**: Triage and severity assessment
3. **30 days**: Target fix timeline for critical/high severity
4. **90 days**: Public disclosure (coordinated with reporter)


### Safe harbor

We will not pursue legal action against researchers who:
- Make a good-faith effort to avoid privacy violations
- Only interact with accounts they own or have explicit permission to access
- Stop testing immediately if they encounter user data
- Report vulnerabilities through the channel above before public disclosure

### Hall of fame

We recognize security researchers who report valid vulnerabilities. See
https://openagentplatform.io/security/hall-of-fame for the current list.

### Bug bounty

A bug bounty program is available for high and critical severity
findings. Contact security@openagentplatform.io for details.

---

## Security checklist

Before deploying to production, verify:

### Authentication

- [ ] `JWT_SECRET` is a 32+ byte random value (not the default)
- [ ] `COOKIE_SECURE=true`
- [ ] `COOKIE_SAMESITE=strict` (or `lax`)
- [ ] OIDC provider is configured with MFA enforcement
- [ ] Default Dex users are removed

### Database

- [ ] `POSTGRES_PASSWORD` is a strong, unique value
- [ ] `DB_SSLMODE=verify-full`
- [ ] Database is not exposed on a public interface
- [ ] Automated daily backups are configured
- [ ] Backup restoration is tested quarterly

### Network

- [ ] TLS is enabled on all public endpoints
- [ ] HSTS is enabled
- [ ] mTLS is enabled for NATS
- [ ] CORS is configured with explicit origins
- [ ] Rate limiting is enabled
- [ ] Firewall rules restrict inter-service traffic

### Secrets

- [ ] Master key is stored in a KMS or secure secret store
- [ ] Secret rotation schedule is defined
- [ ] Secret access is logged and monitored
- [ ] No secrets in git history

### Audit

- [ ] Audit log retention is configured
- [ ] SIEM integration is active
- [ ] Alerts are configured for suspicious activity
- [ ] Audit logs are backed up separately from the database

### Agents

- [ ] Agent mTLS certs are auto-renewed
- [ ] Agent cert fingerprints are verified on first connect
- [ ] Dormant agents (>30 days offline) are flagged for review

### Monitoring

- [ ] Prometheus alerts are configured for:
  - High error rate (>1% 5xx)
  - Unusual login patterns
  - Privilege escalation attempts
  - Secret access spikes
- [ ] Log aggregation is configured (Loki, ELK, etc.)
- [ ] Uptime monitoring is active

---

## Compliance considerations

OAP supports compliance with common frameworks:

- **SOC 2**: audit logging, RBAC, encryption at rest and in transit
- **HIPAA**: encryption, access controls, audit logging (not certified
  out of the box; deployment must be configured for HIPAA)
- **GDPR**: data export, user deletion, DPAs (Enterprise)
- **ISO 27001**: access controls, logging, encryption, IR (Enterprise)

Contact compliance@openagentplatform.io for documentation.

---

## Related

- [ARCHITECTURE.md](ARCHITECTURE.md) -- system design and security model
- [DEPLOYMENT.md](DEPLOYMENT.md) -- production deployment
- [COMMERCIAL.md](COMMERCIAL.md) -- tiers and SSO
- [API.md](API.md) -- API auth
