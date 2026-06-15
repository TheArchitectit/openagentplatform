export const meta = {
  name: 'full-implementation-plan',
  description: 'Exhaustive implementation plan for OpenAgentPlatform covering all subsystems, data models, APIs, agents, infra, testing, CI/CD, docs, and commercial tiers',
  phases: [
    { title: 'Analyze', detail: 'Deep-read research report and existing codebase to anchor the plan' },
    { title: 'Domain Plans', detail: 'Parallel plans for each major domain subsystem' },
    { title: 'Cross-Cut', detail: 'Plan cross-cutting concerns: auth, observability, testing, infra' },
    { title: 'Integration', detail: 'Plan integration points between all subsystems' },
    { title: 'Sequencing', detail: 'Build dependency graph, phased roadmap, and milestone definitions' },
    { title: 'Synthesize', detail: 'Merge all domain plans into one master implementation plan' },
  ],
}

// Phase 1: Analyze — haiku for reading/extraction tasks
phase('Analyze')

const reportArtifacts = await agent(`Read the research report at /mnt/data/git/openagentplatform/docs/RESEARCH_REPORT.md and extract EVERY implementable artifact mentioned or implied. For each, give: name, domain (rmm-core/a2a/agent-frameworks/secret-mgmt/endpoint-api/frontend/infra/auth/testing/observability/docs/commercial), dependencies on other artifacts, key design decisions, and open questions. Be exhaustive - every model, endpoint, service, adapter, event topic, config, test category, and doc.`, {
  label: 'analyze:report',
  phase: 'Analyze',
  model: 'haiku',
})

const codebaseState = await agent(`Analyze the existing codebase at /mnt/data/git/openagentplatform/. List every directory and key file, what it does, and whether to keep/extend/replace/remove it. Identify what the agent-guardrails-template already provides that we can build on.`, {
  label: 'analyze:codebase',
  phase: 'Analyze',
  model: 'haiku',
})

log('Analysis complete')

// Phase 2: Domain Plans — sonnet for detailed planning
phase('Domain Plans')

