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
| `-trust-config` | — | `""` | Issued-identity trust config path (RELAY-02) |
| `-max-connections` | `RELAY_MAX_CONNECTIONS` | `1000` | Per-tenant max concurrent connections (0 = unlimited) |
| `-idle-timeout` | `RELAY_IDLE_TIMEOUT` | `30m` | Idle connection reaping window |

## Trust Config

The `-trust-config` flag points to a YAML file containing the issued-identity
trust configuration. This is consumed in RELAY-02 when mTLS + bearer-token
admission is implemented. Until then, the relay accepts WSS upgrades but closes
every session without registration (fail-closed).

## Deployment

See `deploy/relay/systemd/relay.service` for the systemd unit. The relay
terminates TLS itself; no reverse-proxy TLS termination is needed.

## Architecture

- **R.1:** WSS rendezvous (agents connect over WSS; relay matches legs). No raw
  TCP forwarder.
- **R.2:** Dedicated `cmd/relay` binary, NOT wired into `cmd/server` (W8).
- **I.3:** Identity admission (mTLS + bearer token) — RELAY-02.
- **D.2:** Discovery federation (gRPC) — RELAY-05.
- **E.4:** Blind forwarder (relay never sees decrypted payloads) — RELAY-06.
