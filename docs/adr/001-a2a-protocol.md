# ADR 001: A2A protocol uses gRPC and SSE

- **Status:** Accepted
- **Date:** 2026-08-22

## Context

OpenAgentPlatform needs an agent-to-agent transport for typed service calls and long-lived task updates. Internal components require efficient, strongly typed request/response communication. Browser and HTTP clients need updates over infrastructure that works with standard proxies and does not require bidirectional messaging.

Using one transport for both concerns would either weaken the internal contract or add browser-facing protocol complexity.

## Decision

Use gRPC with Protocol Buffers for service-to-service A2A commands, queries, and streaming where both peers support HTTP/2. Treat the protobuf definitions as the canonical typed RPC contract.

Use Server-Sent Events (SSE) for browser-compatible, server-to-client task and status updates. SSE endpoints expose ordered event identifiers and heartbeats so clients can detect interruption and resume with `Last-Event-ID` where the backing event stream supports replay.

Keep transport handlers thin. Domain behavior and task state transitions remain transport-independent, and errors are translated into the conventions of each protocol.

## Consequences

- Internal calls receive generated types, compatibility checks, and efficient streaming.
- Web clients can consume live updates with native browser APIs and normal HTTP infrastructure.
- The platform maintains two transport adapters and must test contract parity between them.
- SSE is intentionally one-way; client commands continue through request/response APIs.
- Protocol changes require backward-compatible protobuf evolution and documented SSE event schemas.

## Alternatives considered

- **WebSockets for all streaming:** Supports bidirectional traffic but adds connection and proxy complexity not needed by the browser update path.
- **REST and polling only:** Simple, but increases update latency and request load.
- **gRPC-Web only:** Preserves protobuf contracts but requires additional browser and proxy tooling and does not remove the need for HTTP compatibility decisions.
