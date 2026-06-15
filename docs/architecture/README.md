# Architecture Documentation

> Comprehensive architecture documentation for OpenAgentPlatform — an open-source, agent-first RMM platform.

## Documents

| Document | Description |
|----------|-------------|
| [RMM Core](RMM_CORE.md) | Device registration, checks, policies, patches, alerts, scripts, remote access, NATS orchestration |
| [A2A Protocol](A2A_PROTOCOL.md) | Agent-to-Agent gateway, task lifecycle, protocol bindings, agent card registry, HITL |
| [Agent Framework Adapters](AGENT_FRAMEWORKS.md) | AgentWrapper ABC, 6 framework adapters (LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI, Anthropic), process pool |
| [Secret Management](SECRET_MANAGEMENT.md) | Vault/Infisical backends, secret references, credential injection, A2A auth tokens, MCP OAuth 2.1 |
| [Endpoint API](ENDPOINT_API.md) | REST + NATS dual-transport API, agent binary, NATS bus, gRPC service |
| [Frontend](FRONTEND.md) | React 19 SPA, TanStack Router/Query, Shadcn/ui, 10 feature modules, 20 pages |
| [Infrastructure](INFRASTRUCTURE.md) | Docker Compose, Helm chart, CI/CD, observability, security, backup/DR |
| [Auth & RBAC](AUTH_AND_RBAC.md) | JWT, mTLS, OAuth 2.1, SAML, OIDC, API keys, 5 RBAC roles, MFA, audit log, SCIM |
| [Integration & Events](INTEGRATION_AND_EVENTS.md) | Service communication map, NATS event flow, shared schemas, error propagation, consistency patterns |
| [Commercial & Licensing](COMMERCIAL_AND_LICENSING.md) | BSL 1.1 license, feature gating, tier definitions, multi-tenancy, billing |
| [Roadmap & Sprints](ROADMAP_AND_SPRINTS.md) | 7-phase/46-week plan, sprint stories, parallel streams, release strategy, risks |

## Related Documents

- [Research Report](../RESEARCH_REPORT.md) — Deep research findings (96 agents, adversarial verification)
- [Master Implementation Plan](../plans/MASTER_IMPLEMENTATION_PLAN.md) — Exhaustive implementation blueprint (1,315 lines)
