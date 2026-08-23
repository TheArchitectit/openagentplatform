# A2A Agent Registry

> **Phase:** 2 (A2A + Agents)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/A2A_PROTOCOL.md` §3, §6, §12
> **App Path:** `a2a/registry/registry.go`, `a2a/models/` (agent card types)

---

## Description

The Agent Registry is the catalogue of agents the platform knows how to reach.
Each entry is an **Agent Card** — a self-describing document naming the agent's
framework, endpoint, capabilities, skills, tags, and authentication
requirements. Without the registry, the gateway has no way to answer "who can
handle a task tagged `patch.planning`?"

The registry is deliberately two-tiered: an in-memory map serves the read-heavy
routing path (every routed task hits it), while PostgreSQL provides durability
via periodic snapshots. This is the right trade because agent cards change
rarely and are read constantly. Cold-start rehydration comes from the database;
steady-state lookups never touch it.

Discovery follows the A2A convention of publishing each agent's card at
`/.well-known/agent-card.json`. The registry consumes those documents, validates
them, and exposes skill-based lookup to the GatewayRouter.

## User Story

**As** the gateway router deciding where to send a task,
**I want** a validated, queryable catalogue of every registered agent's skills,
capabilities, and endpoint,
**so that** I can select a capable agent by skill tag in memory-speed time,
and so that operators can register a new framework adapter without redeploying
the gateway.

---

## Requirements

### 1. Agent Card Schema

1.1. Each registry entry MUST carry these fields:

```go
type AgentCard struct {
    ID             string
    Name           string
    Description    string
    Version        string
    Framework      string   // langgraph|crewai|autogen|semantickernel|openai|anthropic
    Endpoint       string   // http://adapter-{framework}:8090/a2a
    Capabilities   []string // streaming, pushNotifications, stateTransitionHistory
    Tags           []string // alert.triage, patch.planning, security-scanning
    Skills         []AgentSkill
    Authentication AgentAuth
}
```

1.2. `ID` MUST be unique across the registry and stable across the agent's
lifetime; it is the tie-break key for deterministic routing.

1.3. `Framework` MUST be one of the six supported values. An unrecognized
framework MUST be rejected at registration.

1.4. `Capabilities` MUST be drawn from the known capability vocabulary
(`streaming`, `pushNotifications`, `stateTransitionHistory`). Unknown
capabilities MUST NOT be silently accepted, because the gateway makes routing
and streaming decisions from them.

### 2. Card Storage

2.1. The registry MUST maintain an in-memory map of Agent Cards as the
authoritative read path for routing.

2.2. The registry MUST snapshot cards to the `a2a_agent_cards` PostgreSQL table
periodically:

| Table | Key Columns | Indexes |
|-------|-------------|---------|
| `a2a_agent_cards` | id, name, framework, endpoint, capabilities (jsonb), tags (jsonb), skills (jsonb), auth_config (jsonb) | GIN on `tags`, GIN on `capabilities` |

2.3. GIN indexes on `tags` and `capabilities` MUST exist so database-side
skill queries remain viable for reporting and cold-path lookups.

2.4. On startup the registry MUST rehydrate its in-memory map from PostgreSQL
before accepting routing requests, so a restart does not blackhole tasks.

2.5. Routing lookups MUST NOT require a database round trip in steady state.

### 3. CRUD Operations

3.1. The registry MUST support create, read, update, and delete of Agent Cards.

3.2. Creating a card with an existing `ID` MUST either update in place or fail
explicitly — it MUST NOT create a duplicate entry.

3.3. Deleting a card MUST remove it from both the in-memory map and the
database snapshot, and MUST take effect for routing immediately.

3.4. Updating a card MUST be atomic from the router's perspective: a concurrent
lookup MUST observe either the old card or the new one, never a partially
mutated card.

### 4. Discovery

4.1. Each agent MUST publish its card at `/.well-known/agent-card.json`.

4.2. The gateway MUST expose `GET /a2a/v1/agents/{id}/card` publicly (no
credentials), because card discovery precedes authentication negotiation.

4.3. The gateway MUST expose `GET /a2a/v1/agents` (Bearer-authenticated) to list
all registered agents.

4.4. `GetExtendedAgentCard` MUST return authenticated-only card detail for
callers presenting valid credentials, allowing agents to withhold sensitive
skill or endpoint information from anonymous discovery.

### 5. Validation

5.1. Registration MUST validate that: `ID`, `Name`, `Version`, `Framework`, and
`Endpoint` are present and non-empty; `Framework` is a supported value;
`Endpoint` is a well-formed URL; `Capabilities` are recognized; and `Skills`
entries are well-formed.

5.2. Validation failures MUST be rejected with a structured error naming the
offending field. A partially valid card MUST NOT be partially registered.

5.3. The registry MUST NOT trust card contents for authorization decisions;
`Authentication` describes what the agent requires of callers, not what the
agent is permitted to do.

### 6. Skill-Based Lookup

6.1. The registry MUST support lookup of candidate agents by required skill tag.

6.2. Lookup MUST return all matching candidates, leaving selection and scoring
to the GatewayRouter — the registry ranks nothing.

6.3. Lookup by a skill tag no agent provides MUST return an empty candidate set,
distinguishable from an error, so the router can raise a precise "no capable
agent" failure.

### 7. Load Signal for Routing

7.1. The registry MUST expose a current-load signal per agent, consumed by the
router's scoring formula
(`score = 1.0 + matching_tags × 0.1 − current_load × 0.05`).

7.2. The load signal MUST reflect in-flight task count for that agent.

7.3. A stale or unavailable load signal MUST degrade to zero load rather than
excluding the agent from routing, so a metrics failure does not cause a
platform-wide routing outage.

### 8. Operational Requirements

8.1. Registry contents MUST be observable — operators MUST be able to list
registered agents with their frameworks, endpoints, and current load.

8.2. Registration and deregistration MUST produce audit records.

8.3. Card registration MUST be verified end-to-end by integration tests that
register a card, route a task by its skill tag, and confirm delivery to the
declared endpoint.
