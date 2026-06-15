export const meta = {
  name: 'deep-research-openagentplatform',
  description: 'Deep research on designing an open-source agent-first RMM platform with A2A, LLM agent support, endpoint management, and secret management',
  phases: [
    { title: 'Scope', detail: 'Decompose research question into 5 search angles' },
    { title: 'Search', detail: '5 parallel WebSearch agents, one per angle' },
    { title: 'Fetch', detail: 'Fetch top sources and extract falsifiable claims' },
    { title: 'Verify', detail: '3-vote adversarial verification per claim' },
    { title: 'Synthesize', detail: 'Merge semantic dupes, rank by confidence, cite sources' },
  ],
}

const RESEARCH_QUESTION = args || 'Design an open-source agent-first RMM platform that supports all LLM agent platforms, A2A protocol, full endpoint management, top open-source secret management, with a future commercial tier.';

// Phase 1: Scope — decompose into 5 search angles
phase('Scope')
const angles = [
  {
    key: 'rmm-core',
    prompt: `Research the core capabilities and architecture of modern RMM (Remote Monitoring and Management) platforms. What are the essential features every RMM must have? Include: endpoint monitoring, patch management, remote access, alerting, scripting/automation, reporting, asset inventory, network topology. Look at open-source RMM tools like Tactical RMM, MeshCentral, and commercial ones like NinjaOne, Datto, ConnectWise. Focus on: data models, API patterns, agent architecture (how endpoints report back), and event-driven patterns.`
  },
  {
    key: 'agent-a2a',
    prompt: `Research the A2A (Agent-to-Agent) protocol by Google for inter-agent communication. What is the protocol specification, how does it work, what are the key primitives (Agent Cards, tasks, messages, artifacts)? Also research how major LLM agent platforms (LangChain/LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, Anthropic Claude Agent SDK) expose agent interfaces. How would you build a platform that natively supports all of these? Focus on: protocol schemas, transport layers, discovery mechanisms, and authentication patterns.`
  },
  {
    key: 'secret-management',
    prompt: `Research the top open-source secret management platforms and how they integrate with infrastructure automation tools. Cover: HashiCorp Vault, Infisical, Conjur, SOPS, Doppler (open-source components). How do these platforms expose APIs for secret retrieval? What are their agent/sidecar patterns? How do RMM or similar platforms integrate with secret managers (injecting secrets into scripts, endpoint configurations, API keys)? Focus on: API patterns, dynamic secrets, PKI, and rotation capabilities.`
  },
  {
    key: 'endpoint-api',
    prompt: `Research how modern platforms expose full endpoint APIs for device management and observability. Look at: osquery (SQL-based endpoint visibility), Fleet (osquery fleet management), SaltStack, Ansible (as an API-driven endpoint manager), and how platforms like Fleet dm expose their APIs. What API patterns (REST, GraphQL, gRPC) work best for endpoint management at scale? How do you design an endpoint schema that covers Windows, macOS, Linux, and network devices? Focus on: API design, real-time streaming of endpoint events, and scalable architecture patterns.`
  },
  {
    key: 'open-source-monetization',
    prompt: `Research successful open-source RMM and IT management platforms and their commercial models. How do platforms like Tactical RMM, Uptime Kuma, Zabbix, Netdata, and Fleet dm handle open-source vs commercial tiers? What features are typically gated behind paid tiers (multi-tenancy, SSO, advanced reporting, compliance, priority support)? How do platforms built on agent architectures monetize — per-endpoint pricing, per-agent pricing, usage-based? Also research: BSL (Business Source License) vs AGPL vs MIT for open-core RMM platforms, and campground licensing patterns.`
  },
]

log(`Decomposed into ${angles.length} search angles: ${angles.map(a => a.key).join(', ')}`)

// Phase 2: Search — 5 parallel WebSearch agents
phase('Search')
const searchResults = await parallel(angles.map(angle => () =>
  agent(angle.prompt, {
    label: `search:${angle.key}`,
    phase: 'Search',
    schema: {
      type: 'object',
      properties: {
        angle: { type: 'string' },
        findings: {
          type: 'array',
          items: {
            type: 'object',
            properties: {
              claim: { type: 'string', description: 'A specific, falsifiable claim' },
              source: { type: 'string', description: 'Where this claim came from (URL or reference)' },
              detail: { type: 'string', description: 'Supporting detail or context' },
            },
            required: ['claim', 'source', 'detail'],
          },
        },
        key_references: {
          type: 'array',
          items: { type: 'string' },
          description: 'Key URLs or references to fetch later',
        },
      },
      required: ['angle', 'findings', 'key_references'],
    },
  })
))

log(`Search complete: ${searchResults.filter(Boolean).reduce((sum, r) => sum + r.findings.length, 0)} claims collected`)

// Phase 3: Fetch — deduplicate URLs and fetch top sources
phase('Fetch')
const allUrls = searchResults.filter(Boolean).flatMap(r => r.key_references || [])
const uniqueUrls = [...new Set(allUrls)].slice(0, 15)

log(`Fetching ${uniqueUrls.length} unique sources`)

const fetchedSources = await parallel(uniqueUrls.map(url => () =>
  agent(`Fetch and extract key technical claims from: ${url}. Focus on: architecture details, API specifications, protocol details, integration patterns, and licensing models. Return the most important falsifiable claims with supporting detail.`, {
    label: `fetch:${url.split('/').slice(-2).join('/')}`,
    phase: 'Fetch',
    schema: {
      type: 'object',
      properties: {
        url: { type: 'string' },
        claims: {
          type: 'array',
          items: {
            type: 'object',
            properties: {
              claim: { type: 'string' },
              detail: { type: 'string' },
              confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
            },
            required: ['claim', 'detail', 'confidence'],
          },
        },
        summary: { type: 'string' },
      },
      required: ['url', 'claims', 'summary'],
    },
  })
))

