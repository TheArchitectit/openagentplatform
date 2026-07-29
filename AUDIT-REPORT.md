# Audit Report: OpenAgentPlatform

> Note: a previous version of this file described an unrelated project
> ("agent-guardrails-template", Go/Python/TypeScript guardrails framework,
> 393 files). That content was stale and did not apply to this repository
> (OpenAgentPlatform, an agent-first RMM platform). It has been replaced.

## Scope

Security and correctness audit of the OpenAgentPlatform server (Go 1.25),
agent (`cmd/agent`), A2A gateway, MCP server, and the React web UI.

## Findings and remediation

Findings are tracked as pull requests. Each PR is focused on one theme and
includes tests.

| Area | Finding | PR |
| --- | --- | --- |
| Authorization | Mutating API routes had no role checks (script RCE by any `viewer`) | `fix/rbac-enforce-role-middleware` |
| Authorization | A2A gateway built with `RequireAuth=false`; unauthenticated task/agent control | `fix/a2a-gateway-enable-auth` |
| Hardening | WS shell dev-fallback minted admin; webhook SSRF; prod debug/cookie guards; body-size limit | `fix/defense-in-depth-hardening` |
| Concurrency | WebSocket send-on-closed-channel panic | `fix/websocket-hub-panic` |
| Correctness | Patch-scan context cancelled on return; OIDC exchange double-request; audit outcome; JWT claims; agent shutdown | `fix/logic-bugs` |
| Deploy/docs | Missing Dockerfiles; NATS cert generation; OIDC issuer mismatch; seed target; web test toolchain; README accuracy | `fix/deploy-and-docs` |

## Verified clean

- **SQL injection**: all dynamic queries use `$N` parameterization; `fmt.Sprintf`
  only builds placeholder positions, never interpolates values.
- **Command execution**: the agent executor/patcher/shell use
  `exec.Command(name, args...)` (no shell).
- **Crypto**: AES-GCM nonces from `crypto/rand`; session/agent tokens use
  Ed25519 with method pinning; `subtle.ConstantTimeCompare` for API keys.
- **Resource handling**: DB rows uniformly `defer rows.Close()`; transactions
  `defer tx.Rollback`; HTTP bodies closed; NATS drained on shutdown.