const domainSpecs = {
  'rmm-core': `Plan the core RMM subsystem: Django app layout, Agent model (devices with JSON inventory fields), Check model (flat-table polymorphic with check_type discriminator), Policy model (Client>Site>Agent propagation with enforcement/exclusion), WinUpdate model (per-agent per-update with approval workflow), AutomatedTask model (bitmask scheduling, JSON action arrays), InstalledSoftware model. All REST API endpoints with request/response schemas. NATS JetStream: subject taxonomy, msgpack for agent/JSON for server, per-agent subscription. Agent check-in protocol. Script execution engine (multi-runtime, streaming). Patch management scan/approve/install workflow. Remote access relay (WebSocket, WebRTC P2P). Alert lifecycle. Policy engine. Background Celery tasks. Database schema with all tables, fields, indexes, constraints, JSONField/ArrayField structures. Migration strategy.`,

  'a2a': `Plan the A2A subsystem: Gateway service (task lifecycle state machine, Agent Card registry, routing). Protocol bindings (JSON-RPC 2.0 over HTTP/SSE, gRPC server-streaming, REST+JSON+SSE). Proto compilation from a2a.proto. Agent Card discovery at /.well-known/agent-card.json. Task persistence in PostgreSQL. Push notification system. Auth (bearer, mTLS, OAuth 2.1). Event-to-Task bridge converting RMM events to A2A Tasks. Streaming support. Error handling. Gateway scaling. A2A test suite integration.`,

  'agent-frameworks': `Plan the Agent Framework Adapter subsystem: AgentWrapper interface (agentCard, invoke, stream, cancel, interrupt). LangGraph adapter (state dict translation, compiled graph execution, artifact extraction, LangSmith A2A endpoint). CrewAI adapter (leverage native crewai.a2a module directly, register as A2A peers). AutoGen adapter (GroupChat to SendMessage mapping, termination to Task state). Semantic Kernel adapter (HandoffOrchestration wrapping, MAF A2A hosting). OpenAI Agents SDK adapter (handoff to SendMessage, tools to MCP). Anthropic Claude adapter (tool-use as A2A skills, streaming to Artifacts). Agent process pool (warm pool, LRU eviction, health check, spawn/drain/kill). Orchestration service routing. Cost management (per-endpoint caps, token usage). Human-in-the-loop (INPUT_REQUIRED state, approval UI, notifications).`,

  'secret-mgmt': `Plan Secret Management: Provider abstraction (SecretBackend ABC with get/set/delete/rotate/list). Vault integration (hvac, AppRole/K8s/JWT auth, KV v2, dynamic secrets, audit, policy-to-hierarchy mapping). Infisical integration (SDK, machine identity, folder mapping, rotation). Secret Reference model (backend_type + path + version, never values). Credential injection pipeline (JIT fetch, env/file injection, post-task revocation). A2A auth token management. Script credential safety (server-side auth, JIT delivery, audit). MCP OAuth 2.1 integration. Hierarchy-based access. K8s Secrets Store CSI Driver. Migration and fallback.`,

  'endpoint-api': `Plan the Endpoint API: Complete REST API spec for every endpoint with request/response schemas, status codes, pagination, filtering. NATS subject taxonomy with message schemas, consumer groups, retention. Agent binary (Go): NATS client, msgpack, per-agentID subscription, Func dispatch, multi-runtime script executor, CmdV2 streaming. Agent registration flow. Check execution protocol. Real-time event streaming. API versioning. Rate limiting. gRPC reflection. OpenAPI 3.1 spec. SDK generation (Python, Go, TypeScript).`,

  'frontend': `Plan the Frontend: React 19 + TypeScript project. TanStack Router (file-based routing, all routes). TanStack Query (server state, optimistic updates). Shadcn/ui + Tailwind CSS. Dashboard (real-time agent status, alerts, check health, patch compliance). Agent Management UI. Monitoring UI (check config, time-series charts, alerts). Patch Management UI. Remote Access (xterm.js terminal, noVNC desktop). Script Editor (Monaco, execution console). A2A Dashboard (card viewer, task lifecycle, artifacts). Policy Editor. Secret Management UI. Settings (users, RBAC, SSO, notifications). Real-time via WebSocket.`,

  'infra': `Plan Infrastructure: Docker Compose dev stack. Kubernetes Helm chart. CI/CD GitHub Actions (lint, test, build, release, deploy). Database migrations (zero-downtime, rollback). OpenTelemetry (traces, metrics, logs). Prometheus + Grafana + Loki. Security (TLS, cert-manager, network policies). Backup/DR (PostgreSQL backup, NATS stream backup, Velero, RPO/RTO). Multi-region. Scaling strategies. Dev environment setup. Production hardening.`,
}

const domainPlans = await parallel(Object.entries(domainSpecs).map(([domain, spec]) => () =>
  agent(`You are a senior systems architect creating an exhaustive, implementation-ready plan for the "${domain}" domain of OpenAgentPlatform — an agent-first open-source RMM.

${spec}

For EACH component, provide:
1. File paths (exact files to create)
2. Data models (complete field definitions, types, constraints, defaults, indexes)
3. API schemas (full request/response JSON per endpoint)
4. Service logic (algorithms, state machines, error handling)
5. Configuration (env vars, settings, feature flags)
6. Tests (file paths, test names, what each verifies)
7. Dependencies (packages with versions)
8. Implementation steps (ordered, atomic, each producing a working increment)

Be exhaustive. No "TODO" or placeholders. Every component gets a full specification a developer can implement without ambiguity.`, {
    label: `plan:${domain}`,
    phase: 'Domain Plans',
    model: 'sonnet',
  })
))

log(`Domain plans complete: ${domainPlans.filter(Boolean).length} domains planned`)

// Phase 3: Cross-Cut — sonnet for detailed cross-cut plans
phase('Cross-Cut')