log(`Fetch complete: ${fetchedSources.filter(Boolean).length} sources processed`)

// Phase 4: Verify — 3-vote adversarial verification per claim
phase('Verify')
const allClaims = searchResults.filter(Boolean).flatMap(r => r.findings)
  .concat(fetchedSources.filter(Boolean).flatMap(s => (s.claims || []).map(c => ({ ...c, source: c.source || s.url }))))
  .filter(c => c.claim && c.claim.length > 10)

// Deduplicate similar claims
const seenClaims = new Set()
const uniqueClaims = allClaims.filter(c => {
  const key = c.claim.toLowerCase().slice(0, 100)
  if (seenClaims.has(key)) return false
  seenClaims.add(key)
  return true
}).slice(0, 25) // Cap at 25 most important claims for verification

log(`Verifying ${uniqueClaims.length} unique claims with 3-vote adversarial check`)

const verifiedClaims = await parallel(uniqueClaims.map(claim => () =>
  parallel([1, 2, 3].map(voteNum => () =>
    agent(`You are a skeptical fact-checker. Your job is to REFUTE the following claim if possible. Search the web to find evidence against it. If you find strong contradicting evidence, mark it as refuted. If the claim is plausible and consistent with known facts, mark it as confirmed. Be especially skeptical of specific technical claims about protocols, APIs, and architectures.\n\nClaim: "${claim.claim}"\nSource: ${claim.source || 'unknown'}\nDetail: ${claim.detail || 'none'}\n\nIs this claim accurate?`, {
      label: `verify:vote${voteNum}:${claim.claim.slice(0, 40)}`,
      phase: 'Verify',
      schema: {
        type: 'object',
        properties: {
          refuted: { type: 'boolean', description: 'true if you found evidence against this claim' },
          reasoning: { type: 'string', description: 'Your reasoning with any sources' },
          correction: { type: 'string', description: 'If refuted, what is the correct version?' },
        },
        required: ['refuted', 'reasoning'],
      },
    })
  )).then(votes => {
    const refutedCount = votes.filter(Boolean).filter(v => v.refuted).length
    const survives = refutedCount < 2 // Need 2/3 refutes to kill
    return {
      claim: claim.claim,
      source: claim.source,
      detail: claim.detail,
      survives,
      confidence: survives ? (refutedCount === 0 ? 'high' : 'medium') : 'low',
      votes: votes.filter(Boolean).map(v => ({ refuted: v.refuted, reasoning: v.reasoning, correction: v.correction })),
    }
  })
))

log(`Verification complete: ${verifiedClaims.filter(c => c && c.survives).length}/${verifiedClaims.filter(Boolean).length} claims survived`)

// Phase 5: Synthesize — merge, rank, cite
phase('Synthesize')
const survivingClaims = verifiedClaims.filter(Boolean).filter(c => c.survives)

const synthesis = await agent(`You are a senior systems architect writing a comprehensive research report for building an open-source, agent-first RMM (Remote Monitoring and Management) platform. The platform must:

1. Be agent-first (LLM agents, not just monitoring agents) — natively support multiple LLM agent frameworks (LangChain, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, Anthropic Claude Agent SDK)
2. Support the A2A (Agent-to-Agent) protocol for inter-agent communication
3. Expose a full endpoint API for device management and observability (Windows, macOS, Linux)
4. Integrate with top open-source secret management platforms (Vault, Infisical, etc.)
5. Start open-source with a path to a commercial tier (open-core model)

Based on the verified research findings below, write a detailed architectural design report with:

## Executive Summary
## Core Architecture (agent-first design principles)
## RMM Essential Capabilities Matrix (what every RMM must do, mapped to our agent-first approach)
## A2A Protocol Integration (how A2A works, how we implement it)
## Multi-Agent Platform Support (how we abstract across LangChain/CrewAI/AutoGen/etc.)
## Endpoint API Design (schemas, transport, real-time events)
## Secret Management Integration (Vault, Infisical, patterns)
## Open-Core Commercial Strategy (license choice, tiering, monetization)
## Technology Stack Recommendations
## Risks and Mitigations
## References

VERIFIED CLAIMS (use these as the factual foundation):
${JSON.stringify(survivingClaims, null, 2)}

Be concrete — include specific API patterns, data models, protocol details, and implementation strategies. Cite sources inline as [source: URL]. When claims were corrected during verification, use the corrected version.`, {
  label: 'synthesize-report',
  phase: 'Synthesize',
  schema: {
    type: 'object',
    properties: {
      title: { type: 'string' },
      executive_summary: { type: 'string' },
      core_architecture: { type: 'string' },
      rmm_capabilities_matrix: { type: 'string' },
      a2a_integration: { type: 'string' },
      multi_agent_support: { type: 'string' },
      endpoint_api_design: { type: 'string' },
      secret_management: { type: 'string' },
      commercial_strategy: { type: 'string' },
      technology_stack: { type: 'string' },
      risks_and_mitigations: { type: 'string' },
      references: {
        type: 'array',
        items: {
          type: 'object',
          properties: {
            title: { type: 'string' },
            url: { type: 'string' },
            relevance: { type: 'string' },
          },
        },
      },
    },
    required: ['title', 'executive_summary', 'core_architecture', 'rmm_capabilities_matrix', 'a2a_integration', 'multi_agent_support', 'endpoint_api_design', 'secret_management', 'commercial_strategy', 'technology_stack', 'risks_and_mitigations', 'references'],
  },
})

return synthesis