# Secret Management Architecture

> **Version:** 1.0.0 | **Last Updated:** 2026-06-15 | **Status:** Authoritative Blueprint

---

## 1. Overview

In an RMM platform, credentials are everywhere — SSH keys for remote access, API tokens for cloud providers, database passwords for patch deployments, domain admin credentials for Windows updates. The core principle of OpenAgentPlatform's secret management:

> **NEVER store credentials in the primary database. Store only secret references (backend_type + path). Delegate to dedicated backends.**

This ensures credentials are never leaked via database backups, API responses, or log files. Secret backends provide audit logging, rotation, dynamic lease management, and access control.

**App Path:** `secrets/` (Go module)

---

## 2. Backend Abstraction

```go
type SecretBackend interface {
    Get(ctx context.Context, path string, version *int) (*SecretValue, error)
    Set(ctx context.Context, path string, data MapStr, opts SetOptions) (*SecretVersion, error)
    Delete(ctx context.Context, path string, opts DeleteOptions) error
    List(ctx context.Context, prefix string, opts ListOptions) ([]string, error)
    Metadata(ctx context.Context, path string) (*SecretMetadata, error)
    Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error)
    Healthcheck(ctx context.Context) bool
    Close(ctx context.Context) error
    SupportsDynamic() bool
}
```

5 implementations:

| Backend | Use Case | Dynamic Secrets | Auth Methods |
|---------|----------|----------------|--------------|
| `VaultBackend` | Production primary | ✅ Yes | AppRole, Kubernetes, JWT/OIDC, Token |
| `InfisicalBackend` | Production alternative | ❌ No | Universal Auth, Kubernetes |
| `K8sCSIBackend` | K8s-native workloads | ❌ No | ServiceAccount tokens |
| `EnvBackend` | Local dev | ❌ No | None (process env vars) |
| `MemoryBackend` | Testing only | ❌ No | None (in-process dict) |

---

## 3. Vault Backend

### 3.1 Authentication Methods

| Method | Use Case | How It Works |
|--------|----------|---------------|
| **AppRole** | Machine-to-machine (API server → Vault) | RoleID + SecretID exchanged for token |
| **Kubernetes** | Container-to-Vault | ServiceAccount JWT validated by Vault's K8s auth |
| **JWT/OIDC** | Agent identity | Agent presents signed JWT; Vault validates against OIDC provider |
| **Token** | Manual/admin | Direct Vault token (debugging only) |

### 3.2 KV v2 Operations

- Read/write with versioning and rollback (`check_version` for optimistic concurrency)
- Path template: `openagentplatform/{{client_id}}/{{site_id}}/{{agent_id}}/credentials`

### 3.3 Dynamic Secrets

- `read_dynamic()`: Vault generates short-lived DB credentials or cloud API tokens
- `renew_lease()`: Extends lease before expiry
- `revoke_dynamic()`: Revokes lease when RMM operation completes
- Background token renewal at `token_ttl * 0.7`

### 3.4 Policy-to-Hierarchy Mapping

```hcl
path "openagentplatform/data/{{client_id}}/*" {
  capabilities = ["read", "list"]
}
path "openagentplatform/data/{{client_id}}/{{site_id}}/*" {
  capabilities = ["read"]
}
```

### 3.5 Audit Logging

Vault's audit device captures all secret access. Request IDs from RMM operations propagated as Vault metadata for cross-referencing.

---

## 4. Infisical Backend

### 4.1 Authentication

| Method | How |
|--------|-----|
| **Universal Auth** | Client ID + Secret exchanged for access token |
| **Kubernetes** | ServiceAccount JWT validated by Infisical's K8s integration |

### 4.2 Path Mapping

OAP path `openagentplatform/acme/ny/agent-042/ssh_key` → Folder: `acme/ny`, Key: `agent-042_ssh_key`. Configurable folder prefix via `OAP_INFISICAL_FOLDER_PREFIX`. Auto token-refresh on 401.

### 4.3 Advantages over Vault

- Developer-friendly UI, native GitOps integration, simpler setup (no unseal ceremony)
- Folder inheritance maps naturally to Client > Site > Agent

---

## 5. Secret Reference Model

### URI Format

```
ref:oap://<backend_type>/<workspace_id>/<path>?version=<v>&key=<k>
```

**Examples:**
- `ref:oap://vault/prod/acme/ny/agent-042/ssh_key?version=5`
- `ref:oap://infisical/default/acme/ny/db_password?key=connection_string`

### Resolution Pipeline

1. Parse URI → extract backend_type, workspace, path, version, key
2. Look up backend from registry
3. Authorize: check Client>Site>Agent hierarchy
4. Fetch: call `backend.Get(path, version)`
5. If key specified: extract nested field
6. Audit: emit resolution event
7. Return SecretValue

Concurrent: `asyncio.gather` + `Semaphore(16)`. Cache: TTL-based LRU in Redis (5-15s for non-dynamic, no cache for dynamic).

---

## 6. Credential Injection Pipeline

### 3 Injection Methods

| Method | How | Use Case |
|--------|-----|----------|
| **env** | Write to agent's env namespace with `OAP_INJECTED_` prefix | API keys, connection strings |
| **file** | Write to temp path with mode 0600, owned by agent process UID | SSH keys, certificates |
| **stdin** | Pipe credential to agent stdin via Unix socket | One-time passwords |

### TTL Sweeper

Runs every 10s. Expired credentials: secure delete (zero-fill + unlink). Dynamic leases: revoke. Audit event emitted for each revocation.

---

## 7. A2A Auth Token Management

### EdDSA (Ed25519) JWTs

| Operation | Description |
|-----------|-------------|
| **Issue** | Sign JWT with Ed25519. Claims: `iss`, `sub`, `aud`, `jti`, `scopes`, `delegation_chain`, `exp`, `iat` |
| **Exchange** | Verify token, down-scope, extend delegation chain (max depth 3, TTL reduces 50% per hop) |
| **Verify** | Signature + exp/nbf + scope matching (wildcards: `patch:*` matches `patch:approve`) + revocation check |
| **Revoke** | Add `jti` to revocation list in Redis with TTL = token expiry. Emit audit event. |

---

## 8. Script Credential Safety

**The Problem:** RMM agents execute scripts as SYSTEM (no sandboxing in Tactical RMM). Passing secrets as script args or env vars exposes them in process listings, shell history, logs, and core dumps.

**The Solution:**

| Pattern | How | Why Safe |
|---------|-----|----------|
| Server-side authenticated operations | RMM server performs auth operations on behalf of script | Credentials never leave the server |
| JIT endpoint credential delivery | Credential in HTTP response body only, never script args | Not visible in process listings |
| Audit logging | Every credential fetch logged | Full traceability |
| No script-arg secrets | Secrets NEVER passed as arguments or endpoint env vars | No shell history exposure |

---

## 9. MCP OAuth 2.1 Integration

| Feature | RFC | Implementation |
|---------|-----|----------------|
| DPoP binding | RFC 9449 | Client proves possession of key pair via `cnf` claim |
| Dynamic Client Registration | RFC 7591 | MCP clients register dynamically |
| Protected Resource Metadata | RFC 9728 | `/.well-known/oauth-protected-resource` |
| Authorization Code + PKCE | RFC 7636 | Code challenge/verifier prevents interception |