const crossCutSpecs = {
  'auth': `Plan Auth & RBAC for OpenAgentPlatform: JWT auth (access+refresh), API key auth for agents, mTLS for NATS, OAuth 2.1 for MCP, A2A auth schemes. RBAC: SuperUser/Manager/Technician/ReadOnly/Agent roles, per-resource permissions, Client>Site scoping, agent action policies. SSO (SAML 2.0, OIDC, SCIM) for Professional tier. Session management (Redis, concurrent limits, revocation). MFA (TOTP, backup codes, enforcement). Audit logging (every API call, agent action, A2A task, secret access). File paths, models, middleware, tests.`,

  'testing': `Plan Testing Strategy: Unit tests (90%+ coverage target). Integration tests (REST API, NATS roundtrip, A2A conformance, secret backend). E2E tests (agent deploy→check-in→monitor→alert→remediate, A2A cross-framework, remote access, patch mgmt). Load tests (k6/Locust: 10k endpoints, 100k checks). Security tests (OWASP ZAP, secret leak detection, RBAC bypass, script injection). Chaos (NATS fail, DB failover, agent disconnect, LLM timeout). Test infra (GitHub Actions matrix, Testcontainers, Playwright, coverage enforcement).`,

  'observability': `Plan Observability: OpenTelemetry (traces from Django/NATS/A2A/Celery/agent, RED metrics, structured JSON logs with correlation IDs). Prometheus custom metrics (endpoint count, check rates, A2A tasks, pool utilization, LLM tokens). Grafana dashboards (overview, RMM, A2A, infra, cost). Operational runbooks (agent failure, NATS down, A2A stuck, DB rollback, secret rotation fail, hallucination rollback). SLIs/SLOs (API 99.9%, check-in p99 <5s, A2A task p95 <30s, alert delivery <60s).`,

  'docs': `Plan Documentation: Developer docs (getting started 5-min setup, architecture, contributing, API reference, A2A integration guide, agent dev guide). Operator docs (install Docker/K8s/bare metal, config reference, secret backend setup, upgrade guide, backup/DR, troubleshooting). User docs (dashboard, agent deploy, monitoring, patch mgmt, scripts, A2A config, remote access). Doc infra (MkDocs Material or Docusaurus, GitHub Pages auto-deploy, versioned docs, OpenAPI generation, search). Inline doc standards (Python Google-style, Go godoc, TypeScript TSDoc, OpenAPI 3.1).`,

  'commercial': `Plan Commercial Tier: BSL 1.1 LICENSE file with change date. CONTRIBUTING.md with agreement. Code boundary (open vs proprietary). Build system (community vs enterprise). Feature flags system. Runtime license validation (key + signature). Graceful degradation. Feature gating UI. Multi-tenancy (tenant model, schema vs row isolation, provisioning, NATS subjects, billing/Stripe). Managed A2A relay (cloud relay, cross-network discovery, auth, usage metering). Enterprise reporting (templates, scheduled delivery, aggregation, PDF/HTML). SSO/RBAC extensions (SAML SP, OIDC RP, SCIM). Billing (license key gen, per-endpoint subscription, per-agent metering, Stripe Billing, usage dashboard).`,
}

const crossCutPlans = await parallel(Object.entries(crossCutSpecs).map(([area, spec]) => () =>
  agent(`You are a senior systems architect planning the "${area}" cross-cutting concern for OpenAgentPlatform.

${spec}

Provide exhaustive implementation details: file paths, data models, middleware, configuration, tests, step-by-step implementation sequence.`, {
    label: `plan:${area}`,
    phase: 'Cross-Cut',
    model: 'sonnet',
  })
))

log(`Cross-cut plans complete: ${crossCutPlans.filter(Boolean).length} areas planned`)

// Phase 4: Integration — sonnet for integration mapping
phase('Integration')

const integrationPlan = await agent(`Map ALL integration points between OpenAgentPlatform subsystems based on these domain plans:

${domainPlans.filter(Boolean).map((p, i) => `--- Domain ${i}: ${p.slice(0, 200)}...`).join('\n')}

Specify:
1. Service Communication Map: which services talk to which, via what protocol (REST, NATS, gRPC, A2A), what data flows
2. Event Flow Map: every NATS subject, publisher, subscriber, trigger, action
3. Data Flow Diagrams for: (a) agent check-in→check failure→alert→A2A task→LLM triage→remediation, (b) patch scan→approval→A2A evaluation→install, (c) secret reference→injection→execution→revocation, (d) A2A LangGraph→CrewAI delegation→result
4. Shared Schemas crossing service boundaries (agent event, A2A message/artifact, secret reference, auth token)
5. Error Propagation: what happens when NATS/A2A/Vault/LLM is down
6. Consistency Patterns: idempotency, saga, eventual consistency per operation`, {
  label: 'plan:integration',
  phase: 'Integration',
  model: 'sonnet',
})

