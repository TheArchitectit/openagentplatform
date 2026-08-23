# Secret Management

> **Phase:** 3 (Secret Management)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/ROADMAP_AND_SPRINTS.md` §5 (Phase 3)
> **Current state:** Fully implemented — Vault, Infisical, DB, env, and memory
> backends plus URI resolver, injection, rotation, OAuth, and k8s CSI support
> (`secrets/` module: `vault/`, `infisical/`, `db_backend.go`, `env_backend.go`,
> `memory_backend.go`, `resolver/`, `inject/`, `rotation/`, `oauth/`, `k8s_csi.go`).



---

## Description

Secret Management provides one abstraction — `SecretBackend` — behind which any
number of concrete secret stores can sit: HashiCorp Vault, Infisical, a
database-backed store, environment variables, and a local file store. Callers
reference secrets by URI and never learn which backend resolved them.

The design goal is that a secret's *value* has the shortest possible lifetime
and the smallest possible blast radius. Configuration files and task payloads
carry only references (`vault://kv/data/prod/db#password`), not values.
Resolution happens as late as possible, and injection into a child process
happens by the least-exposed channel available.

This subsystem also owns the credentials the platform itself needs to federate:
EdDSA-signed JWTs for A2A agent-to-agent auth, and an MCP OAuth 2.1
authorization server for tool access.

The critical operational risk is Vault seal (R4): if the backend becomes
unavailable, every dependent operation fails at once. Caching with a grace
period and an alert on seal events are therefore requirements, not
optimizations.

## User Story

**As** a platform operator running agents that need database passwords, API
keys, and cloud credentials,
**I want** to store secrets in my existing vault and reference them by URI
everywhere else,
**so that** no plaintext credential is ever committed to a config file, written
to a log, or persisted in a task record — and so that rotating a secret in the
vault takes effect without redeploying anything.

---

## Requirements

### 1. SecretBackend Abstraction

1.1. A single `SecretBackend` interface MUST be defined, with five
implementations: HashiCorp Vault, Infisical, database store, environment
variables, and local file store.

1.2. The interface MUST cover: read a secret, write a secret, delete a secret,
list secrets under a path, and report backend health.

1.3. Callers MUST depend only on the interface. Adding a sixth backend MUST NOT
require changes to any caller.

1.4. Backends MUST be selectable per-secret-reference by URI scheme, so a single
deployment can read from Vault and Infisical simultaneously during migration.

### 2. HashiCorp Vault Integration

2.1. Three auth methods MUST be supported: AppRole, Kubernetes, and JWT.

2.2. Vault tokens MUST be renewed before expiry; token expiry MUST NOT cause a
resolution failure that a renewal would have prevented.

2.3. KV v2 versioned secrets MUST be supported, including reading a specific
version.

2.4. Vault namespaces MUST be supported for enterprise multi-tenant
deployments (open question O5).

2.5. A seal event MUST raise an alert immediately, and auto-unseal via
Kubernetes MUST be supported (risk R4 mitigation).

### 3. Infisical Integration

3.1. Infisical MUST be supported as a first-class backend with equivalent
read/write/list/delete semantics.

3.2. Infisical environments and folder paths MUST map cleanly onto the URI
scheme so references are portable.

3.3. Authentication MUST use Infisical machine identities rather than
long-lived personal tokens.

### 4. Secret Reference URI Pipeline

4.1. Secrets MUST be referenceable by URI rather than by value, in the form
`{scheme}://{path}#{key}` — for example
`vault://kv/data/prod/db#password`.

4.2. The resolution pipeline MUST: parse the URI, select the backend by scheme,
authenticate, fetch, extract the keyed field, and return the value.

4.3. Resolution MUST occur as late as possible — at point of use, not at
configuration load — so a value's in-memory lifetime is minimized.

4.4. Unresolvable references MUST fail loudly with a diagnostic naming the URI
and the failure reason. A reference MUST NEVER silently resolve to an empty
string, because empty credentials produce confusing downstream auth failures.

4.5. Resolved values MUST be cached with a bounded TTL, and the cache MUST
support a grace period so a brief backend outage does not immediately fail all
dependent operations.

4.6. Resolved values MUST NOT be written to logs, traces, error messages, task
metadata, or artifacts. Redaction MUST be applied at the logging boundary, not
left to individual call sites.

### 5. Credential Injection

5.1. Three injection mechanisms MUST be supported for child processes:
environment variables, file, and stdin.

5.2. File injection MUST write to a path with `0600` permissions and MUST delete
the file after the process exits, including on the error path.

5.3. Environment injection MUST scope variables to the child process only and
MUST NOT modify the parent's environment.

5.4. stdin injection MUST close the stream after writing, so the child does not
block waiting for more input.

5.5. Injected secrets MUST be zeroed in memory after use.

5.6. Core dump size MUST be set to 0 for processes receiving injected
credentials, per risk R8.

5.7. Injected credentials MUST NOT appear in the child process's command line,
because process listings are world-readable on most platforms.

### 6. A2A Auth Token Management

6.1. A2A agent-to-agent authentication tokens MUST be EdDSA-signed JWTs.

6.2. Signing keys MUST be stored in the secret backend, never in source or
configuration.

6.3. Tokens MUST carry a bounded expiry, and issuance MUST be auditable.

6.4. Key rotation MUST be supported without invalidating in-flight tasks:
verification MUST accept the previous key for a defined overlap window.

### 7. MCP OAuth 2.1 Server

7.1. An MCP OAuth 2.1 authorization server MUST be implemented.

7.2. It MUST support the authorization code flow with PKCE.

7.3. It MUST support DPoP sender-constrained tokens.

7.4. It MUST support RFC 8707 resource indicators, so a token issued for one
resource cannot be replayed against another.

7.5. Token revocation MUST take effect promptly and MUST be verified by
security testing.

### 8. Audit and Access Control

8.1. Every secret read MUST produce an audit record naming the accessing
principal, the reference, and the timestamp — but never the value.

8.2. Access to secret references MUST be governed by RBAC; a principal without
permission MUST be denied at resolution time, not merely hidden in the UI.

8.3. Failed resolution attempts MUST be audited, since repeated failures may
indicate credential probing.

### 9. Operational Requirements

9.1. Backend health MUST be exposed for monitoring, and backend unavailability
MUST alert.

9.2. A runbook MUST exist for backend unavailability, covering the cache grace
period and recovery.

9.3. `gitleaks` MUST run in CI to catch committed secrets.

9.4. Security review MUST cover token revocation, injection leakage paths, and
cache invalidation.