---

## 10. Hierarchy-Based Access

Agent permissions follow Client > Site > Agent hierarchy:
- **Client-level agent**: can read `{client_id}/*`
- **Site-level agent**: can read `{client_id}/{site_id}/*`
- **Agent-level agent**: can read `{client_id}/{site_id}/{agent_id}/*`

Enforced via Vault policy templates, Infisical folder structure, and API layer scope validation.

---

## 11. API Endpoints (9 Route Groups)

| Route Group | Key Endpoints | Purpose |
|-------------|---------------|---------|
| `routes_secrets` | GET/PUT/DELETE per backend, GET metadata | CRUD on secrets |
| `routes_references` | POST /resolve, /validate, /batch-resolve | Resolve secret refs |
| `routes_rotation` | POST /{path}/rotate, GET /policies | Manual/auto rotation |
| `routes_injection` | POST /inject, /{id}/revoke, GET /active | Credential injection |
| `routes_a2a_tokens` | POST /issue, /exchange, /verify, /{id}/revoke | A2A auth tokens |
| `routes_mcp` | POST /oauth2/authorize, /token, GET /.well-known/* | MCP OAuth 2.1 |
| `routes_audit` | GET /audit, /audit/{id} | Secret access audit |
| `routes_hierarchy` | GET /tree, POST /grant, DELETE /revoke | Hierarchy permissions |
| `routes_migration` | POST /migrate, GET /status | Backend migration |

---

## 12. K8s Integration

Secrets Store CSI Driver with Vault and Infisical providers. `SecretProviderClass` CRD mounts secrets as files in pod tmpfs. Sync interval: 5min default. Pod ServiceAccount tokens used for backend auth. Filesystem-watch updates mounted secrets on rotation without pod restart.

---

## 13. Implementation Steps (10 Ordered)

| Step | Produces |
|------|----------|
| 1. Backend ABC + exceptions + data classes + tests | Interface and types |
| 2. MemoryBackend + EnvBackend (testing) | Test backends |
| 3. VaultBackend: init, auth (AppRole, K8s, JWT, Token), CRUD | Vault core |
| 4. VaultBackend: Rotate, dynamic secrets, lease, audit | Vault advanced |
| 5. InfisicalBackend: Universal Auth, K8s Auth, CRUD, mapping | Infisical |
| 6. K8sCSIBackend: SecretProviderClass integration | K8s CSI |
| 7. Secret Reference model: URI, resolution, caching | References |
| 8. Credential Injection Pipeline: env/file/stdin + TTL sweeper | Injection |
| 9. A2A Token Manager: issue, exchange, verify, revoke | A2A auth |
| 10. MCP OAuth 2.1 + API routes + integration tests + E2E | Full stack |

A Remote Monitoring and Management (RMM) platform like OpenAgentPlatform (OAP) routinely handles some of the most sensitive credentials in an organization's infrastructure: database passwords, cloud provider API keys, TLS private keys, SSH credentials for endpoint access, and OAuth tokens for third-party service integrations. A single credential leak can cascade into lateral compromise across every managed endpoint, every cloud tenant, and every customer environment.

The core principle of OAP's secret management subsystem is:

> **NEVER store credentials in the primary database. Only store secret references. Delegate to dedicated backends.**

The primary database stores structured records: agent heartbeats, task results, alert states, configuration. Mixing raw credentials into these tables creates an enormous attack surface — every database backup becomes a credential exfiltration vector, every read replica is a potential leak, every analytics export is a breach. Instead, OAP stores *references* (a backend type and a path), and resolves those references to actual secrets at the moment they are needed, via a dedicated secret backend.

This architecture provides:

- **Blast-radius reduction**: A database compromise yields paths, not passwords.
- **Backend portability**: Customers can choose Vault, Infisical, K8s CSI, or environment variables without code changes.
- **Audit clarity**: Every secret access is logged through the backend, not scattered across application logs.
- **Dynamic credentials**: Short-lived, automatically-rotated database and cloud credentials eliminate static-secret risk.
- **Separation of concerns**: The RMM core never needs to know how a secret is stored — only that it can be retrieved.

## Backend Abstraction

All secret backends implement a single Go interface, defined in `internal/secrets/backend.go`:

```go
type SecretBackend interface {
    // Get retrieves a secret value by path. Returns the value, metadata, and error.
    Get(ctx context.Context, path string, opts GetOptions) (*SecretValue, error)

    // Set writes a secret at the given path. May be a no-op for read-only backends.
    Set(ctx context.Context, path string, value *SecretValue, opts SetOptions) error

    // Delete removes a secret. May be a no-op for read-only backends.
    Delete(ctx context.Context, path string) error

    // List enumerates secret paths under a prefix.
    List(ctx context.Context, prefix string) ([]string, error)

    // Metadata returns backend-specific metadata (version, created time, lease info).
    Metadata(ctx context.Context, path string) (*SecretMetadata, error)

    // Rotate triggers backend-native rotation (Vault dynamic, Infisical auto-rotate, etc.).
    Rotate(ctx context.Context, path string) (*SecretMetadata, error)

    // Healthcheck verifies the backend is reachable and authenticated.
    Healthcheck(ctx context.Context) error

    // Close releases any held connections, leases, or tokens.
    Close() error

    // SupportsDynamic returns true if the backend can issue short-lived dynamic secrets.
    SupportsDynamic() bool
}
```

The interface is deliberately minimal — eight methods that cover the full lifecycle of a secret reference. Backend-specific complexity (Vault leases, Infisical folder hierarchies, K8s CSI file mounts) is hidden behind these methods.

### Method Semantics

| Method | Purpose | Error Semantics |
|--------|---------|-----------------|
| `Get` | Resolve a path to a value. May trigger lease acquisition for dynamic secrets. | Returns `ErrNotFound` for missing paths, `ErrPermissionDenied` for ACL failures, `ErrLeaseExpired` for dynamic secrets. |
| `Set` | Write a static secret. Backends that are read-only (K8s CSI, Env) return `ErrReadOnly`. | Returns `ErrPathInvalid` for malformed paths. |
| `Delete` | Remove a secret. Revokes all active leases. | Returns `ErrNotFound` if the path does not exist. |
| `List` | Enumerate paths under a prefix. Used for inventory and migration. | Returns empty slice for no matches, not an error. |
| `Metadata` | Return version, timestamps, lease TTL, and rotation history. | Returns `ErrNotFound` for missing paths. |
| `Rotate` | Trigger backend-native rotation. For Vault, this generates new dynamic credentials. For Infisical, it triggers auto-rotate if configured. | Returns the new metadata after rotation. |
| `Healthcheck` | Ping the backend. Used by the readiness probe and the circuit breaker. | Returns descriptive error with cause. |
| `Close` | Release resources. Flushes token renewers, closes HTTP connections, unmounts CSI volumes. | Best-effort; errors are logged, not returned. |
| `SupportsDynamic` | Capability flag. `true` for Vault (database/cloud secrets engines), `false` for K8s CSI, Env, Memory, and Infisical (unless using their dynamic secret feature). | N/A. |

### Backend Implementations

OAP ships with five backend implementations, each selected based on deployment context:

| Backend | Type | Use Case | Dynamic Support | Auth Method |
|---------|------|----------|-----------------|-------------|
| `VaultBackend` | `vault` | Production default. HashiCorp Vault with KV v2 and dynamic secrets. | Yes | AppRole, Kubernetes, JWT/OIDC, Token |
| `InfisicalBackend` | `infisical` | Cloud-native teams using Infisical for secrets. | Limited (requires Infisical dynamic secret feature) | Universal Auth, Kubernetes Auth |
| `K8sCSIBackend` | `k8s-csi` | Read-only secrets mounted via Secrets Store CSI Driver in Kubernetes. | No | CSI volume mount (no runtime auth) |
| `EnvBackend` | `env` | Development, CI/CD, and simple deployments using process environment variables. | No | None (reads `os.Getenv`) |
| `MemoryBackend` | `memory` | Testing only. Thread-safe in-memory map with no persistence. | No | None |

The backend to use is selected per-reference via the `backend_type` field in the secret reference URI. A single OAP deployment can simultaneously use multiple backends — for example, Vault for production customer credentials and Env for local development overrides.

## Vault Backend

The Vault backend is the production default. It wraps the official HashiCorp Vault Go client (`github.com/hashicorp/vault/api`) and provides OAP-specific conveniences for authentication, policy mapping, and lease management.

### Authentication Methods

Vault supports four authentication methods, configured at startup via `oapd.yaml`:

```yaml
secrets:
  backend: vault
  vault:
    address: https://vault.internal.oap.example.com:8200
    auth_method: approle          # approle | kubernetes | jwt | token
    role_id: "${VAULT_ROLE_ID}"
    secret_id: "${VAULT_SECRET_ID}"
    mount_path: secret            # KV v2 mount
    namespace: oap-prod            # Vault Enterprise namespace
    token_ttl: 1h
    token_max_ttl: 24h
```

#### 1. AppRole

AppRole is the recommended method for service-to-service authentication. OAP provides a `role_id` and `secret_id` at startup. The `secret_id` is obtained from a trusted orchestrator (Kubernetes secret, CI/CD vault, etc.) and is never hardcoded. The backend authenticates and receives a Vault token with the configured TTL and policies.

```go
func (v *VaultBackend) loginAppRole(ctx context.Context) (*api.Secret, error) {
    data := map[string]interface{}{
        "role_id":   v.config.RoleID,
        "secret_id": v.config.SecretID,
    }
    return v.client.Logical().WriteWithContext(ctx,
        "auth/approle/login", data)
}
```

#### 2. Kubernetes

For OAP deployments running inside Kubernetes, the Kubernetes auth method uses the pod's service account token to authenticate. This eliminates the need to distribute `secret_id` values:

```go
func (v *VaultBackend) loginKubernetes(ctx context.Context) (*api.Secret, error) {
    jwt, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
    if err != nil {
        return nil, fmt.Errorf("reading SA token: %w", err)
    }
    data := map[string]interface{}{
        "jwt":  string(jwt),
        "role": v.config.K8sRole,
    }
    return v.client.Logical().WriteWithContext(ctx,
        "auth/kubernetes/login", data)
}
```

#### 3. JWT/OIDC

OIDC authentication is used when OAP is deployed as part of a CI/CD pipeline or when it needs to exchange an external identity token (e.g., from a cloud IAM provider) for a Vault token. The backend reads the JWT from a configurable file path and exchanges it for a Vault token bound to the configured OIDC role.

#### 4. Token

For development or when Vault is running in dev mode, a static token can be provided directly. This method is **not recommended for production** and is blocked by the production guardrails.

### KV v2 and Versioning

The Vault backend uses KV v2 (the versioned key-value store). Every `Set` call creates a new version. The `Metadata` method returns the current version number, creation timestamp, and destruction status:

```go
type SecretMetadata struct {
    Version     int       `json:"version"`
    CreatedTime time.Time `json:"created_time"`
    Deleted     bool      `json:"deleted"`
    CustomMetadata map[string]string `json:"custom_metadata,omitempty"`
}
```

The backend supports version pinning via the `version` query parameter in the secret reference URI (e.g., `?version=3`). If no version is specified, the latest version is returned. Rollback is supported by calling `Set` with a previous version's data, which creates a new version with the old content.

### Dynamic Secrets

Vault's dynamic secrets engines generate short-lived credentials on demand. OAP supports the following dynamic secret types:

| Engine | Dynamic Credential Type | Typical Use Case |
|--------|------------------------|-----------------|
| `database/` | PostgreSQL/MySQL/MongoDB usernames and passwords | Agent-to-database connections with automatic password rotation |
| `aws/` | AWS access key + secret + session token | Cloud resource management with STS-bound sessions |
| `gcp/` | GCP service account keys | GCP project access with automatic key expiry |
| `azure/` | Azure service principal credentials | Azure resource access |
| `PKI` | X.509 certificates | Agent-to-server mTLS certificates |

When `SupportsDynamic` returns `true` and the reference path points to a dynamic secret mount, `Get` triggers credential generation and returns a lease:

```go
func (v *VaultBackend) Get(ctx context.Context, path string, opts GetOptions) (*SecretValue, error) {
    if opts.Dynamic {
        secret, err := v.client.Logical().ReadWithContext(ctx, path)
        if err != nil {
            return nil, err
        }
        leaseID := secret.LeaseID
        v.leaseMgr.Register(leaseID, path, secret.LeaseDuration)
        return &SecretValue{
            Data:     secret.Data,
            Metadata: extractMetadata(secret),
            LeaseID:  leaseID,
        }, nil
    }
    // ... static KV v2 path
}
```

### Lease Management

Dynamic secrets come with a lease (TTL). OAP's lease manager tracks all active leases and handles renewal and revocation:

```go
type LeaseManager struct {
    mu       sync.RWMutex
    leases   map[string]*Lease
    backend  SecretBackend
    renamer  *time.Timer
}

type Lease struct {
    ID       string
    Path     string
    TTL      time.Duration
    RenewAt  time.Time
    Revoked  bool
}
```

- **Renewal**: The lease manager runs a background goroutine that renews leases at 70% of their TTL. If renewal fails (e.g., the Vault token has expired), the lease is marked for revocation and the caller is notified.
- **Revocation**: When a secret is no longer needed (e.g., an agent disconnects), `Delete` or explicit revocation calls `v.client.Sys().Revoke(leaseID)`. The lease is removed from the manager.
- **Expiry**: If a lease expires without renewal, Vault automatically revokes the underlying credential. The next `Get` call will generate a new one.

### Audit Logging

Every Vault operation is logged to Vault's audit device (configured externally). OAP additionally logs a structured event for each resolution:

```go
log.Info("secret.resolved",
    "backend", "vault",
    "path", path,
    "version", metadata.Version,
    "lease_id", leaseID,
    "requester", opts.RequesterID,
    "duration_ms", time.Since(start).Milliseconds(),
)
```

### Policy-to-Hierarchy Mapping

OAP maps its organizational hierarchy (Client > Site > Agent) to Vault policies. Each reference path in OAP contains template variables that are substituted at policy-creation time:

| Template Variable | OAP Entity | Vault Policy Attribute |
|-------------------|------------|------------------------|
| `{{client_id}}` | Top-level customer organization | Path prefix `secret/data/clients/{{client_id}}/*` |
| `{{site_id}}` | Physical or logical site | Path prefix `secret/data/clients/{{client_id}}/sites/{{site_id}}/*` |
| `{{agent_id}}` | Individual managed agent | Path prefix `secret/data/clients/{{client_id}}/sites/{{site_id}}/agents/{{agent_id}}/*` |

Policies are generated by OAP at hierarchy-creation time and applied to Vault via the `sys/policy/acl/` API:

```go
func (v *VaultBackend) ApplyHierarchyPolicy(ctx context.Context, clientID, siteID, agentID string) error {
    policy := fmt.Sprintf(`
path "secret/data/clients/%s/*" {
  capabilities = ["read"]
}
path "secret/data/clients/%s/sites/%s/*" {
  capabilities = ["read", "list"]
}
path "secret/data/clients/%s/sites/%s/agents/%s/*" {
  capabilities = ["read", "list", "create", "update", "delete"]
}
`, clientID, clientID, siteID, clientID, siteID, agentID)

    return v.client.Sys().PutPolicyWithContext(ctx,
        fmt.Sprintf("oap-%s-%s-%s", clientID, siteID, agentID), policy)
}
```

### Token Renewal

OAP's Vault token is renewed at 70% of its `token_ttl` to ensure it never expires during normal operation. The renewal loop runs as a background goroutine:

```go
func (v *VaultBackend) tokenRenewalLoop(ctx context.Context) {
    ticker := time.NewTicker(time.Duration(float64(v.config.TokenTTL) * 0.7))
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            secret, err := v.client.Auth().Token().RenewSelfWithContext(ctx, 0)
            if err != nil {
                log.Error("vault.token_renewal_failed", "error", err)
                v.reauthenticate(ctx)
            } else {
                v.config.TokenTTL = time.Duration(secret.Auth.LeaseDuration) * time.Second
            }
        }
    }
}
```

## Infisical Backend

The Infisical backend wraps the Infisical Go SDK (`github.com/infisical/go-sdk`) and provides a simpler alternative to Vault for teams that prefer Infisical's developer experience.

### Authentication

Two authentication methods are supported:

#### Universal Auth

Universal Auth uses a client ID and client secret to obtain a short-lived access token from Infisical's auth service:

```go
func (i *InfisicalBackend) authenticate(ctx context.Context) error {
    resp, err := i.sdk.Auth().UniversalAuthLogin(ctx, infisical.UniversalAuthLoginRequest{
        ClientID:     i.config.ClientID,
        ClientSecret: i.config.ClientSecret,
    })
    if err != nil {
        return fmt.Errorf("infisical auth: %w", err)
    }
    i.accessToken = resp.AccessToken
    i.tokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
    return nil
}
```

#### Kubernetes Auth

For OAP deployments in Kubernetes, Kubernetes Auth uses the pod's service account token to authenticate, similar to Vault's Kubernetes auth method.

### Path Mapping

OAP paths are mapped to Infisical's folder and key structure:

| OAP Path Component | Infisical Equivalent |
|--------------------|----------------------|
| `backend_type` | Project selector (each Infisical project maps to one OAP backend instance) |
| `workspace_id` | Infisical environment (`dev`, `staging`, `prod`) |
| `path` | Folder path + key name (e.g., `/clients/acme/sites/branch-01/db-password`) |

The mapping is configured per-backend in `oapd.yaml`:

```yaml
secrets:
  backend: infisical
  infisical:
    site_url: https://app.infisical.com
    project_id: "abc123"
    environment: prod
    auth_method: universal
    client_id: "${INFISICAL_CLIENT_ID}"
    client_secret: "${INFISICAL_CLIENT_SECRET}"
```

### Auto Token Refresh

When an API call returns a 401 Unauthorized response, the backend transparently re-authenticates and retries the request. This handles the case where the access token expires during long-running operations:

```go
func (i *InfisicalBackend) doWithRefresh(ctx context.Context, fn func() error) error {
    err := fn()
    if err == nil {
        return nil
    }
    if !isAuthError(err) {
        return err
    }
    if err := i.authenticate(ctx); err != nil {
        return err
    }
    return fn() // retry once after re-auth
}
```

### Folder Inheritance

Infisical supports nested folder permissions. OAP maps its hierarchy to Infisical folders:

```
/clients/{{client_id}}/
    /sites/{{site_id}}/
        /agents/{{agent_id}}/
            db-password
            api-key
            tls-cert
```

Folder-level permissions in Infisical ensure that an agent scoped to `/agents/agent-042/` cannot read secrets in `/sites/branch-02/` even if it knows the path.

## Other Backends

### K8sCSIBackend

The Kubernetes CSI backend is read-only. It reads secrets from files mounted by the Secrets Store CSI Driver in the OAP pod's filesystem. The backend does not perform any authentication — the CSI driver handles secret synchronization at the kubelet level.

```go
type K8sCSIBackend struct {
    mountPath string // e.g., /var/secrets/oap
}

func (k *K8sCSIBackend) Get(ctx context.Context, path string, opts GetOptions) (*SecretValue, error) {
    fullPath := filepath.Join(k.mountPath, path)
    data, err := os.ReadFile(fullPath)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, ErrNotFound
        }
        return nil, err
    }
    return &SecretValue{
        Data: map[string]interface{}{"value": string(data)},
        Metadata: &SecretMetadata{Version: 1},
    }, nil
}
```

`Set` and `Delete` return `ErrReadOnly`. The CSI driver handles synchronization from Vault or Infisical into the pod's filesystem via `SecretProviderClass` custom resources (see K8s Integration below).

### EnvBackend

The environment variable backend reads secrets from the OAP process's environment. It is intended for development, CI/CD pipelines, and simple deployments where a full secret backend is unnecessary.

```go
type EnvBackend struct {
    prefix string // e.g., "OAP_SECRET_"
}

func (e *EnvBackend) Get(ctx context.Context, path string, opts GetOptions) (*SecretValue, error) {
    key := e.prefix + strings.ToUpper(strings.ReplaceAll(path, "/", "_"))
    val := os.Getenv(key)
    if val == "" {
        return nil, ErrNotFound
    }
    return &SecretValue{
        Data: map[string]interface{}{"value": val},
        Metadata: &SecretMetadata{Version: 1},
    }, nil
}
```

Environment variables are resolved at process start and cannot be updated without a restart. `Set` and `Delete` are no-ops (they log a warning if called).

### MemoryBackend

The memory backend is an in-process `sync.Map` used exclusively for tests. It is never instantiated in production. A build tag or environment check ensures this:

```go
func NewMemoryBackend() *MemoryBackend {
    if os.Getenv("OAP_ENV") == "production" {
        panic("MemoryBackend cannot be used in production")
    }
    return &MemoryBackend{store: &sync.Map{}}
}
```

## Secret Reference Model

### URI Format

All secret references in OAP use a uniform URI scheme:

```
ref:oap://<backend_type>/<workspace_id>/<path>?version=<v>&key=<k>
```

| Component | Description | Example |
|-----------|-------------|---------|
| `ref:oap://` | Fixed scheme prefix identifying an OAP secret reference | `ref:oap://` |
| `<backend_type>` | One of: `vault`, `infisical`, `k8s-csi`, `env`, `memory` | `vault` |
| `<workspace_id>` | OAP workspace or environment identifier | `prod` |
| `<path>` | Backend-specific path to the secret | `clients/acme/sites/branch-01/agents/agent-042/db-password` |
| `?version=<v>` | Optional. Pin to a specific version (KV v2). | `?version=3` |
| `?key=<k>` | Optional. Select a specific key from a multi-key secret. | `?key=password` |

Examples:

```
ref:oap://vault/prod/clients/acme/sites/branch-01/agents/agent-042/db-password
ref:oap://vault/prod/clients/acme/sites/branch-01/agents/agent-042/db-creds?dynamic=true
ref:oap://infisical/prod/clients/acme/api-keys/datadog
ref:oap://k8s-csi/prod/tls/server-cert
ref:oap://env/dev/local-overrides/debug-token
```

### Reference Parsing

The reference parser is defined in `internal/secrets/reference.go`:

```go
type SecretReference struct {
    BackendType string            `json:"backend_type"`
    WorkspaceID string            `json:"workspace_id"`
    Path        string            `json:"path"`
    Version     int               `json:"version,omitempty"`
    Key         string            `json:"key,omitempty"`
    Dynamic     bool              `json:"dynamic,omitempty"`
    Metadata    map[string]string `json:"metadata,omitempty"`
}

func Parse(raw string) (*SecretReference, error) {
    if !strings.HasPrefix(raw, "ref:oap://") {
        return nil, ErrInvalidReference
    }
    u, err := url.Parse(raw)
    if err != nil {
        return nil, fmt.Errorf("parse: %w", err)
    }
    ref := &SecretReference{
        BackendType: u.Host,
        WorkspaceID: strings.SplitN(u.Path, "/", 2)[0],
        Path:        strings.SplitN(u.Path, "/", 2)[1],
        Version:     parseIntQuery(u, "version"),
        Key:         u.Query().Get("key"),
        Dynamic:     u.Query().Get("dynamic") == "true",
    }
    return ref, nil
}
```

### Resolution Pipeline

When OAP needs to use a secret (e.g., to connect to a database on behalf of an agent), it executes the resolution pipeline:

```
  Parse          Lookup         Authorize       Fetch          Return
+--------+   +-----------+   +-----------+   +-----------+   +---------+
| raw URI|-->| backend   |-->| policy    |-->| backend   |-->| Secret- |
| string |   | registry  |   | check     |   | .Get()    |   | Value   |
+--------+   +-----------+   +-----------+   +-----------+   +---------+
```

1. **Parse**: The raw URI string is parsed into a `SecretReference` struct. Invalid URIs are rejected immediately.
2. **Lookup**: The backend registry is consulted to find the configured `SecretBackend` implementation matching the reference's `backend_type`. If no backend is configured for that type, resolution fails with `ErrBackendNotConfigured`.
3. **Authorize**: The requester's identity (agent ID, user ID, service identity) is checked against the reference path's policy. An agent at `agent-042` can resolve paths under `agents/agent-042/*` but not `agents/agent-043/*`.
4. **Fetch**: The backend's `Get` method is called with the reference path and any options (version, key, dynamic flag). For Vault dynamic secrets, this triggers credential generation and lease acquisition.
5. **Return**: The resolved `SecretValue` is returned to the caller. It is never logged or persisted.

### Concurrent Resolution and Semaphore

To prevent a thundering herd when many agents simultaneously request the same secret (e.g., during a mass-connect event), OAP uses a `golang.org/x/sync/semaphore`-based deduplication mechanism:

```go
type ResolutionCoordinator struct {
    inflight sync.Map // path -> *singleFlightResult
    sem      *semaphore.Weighted
}

type singleFlightResult struct {
    done chan struct{}
    val  *SecretValue
    err  error
}
```

When multiple goroutines request the same path simultaneously, only one calls the backend. The others wait on the `done` channel and receive the same result. A weighted semaphore limits total concurrent backend calls to prevent overwhelming the Vault server.

### TTL-Based LRU Cache

Resolved secrets are cached in a thread-safe LRU cache with a configurable TTL:

```go
type SecretCache struct {
    mu      sync.RWMutex
    entries map[string]*cacheEntry
    maxSize int
}

type cacheEntry struct {
    value     *SecretValue
    expiresAt time.Time
}
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_size` | 1000 | Maximum number of cached entries |
| `default_ttl` | 5m | Time-to-live for cached entries |
| `static_ttl` | 5m | TTL for static (KV v2) secrets |
| `dynamic_ttl` | Lease TTL | TTL for dynamic secrets (aligned with Vault lease) |

Dynamic secrets are never cached beyond their lease duration. Static secrets are cached for the configured TTL. Cache entries are evicted on TTL expiry or LRU eviction. The cache is invalidated on `Set` or `Delete`.

### Audit on Every Resolution

Every resolution — whether from the database, API call, or agent message — generates a structured audit log entry:

```go
type ResolutionAuditEvent struct {
    Timestamp    time.Time `json:"timestamp"`
    RequesterID  string    `json:"requester_id"`
    RequesterType string   `json:"requester_type"` // agent, user, service
    BackendType  string    `json:"backend_type"`
    Reference    string    `json:"reference"` // sanitized, no values
    Success      bool      `json:"success"`
    ErrorCode    string    `json:"error_code,omitempty"`
    DurationMs   int64     `json:"duration_ms"`
    CacheHit     bool      `json:"cache_hit"`
}
```

Audit events are shipped to the audit subsystem (see API Endpoints below) and forwarded to the customer's SIEM if configured.

## Credential Injection Pipeline

OAP injects resolved secrets into the execution context of scripts, tasks, and agent operations via three methods, selected based on the target environment:

### Method 1: Environment Variables (Prefix: `OAP_INJECTED_`)

Secrets are injected as environment variables with the `OAP_INJECTED_` prefix:

```bash
OAP_INJECTED_DB_PASSWORD=s3cur3-p4ss
OAP_INJECTED_API_KEY=ak_live_abc123
```

The prefix ensures that injected secrets are clearly distinguishable from regular environment variables. The script execution subsystem filters these out from the agent's regular environment and makes them available only within the script's subprocess. After the script exits, the environment is destroyed.

### Method 2: Files (Mode 0600)

Secrets are written to temporary files with permissions `0600` (read/write for owner only):

```go
func injectFile(secrets map[string]string) (dir string, cleanup func(), err error) {
    dir, err = os.MkdirTemp("", "oap-inject-")
    if err != nil {
        return "", nil, err
    }
    for name, value := range secrets {
        path := filepath.Join(dir, name)
        if err := os.WriteFile(path, []byte(value), 0600); err != nil {
            os.RemoveAll(dir)
            return "", nil, err
        }
    }
    cleanup = func() { os.RemoveAll(dir) }
    return dir, cleanup, nil
}
```

Files are created in a temp directory and securely deleted after use.

### Method 3: Stdin (Unix Socket)

For scripts that should not receive secrets via environment variables or files (e.g., to avoid leaking into process listings), secrets are streamed over a Unix domain socket. The OAP runner creates a socket, streams the secret data, and the script reads from it:

```go
func injectStdin(secrets map[string]string) (sockPath string, err error) {
    sockPath = filepath.Join(os.TempDir(), fmt.Sprintf("oap-sock-%d", os.Getpid()))
    listener, err := net.Listen("unix", sockPath)
    if err != nil {
        return "", err
    }
    go func() {
        conn, err := listener.Accept()
        if err != nil { return }
        defer conn.Close()
        for name, value := range secrets {
            fmt.Fprintf(conn, "%s=%s\n", name, value)
        }
    }()
    return sockPath, nil
}
```

### TTL Sweeper

A background goroutine runs every 10 seconds to clean up expired injection artifacts:

```go
func (i *Injector) sweeper(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            i.cleanupExpired()
        }
    }
}
```

### Secure Deletion

When injection artifacts expire or the task completes, secrets are securely deleted:

```go
func secureDelete(path string) error {
    f, err := os.OpenFile(path, os.O_WRONLY, 0)
    if err != nil {
        return err
    }
    info, err := f.Stat()
    if err != nil {
        f.Close()
        return err
    }
    // Zero-fill the file
    if _, err := f.Write(make([]byte, info.Size())); err != nil {
        f.Close()
        return err
    }
    f.Sync()
    f.Close()
    return os.Remove(path)
}
```

For dynamic secrets, `secureDelete` also revokes the Vault lease to immediately invalidate the credential.

### Just-in-Time Fetching

Secrets are fetched at the moment of injection, not before. The injection pipeline calls `Resolve` immediately before the script or task executes, ensuring that the latest version of the secret is used and that the lease window for dynamic secrets is minimized.

## A2A Auth Token Management

OAP's Agent-to-Agent (A2A) communication uses EdDSA Ed25519 JWTs for authentication. These tokens are distinct from backend secrets — they are issued and verified by OAP itself for inter-service and inter-agent authentication.

### Token Format

A2A tokens are JWTs signed with EdDSA (Ed25519):

```
Header:  { "alg": "EdDSA", "typ": "JWT", "kid": "<key_id>" }
Payload: {
  "iss": "oap://oapd.internal",
  "sub": "agent://acme/branch-01/agent-042",
  "aud": "oap://target-service",
  "jti": "01HXYZ...",
  "exp": 1718000000,
  "nbf": 1717996400,
  "iat": 1717996400,
  "scopes": ["read:metrics", "write:alerts"],
  "delegation_chain": [
    { "issuer": "user://admin@acme.com", "delegated_to": "agent://acme/branch-01/agent-042", "scopes": ["read:metrics"], "exp": 1717997000 }
  ]
}
```

### Claims

| Claim | Description |
|-------|-------------|
| `iss` | Token issuer (OAP server identity) |
| `sub` | Subject — the agent or service identity |
| `aud` | Intended audience (target service or agent) |
| `jti` | Unique token ID for revocation tracking |
| `exp` | Expiration timestamp |
| `nbf` | Not-before timestamp |
| `iat` | Issued-at timestamp |
| `scopes` | Array of permission scopes |
| `delegation_chain` | Array of delegation records showing the chain of authority |

### Operations

#### Issue

```go
func (a *A2ATokenManager) Issue(identity AgentIdentity, scopes []string, ttl time.Duration) (string, error) {
    claims := jwt.MapClaims{
        "iss":   a.issuer,
        "sub":   identity.URI,
        "aud":   identity.TargetAudience,
        "jti":   generateJTI(),
        "exp":   time.Now().Add(ttl).Unix(),
        "nbf":   time.Now().Unix(),
        "iat":   time.Now().Unix(),
        "scopes": scopes,
    }
    token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
    token.Header["kid"] = a.keyID
    return token.SignedString(a.privateKey)
}
```

#### Exchange (Down-Scoping and Chain Extension)

Token exchange allows an agent to receive a new token with a subset of its own scopes, optionally extending the delegation chain. The original token is presented as proof of authority:

```go
func (a *A2ATokenManager) Exchange(parentToken string, requestedScopes []string, targetAudience string) (string, error) {
    parent, err := a.Verify(parentToken)
    if err != nil {
        return "", err
    }
    // Down-scope: requested scopes must be subset of parent scopes
    allowed := intersectScopes(requestedScopes, parent.Scopes)
    // Extend delegation chain
    chain := append(parent.DelegationChain, DelegationRecord{
        Issuer:     parent.Subject,
        DelegatedTo: targetAudience,
        Scopes:     allowed,
        Exp:        time.Now().Add(parent.RemainingTTL).Unix(),
    })
    return a.Issue(AgentIdentity{
        URI:            parent.Subject,
        TargetAudience: targetAudience,
    }, allowed, parent.RemainingTTL)
}
```

#### Verify

Verification checks the signature, expiration, audience, scopes, and revocation status:

```go
func (a *A2ATokenManager) Verify(tokenStr string) (*VerifiedToken, error) {
    token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
        if t.Method.Alg() != "EdDSA" {
            return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
        }
        return a.publicKey, nil
    })
    if err != nil {
        return nil, err
    }
    claims := token.Claims.(jwt.MapClaims)
    // Check expiration
    if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
        return nil, ErrTokenExpired
    }
    // Check not-before
    if nbf, ok := claims["nbf"].(float64); ok && time.Now().Unix() < int64(nbf) {
        return nil, ErrTokenNotYetValid
    }
    // Check revocation
    jti := claims["jti"].(string)
    if a.revocationList.IsRevoked(jti) {
        return nil, ErrTokenRevoked
    }
    return &VerifiedToken{
        Subject:          claims["sub"].(string),
        Audience:         claims["aud"].(string),
        Scopes:           claims["scopes"].([]string),
        DelegationChain:  parseChain(claims["delegation_chain"]),
        RemainingTTL:     time.Duration(claims["exp"].(float64) - float64(time.Now().Unix())) * time.Second,
    }, nil
}
```

Scope wildcards are supported: a token with scope `read:metrics:*` grants any `read:metrics:<subscope>` action.

#### Revoke

Revocation adds the token's `jti` to an in-memory revocation list (replicated across OAP nodes via the gossip protocol):

```go
func (a *A2ATokenManager) Revoke(jti string) error {
    a.revocationList.Add(jti)
    a.gossipBroadcast("token.revoked", jti)
    return nil
}
```

### Delegation Depth Limit

The maximum delegation chain depth is 3. Each delegation reduces the TTL proportionally to limit the lifetime of deeply delegated tokens:

| Depth | TTL Reduction |
|-------|---------------|
| 0 (original issue) | Full TTL |
| 1 (first exchange) | 80% of parent TTL |
| 2 (second exchange) | 60% of parent TTL |
| 3 (third exchange) | 40% of parent TTL |
| > 3 | Rejected with `ErrMaxDelegationDepth` |

## Script Credential Safety

OAP enforces a strict policy: **secrets never appear in script arguments, command lines, or environment variables on the endpoint.**

### Why This Matters

If a secret is passed as a command-line argument, it appears in:
- `ps aux` output on the endpoint
- Shell history files
- Process accounting logs
- Core dumps
- Audit logs on the endpoint

If a secret is passed as a plain environment variable, it can be read by any process running under the same user.

### OAP's Approach: Server-Side Authenticated Operations

Instead of passing credentials to endpoint scripts, OAP performs authenticated operations server-side. The agent on the endpoint never sees the raw credential:

1. **Agent sends request**: The agent sends a task request to the OAP server (e.g., "check database health").
2. **Server resolves secret**: The OAP server resolves the database password from the secret backend.
3. **Server performs operation**: The server connects to the database using the resolved credential and executes the health check.
4. **Server returns result**: The result (e.g., "database is healthy") is sent back to the agent. The credential never leaves the server.

For operations that must run on the endpoint (e.g., updating a local configuration file), OAP uses **Just-in-Time Endpoint Credential Delivery**:

1. The agent requests a credential for a specific operation.
2. The OAP server validates the request against the agent's permissions.
3. If authorized, the server establishes a short-lived mTLS session with the agent.
4. The credential is streamed over the mTLS session, used immediately, and the session is closed.
5. The credential is never written to disk on the endpoint and is zero-filled in memory after use.

### Audit Logging

Every script credential operation is logged:

```go
audit.Log("script.credential.delivered", map[string]interface{}{
    "agent_id":     agentID,
    "script_id":    scriptID,
    "credential_ref": ref.URI(),
    "delivery_method": "mtls_stream",
    "session_duration_ms": sessionDuration,
})
```

## MCP OAuth 2.1

OAP implements OAuth 2.1 for Model Context Protocol (MCP) server authentication, with two advanced features: DPoP (Demonstrating Proof-of-Possession) binding and Dynamic Client Registration.

### DPoP Binding

DPoP (RFC 9449) binds access tokens to a client's public key, preventing token theft and replay. Each MCP request includes a DPoP proof JWT signed by the client's private key:

```
POST /mcp/tool/execute
Authorization: DPoP <access_token>
DPoP: <dpop_proof_jwt>
```

The DPoP proof contains:
- `htm`: HTTP method
- `htu`: HTTP URI
- `iat`: Issued-at timestamp
- `jti`: Unique nonce
- `nonce`: Server-provided nonce (for replay protection)

The server verifies that the DPoP proof's public key matches the key bound to the access token at issuance time. If they don't match, the request is rejected.

### Dynamic Client Registration (RFC 7591)

MCP clients can register themselves with OAP at runtime via the registration endpoint:

```
POST /oauth/register
Content-Type: application/json

{
  "client_name": "My MCP Client",
  "redirect_uris": ["https://my-mcp-client.example.com/callback"],
  "grant_types": ["authorization_code", "refresh_token"],
  "token_endpoint_auth_method": "private_key_jwt",
  "jwks_uri": "https://my-mcp-client.example.com/.well-known/jwks.json"
}
```

OAP issues a client ID and stores the registration. No pre-registration or manual approval is required, enabling zero-touch MCP integration.

### Protected Resource Metadata (RFC 9728)

OAP publishes its protected resource metadata at a well-known endpoint:

```
GET /.well-known/oauth-protected-resource/mcp

{
  "resource": "https://oap.example.com/mcp",
  "authorization_servers": ["https://oap.example.com/oauth"],
  "scopes_supported": ["read:metrics", "write:alerts", "execute:scripts"],
  "bearer_methods_supported": ["header", "body"],
  "dpop_signing_alg_values_supported": ["EdDSA"]
}
```

MCP clients discover OAP's capabilities and supported scopes via this endpoint before initiating the OAuth flow.

## Hierarchy-Based Access

OAP organizes managed resources in a three-level hierarchy:

```
Client (Organization)
└── Site (Location / Environment)
    └── Agent (Individual Managed Device)
```

Secret access is scoped to the hierarchy level. An agent can access secrets at its own level and below, but never above.

### Access Rules

| Requester | Can Access | Cannot Access |
|-----------|-----------|---------------|
| Client admin | `clients/{client_id}/*` (all sites and agents) | N/A (top level) |
| Site admin | `clients/{client_id}/sites/{site_id}/*` | `clients/{other_client_id}/*`, `clients/{client_id}/sites/{other_site_id}/*` |
| Agent | `clients/{client_id}/sites/{site_id}/agents/{agent_id}/*` | Any path not under its own agent ID |

### Vault Policy Hierarchy

In Vault, this is enforced via ACL policies. Each hierarchy entity gets a policy with path-scoped capabilities:

```hcl
# Client-level policy (admin)
path "secret/data/clients/acme/*" {
  capabilities = ["read", "list", "create", "update", "delete"]
}

# Site-level policy (site admin)
path "secret/data/clients/acme/sites/branch-01/*" {
  capabilities = ["read", "list", "create", "update", "delete"]
}

# Agent-level policy (agent)
path "secret/data/clients/acme/sites/branch-01/agents/agent-042/*" {
  capabilities = ["read", "list"]
}
```

### Infisical Folder Mapping

In Infisical, folder-level permissions enforce the same boundaries:

```
/clients/acme/          → Read+Write (client admin)
/clients/acme/sites/branch-01/  → Read+Write (site admin)
/clients/acme/sites/branch-01/agents/agent-042/  → Read-only (agent)
```

The OAP server translates the requester's identity into the appropriate Infisical scope before making API calls.

## API Endpoints

OAP exposes nine route groups for secret management:

### routes_secrets — Core Secret CRUD

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/secrets` | Create a new secret reference |
| `GET` | `/api/v1/secrets/{ref}` | Resolve a secret reference (returns the value) |
| `PUT` | `/api/v1/secrets/{ref}` | Update an existing secret |
| `DELETE` | `/api/v1/secrets/{ref}` | Delete a secret |
| `GET` | `/api/v1/secrets/{ref}/metadata` | Get metadata (version, timestamps) |

### routes_references — Reference Management

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/references` | Create a stored reference (in primary database) |
| `GET` | `/api/v1/references/{id}` | Get a stored reference |
| `PUT` | `/api/v1/references/{id}` | Update a stored reference |
| `DELETE` | `/api/v1/references/{id}` | Delete a stored reference |
| `GET` | `/api/v1/references?prefix=...` | List references by prefix |

### routes_rotation — Rotation Operations

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/secrets/{ref}/rotate` | Trigger rotation of a secret |
| `GET` | `/api/v1/secrets/{ref}/rotation-history` | Get rotation history |
| `POST` | `/api/v1/secrets/rotate/batch` | Batch rotation of multiple secrets |

### routes_injection — Credential Injection

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/injection/env` | Inject secrets as environment variables |
| `POST` | `/api/v1/injection/file` | Inject secrets as files |
| `POST` | `/api/v1/injection/stdin` | Inject secrets via stdin/Unix socket |
| `DELETE` | `/api/v1/injection/{injection_id}` | Revoke an active injection |

### routes_a2a_tokens — A2A Token Management

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/a2a/tokens` | Issue a new A2A token |
| `POST` | `/api/v1/a2a/tokens/exchange` | Exchange a token (down-scope) |
| `POST` | `/api/v1/a2a/tokens/verify` | Verify a token |
| `POST` | `/api/v1/a2a/tokens/{jti}/revoke` | Revoke a token |
| `GET` | `/api/v1/a2a/tokens/{jti}` | Get token metadata (not the value) |

### routes_mcp — MCP OAuth Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/oauth/register` | Dynamic Client Registration (RFC 7591) |
| `GET` | `/.well-known/oauth-protected-resource/mcp` | Protected Resource Metadata (RFC 9728) |
| `POST` | `/oauth/token` | Token endpoint (with DPoP validation) |
| `GET` | `/oauth/authorize` | Authorization endpoint |

### routes_audit — Audit Log Access

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/audit/secrets?from=...&to=...` | List secret resolution events |
| `GET` | `/api/v1/audit/secrets/{event_id}` | Get a specific audit event |
| `GET` | `/api/v1/audit/export?format=jsonl` | Export audit logs |

### routes_hierarchy — Hierarchy-Based Access

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/hierarchy/clients/{client_id}/secrets` | List all secrets under a client |
| `GET` | `/api/v1/hierarchy/sites/{site_id}/secrets` | List all secrets under a site |
| `GET` | `/api/v1/hierarchy/agents/{agent_id}/secrets` | List all secrets accessible by an agent |
| `POST` | `/api/v1/hierarchy/policies/apply` | Apply hierarchy policies to a backend |

### routes_migration — Backend Migration

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/migration/start` | Start a migration between backends |
| `GET` | `/api/v1/migration/{job_id}/status` | Get migration status |
| `POST` | `/api/v1/migration/{job_id}/cancel` | Cancel an in-progress migration |
| `GET` | `/api/v1/migration/{job_id}/report` | Get migration report |

## K8s Integration

OAP integrates with Kubernetes via the Secrets Store CSI Driver, which mounts secrets from external backends (Vault, Infisical) as files in the OAP pod's filesystem.

### Secrets Store CSI Driver Architecture

```
+-----------------------------------+
|        OAP Pod                    |
|  +-----------------------------+  |
|  |  /var/secrets/oap/          |  |
|  |    ├── tls/                 |  |
|  |    │   ├── server-cert      |  |
|  |    │   └── server-key       |  |
|  |    ├── db/                  |  |
|  |    │   └── password         |  |
|  |    └── api/                 |  |
|  |        └── datadog-key      |  |
|  +-----------------------------+  |
|  |  K8sCSIBackend reads from   |  |
|  |  /var/secrets/oap/          |  |
|  +-----------------------------+  |
+-----------------------------------+
              |
              v
+-----------------------------------+
|  Secrets Store CSI Driver         |
|  (kubelet-level volume mount)     |
+-----------------------------------+
              |
              v
+-----------------------------------+
|  Vault Provider / Infisical       |
|  Provider                         |
+-----------------------------------+
              |
              v
+-----------------------------------+
|  Vault / Infisical Backend        |
+-----------------------------------+
```

### SecretProviderClass CRD

OAP creates `SecretProviderClass` custom resources to define which secrets to mount:

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: oap-secrets
  namespace: oap
spec:
  provider: vault          # or "infisical"
  parameters:
    vaultAddress: "https://vault.internal.oap.example.com:8200"
    roleName: "oap-server"
    objects: |
      - objectName: "tls-server-cert"
        secretPath: "secret/data/clients/acme/tls"
        secretKey: "cert"
      - objectName: "tls-server-key"
        secretPath: "secret/data/clients/acme/tls"
        secretKey: "key"
      - objectName: "db-password"
        secretPath: "secret/data/clients/acme/sites/branch-01/db"
        secretKey: "password"
  secretObjects:
    - secretName: oap-tls
      type: kubernetes.io/tls
      data:
      - objectName: tls-server-cert
        key: tls.crt
      - objectName: tls-server-key
        key: tls.key
```

### Sync Interval and Rotation

The CSI driver periodically syncs secrets from the backend. OAP configures a sync interval of 5 minutes for static secrets. For dynamic secrets, the CSI driver is not used — OAP uses direct Vault API calls for just-in-time dynamic credential generation, since CSI-mounted files cannot carry short-lived leases.

### Kubernetes Auth Integration

When using the Vault provider, the CSI driver authenticates using the pod's service account token, which maps to the OAP Vault role. This provides seamless authentication without distributing `secret_id` values.

## Implementation Steps

The secret management subsystem is implemented in 10 ordered steps:

### Step 1: Backend Interface and Registry

Create the `SecretBackend` interface in `internal/secrets/backend.go`. Implement the backend registry in `internal/secrets/registry.go` that maps `backend_type` strings to backend instances. The registry is populated at startup from configuration. Unit tests cover interface compliance for all five implementations.

### Step 2: Memory and Environment Backends

Implement `MemoryBackend` (for tests) and `EnvBackend` (for development). These are the simplest backends and serve as reference implementations. The `EnvBackend` reads from `os.Getenv` with a configurable prefix. Both include comprehensive unit tests.

### Step 3: Reference Parser and URI Schema

Implement the `SecretReference` struct and `Parse` function in `internal/secrets/reference.go`. Support the full URI format including `?version=`, `?key=`, and `?dynamic=true` query parameters. Include fuzz tests to ensure the parser handles malformed URIs gracefully.

### Step 4: Secret Resolution Pipeline and Cache

Implement the resolution coordinator with single-flight deduplication and the TTL-based LRU cache. The coordinator is the central entry point for all secret resolution. It handles the parse→lookup→authorize→fetch→return pipeline and manages cache invalidation on `Set` and `Delete`.

### Step 5: Vault Backend Implementation

Implement `VaultBackend` with all four auth methods (AppRole, Kubernetes, JWT/OIDC, Token). Include the lease manager for dynamic secrets, the token renewal loop at 70% TTL, and the policy-to-hierarchy mapping. Integration tests use Vault's test container (`testcontainers-go/hashicorp/vault`).

### Step 6: Infisical Backend Implementation

Implement `InfisicalBackend` with Universal Auth and Kubernetes Auth. Include the path mapping, auto token-refresh on 401, and folder hierarchy enforcement. Integration tests use the Infisical test environment.

### Step 7: K8s CSI Backend and SecretProviderClass Generation

Implement `K8sCSIBackend` (read-only file reads) and the controller that generates `SecretProviderClass` custom resources from OAP secret references. The controller watches for changes in the OAP database and reconciles the CSI resources.

### Step 8: Credential Injection Pipeline

Implement the three injection methods (env, file, stdin) in `internal/secrets/injector.go`. Include the TTL sweeper (10-second interval), secure deletion (zero-fill + unlink), and the JIT fetching logic. Unit tests cover each injection method and the cleanup lifecycle.

### Step 9: A2A Token Management

Implement the A2A token manager with EdDSA signing/verification, the delegation chain logic (max depth 3, TTL reduction), the revocation list with gossip replication, and the scope wildcard matching. Integration tests cover the full issue→exchange→verify→revoke lifecycle.

### Step 10: MCP OAuth 2.1 and API Endpoints

Implement the OAuth 2.1 server (authorization, token, registration endpoints) with DPoP binding. Implement all nine API route groups. Wire up the audit logging to ship resolution events to the audit subsystem. Add end-to-end tests for the complete secret management flow, from reference creation through credential injection to audit verification.
