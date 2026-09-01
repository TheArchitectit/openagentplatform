# oap-relay — Managed A2A Relay

The managed A2A relay lets agents in different tenants or networks reach each
other without direct connectivity. It is a dedicated binary (`cmd/relay`) that
runs WSS rendezvous; it is **not** wired into `cmd/server` (W8 decision).

## Quick Start

```bash
oap-relay \
  -listen :7000 \
  -cert /etc/oap/relay/server.crt \
  -key /etc/oap/relay/server.key \
  -trust-ca /etc/oap/relay/ca.pem \
  -trust-config /etc/oap/relay/trust.yaml \
  -max-connections 1000 \
  -idle-timeout 30m
```

## Configuration

| Flag | Env Override | Default | Description |
|------|-------------|---------|-------------|
| `-listen` | `RELAY_LISTEN_ADDR` | `:7000` | WSS listen address |
| `-cert` | — | (required) | TLS certificate file for WSS |
| `-key` | — | (required) | TLS key file for WSS |
| `-admin-addr` | — | `127.0.0.1:9090` | Admin listener bind address (loopback by default) |
| `-trust-ca` | — | (required) | Platform CA cert PEM for admin mTLS |
| `-trust-config` | — | `""` | Issued-identity trust config path (RELAY-02) |
| `-max-connections` | `RELAY_MAX_CONNECTIONS` | `1000` | Per-tenant max concurrent connections (0 = unlimited) |
| `-idle-timeout` | `RELAY_IDLE_TIMEOUT` | `30m` | Idle connection reaping window |
| `-store-dsn` | `RELAY_STORE_DSN` | `""` | Postgres DSN for durable state (a2a-relay §8); empty = in-memory |

### Durable state (optional)

Set `-store-dsn` (or `RELAY_STORE_DSN`) to persist the connection ledger and
per-tenant billing meters to PostgreSQL. The relay applies the platform's
embedded migration set (tables `relay_connections` + `relay_metrics`) to that
database itself at boot. What survives a restart: connection records (final
byte counts, establish/close ledger) and lifetime billing aggregates. What
does not: live legs — a restart drops sockets by design; the store is a
billing/audit record, not a session cache. Byte metering flushes every 30s
and at shutdown (a crash loses at most one flush interval).

## Trust Config

The `-trust-config` flag points to a YAML file containing the issued-identity
trust configuration. This is consumed in RELAY-02 when mTLS + bearer-token
admission is implemented. Until then, the relay accepts WSS upgrades but closes
every session without registration (fail-closed).

## Admin API

The relay exposes an operator observability surface on a separate mTLS listener
(`-admin-addr`, default `127.0.0.1:9090`). Every route requires a valid client
certificate from the platform CA (`-trust-ca`).

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/admin/health` | Any mTLS client | Liveness + readiness |
| GET | `/admin/metrics` | Role SAN required | Tenant-scoped metrics |

### Role-Based Visibility

Client certificates carry SANs following the `oap:` convention:

| Role SAN | Visibility |
|----------|------------|
| `oap:role:relay-admin` | All tenants |
| `oap:role:relay-operator` | Only `oap:tenant:*` SANs in the cert |

Operators querying a tenant not in their SAN list receive 403 Forbidden.

## Deployment

See `deploy/relay/systemd/relay.service` for the systemd unit. The relay
terminates TLS itself; no reverse-proxy TLS termination is needed.

## Architecture

- **R.1:** WSS rendezvous (agents connect over WSS; relay matches legs). No raw
  TCP forwarder.
- **R.2:** Dedicated `cmd/relay` binary, NOT wired into `cmd/server` (W8).
- **R.3:** mTLS principal extraction + rendezvous admission (RELAY-03).
- **R.4:** Symmetric matching, bidirectional frame forwarding (RELAY-03).
- **I.3:** Identity admission (mTLS + bearer token) — RELAY-02.
- **D.2:** Discovery federation (gRPC) — RELAY-05.
- **E.4:** Blind forwarder (relay never sees decrypted payloads) — RELAY-06.