log('Integration plan complete')

// Phase 5: Sequencing — sonnet for roadmap planning
phase('Sequencing')

const sequencingPlan = await agent(`Create a phased implementation roadmap for OpenAgentPlatform based on the domain plans above.

Define:
1. Dependency Graph - topological ordering of all components
2. Phase definitions:
   - Phase 0 (Foundation): scaffolding, DB, NATS, auth base, agent binary MVP
   - Phase 1 (Core RMM): checks, alerts, policies, patches, scripts, remote access
   - Phase 2 (A2A + Agents): gateway, framework adapters, process pool, event-to-task bridge
   - Phase 3 (Secret Mgmt): Vault, Infisical, secret references, credential injection
   - Phase 4 (Frontend): full React UI, dashboards, real-time
   - Phase 5 (Production): observability, load testing, security audit, docs, CI/CD
   - Phase 6 (Commercial): feature gating, multi-tenancy, relay, reporting, billing

3. For Phase 0 and Phase 1, break into 2-week sprints with:
   - Sprint goals
   - User stories (As a [role], I want [feature], so that [benefit]) with acceptance criteria
   - Complexity (S/M/L/XL)
   - Work stream (backend/frontend/devops/agent)

4. Parallel work streams (backend/agent/frontend/infra)
5. Release strategy (Alpha after Phase 1, Beta after Phase 3, GA after Phase 5, Commercial after Phase 6)`, {
  label: 'plan:sequencing',
  phase: 'Sequencing',
  model: 'sonnet',
})

log('Sequencing plan complete')

// Phase 6: Synthesize — full model only for the final master document
phase('Synthesize')

const masterPlan = await agent(`You are a senior engineering leader synthesizing the COMPLETE implementation plan for OpenAgentPlatform into a single, exhaustive markdown document.

The document structure MUST be:

# OpenAgentPlatform — Master Implementation Plan

## 1. Project Overview (mission, scope, success metrics)
## 2. Architecture Overview (system topology, service inventory, data flow)
## 3. Domain Implementation Specifications
### 3.1 RMM Core (all models, APIs, services, tests, steps)
### 3.2 A2A Protocol (gateway, bindings, task lifecycle, auth)
### 3.3 Agent Framework Adapters (all 6 adapters, process pool, orchestration)
### 3.4 Secret Management (Vault, Infisical, injection, safety)
### 3.5 Endpoint API (all endpoints, NATS, agent binary)
### 3.6 Frontend (all pages, components, state management)
### 3.7 Infrastructure (Docker, K8s, CI/CD, observability stack)
## 4. Cross-Cutting Specifications
### 4.1 Auth & RBAC
### 4.2 Testing Strategy
### 4.3 Observability
### 4.4 Documentation
### 4.5 Commercial Tiering
## 5. Integration Specifications (service map, events, schemas, errors, consistency)
## 6. Phased Roadmap (phases 0-6, sprints for Phase 0+1, parallel streams, releases)
## 7. Risk Register
## 8. Open Questions
## 9. Appendices (file tree, package manifest, glossary)

DOMAIN PLANS:
${domainPlans.filter(Boolean).join('\n\n---\n\n')}

CROSS-CUT PLANS:
${crossCutPlans.filter(Boolean).join('\n\n---\n\n')}

INTEGRATION PLAN:
${integrationPlan}

SEQUENCING PLAN:
${sequencingPlan}

Write the COMPLETE plan. Every component, every file, every API endpoint, every test case, every implementation step. This is the definitive blueprint. Do NOT summarize or abbreviate — be exhaustive.`, {
  label: 'synthesize:master-plan',
  phase: 'Synthesize',
  // Default model for final synthesis — this is the one place we need full capability
})

return masterPlan