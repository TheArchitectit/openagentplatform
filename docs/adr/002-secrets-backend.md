# ADR 002: Secrets use a backend abstraction

- **Status:** Accepted
- **Date:** 2026-08-22

## Context

Deployments have different secret-management capabilities. Local development may use process-provided values, while production environments may use a managed vault or platform-native secret store. Binding application code to one vendor would complicate testing, migration, and deployment portability.

Secret material must not be persisted in application configuration, logs, or test fixtures.

## Decision

Define a narrow secret backend interface in the domain-facing boundary. The interface covers retrieval and the lifecycle operations the platform actually needs, including health checks where readiness depends on the backend.

Implement backends as infrastructure adapters selected through explicit configuration. Callers depend on the interface rather than vendor SDKs. Backend implementations return descriptive wrapped errors but must never include secret values. Tests use a dedicated in-memory fake with synthetic values; production backends and credentials are never used by automated tests.

Caching, rotation, authorization, and audit behavior remain backend concerns unless a cross-backend semantic is explicitly added to the interface.

## Consequences

- Deployments can choose a backend without changing domain logic.
- Tests remain deterministic and isolated from production secret systems.
- The abstraction limits vendor-specific capabilities to adapter code.
- Interface changes require review because every backend must preserve consistent semantics.
- Operators must configure the backend explicitly and monitor backend readiness.

## Alternatives considered

- **One managed-vault implementation:** Reduces initial code but introduces vendor coupling and makes local development harder.
- **Environment variables accessed throughout the codebase:** Simple but distributes parsing, validation, and lifecycle concerns across callers.
- **Database-backed secrets:** Rejected because it creates an additional high-value secret store and complicates encryption-key bootstrapping.
