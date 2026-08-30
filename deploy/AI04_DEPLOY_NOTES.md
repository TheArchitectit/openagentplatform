# AI04 Test Deploy — Field Notes & Runbook

**Deploy date:** 2026-08-29/30 · **Host:** ai04 (RHEL 10, x86_64, docker + compose v5.0.2) · **Location on host:** `~/oap`
**Status:** all five containers healthy; full OIDC login flow verified scripted end-to-end.

This document records what the compose stack needed to actually come up on a
real, shared, no-Go-toolchain box — every deviation found during the ai04
deploy, which ones are now folded back into the repo (with the commit that
fixed it), and what remains open. Read this before deploying to any host that
already runs other stacks.

---

## 1. Quick start (what works today)

```bash
git clone <repo> ~/oap && cd ~/oap
cp .env.example .env
# 1. Fill every "change-me" in .env with real secrets: openssl rand -hex 32
# 2. Mint the NATS mTLS material (compose server mounts these):
./deploy/nats/scripts/gen-certs.sh          # writes deploy/nats/certs/*.pem
# 3. Create the database schema (idempotent, apply in order):
docker compose -p oap -f deploy/docker-compose.yml --env-file .env up -d postgres
for f in deploy/migrations/00{1,2,3}_*.sql; do
  docker exec -i $(docker ps -qf name=postgres-1) psql -U oap -d oap < "$f"
done
# 4. Bring up the rest:
docker compose -p oap -f deploy/docker-compose.yml --env-file .env up -d
docker compose -p oap -f deploy/docker-compose.yml ps        # 5x healthy
```

> **`-p oap` is not optional.** See §3.1 — the default project name collides
> with other stacks on shared hosts and `down -v` under the wrong name
> destroys someone else's data.

Ports (all loopback-bound): `15432`-style pg remap per §3.2, NATS 4222/8222,
dex 5556, server 8080, web 5173. Smoke battery:

```bash
curl -s http://localhost:8080/healthz                 # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:5173/    # 200 (SPA)
curl -s http://localhost:5173/api/v1/a2a/approvals    # 401 {"error":"unauthorized"} (proxy works)
curl -s http://localhost:8222/healthz                 # {"status":"ok"}
docker logs deploy-server-1 2>&1 | grep -E "nats client ready|approval engine"
```

Scripted login (no browser needed): `deploy/scripts/test-login.sh` — follows
server `/auth/login` → dex static-user form → consent approve → code →
callback, prints the `oap_session` cookie result and exercises two authed
endpoints.

**Migration on an existing deployment:** compose does NOT run the
`deploy/migrations/` SQL automatically (postgres' `docker-entrypoint-initdb.d`
only seeds `init.sql`, and only on an empty volume). Every fresh host must
apply 001→003 manually as above; an already-running stack upgrades in place:

```bash
docker exec -i <postgres-container> psql -U oap -d oap < deploy/migrations/003_platform_schema_addendum2.sql
```

## 2. Verified working at deploy time

| Component | Image | Result |
|---|---|---|
| postgres | timescale/timescaledb:2.17.2-pg16 | healthy; migrations 001–003 applied |
| nats | nats:2.10.22-alpine | healthy; mTLS client (`nats client ready`), JetStream |
| dex | ghcr.io/dexidp/dex:v2.40.0 | healthy; static users `admin@oap.local`/`tech@oap.local`, password `password` (test-only) |
| server | built from Dockerfile.server | healthy; HITL engine (`types:6`), a2a registry loaded, OIDC session JWT issued on scripted login |
| web | built from Dockerfile.web | healthy; SPA 200; `/api/`,`/auth/`,`/a2a/` proxy → server |

## 3. Defects found → fixed in repo

### 3.1 Compose project-name collision (HAZARD)
`-f deploy/docker-compose.yml` without `-p` derives the project name from the
**file's directory** ("deploy"). On ai04 the guardrail stack already owns
`deploy_redis_data`; a `down -v` from `~/oap` would have deleted it. Fixed:
compose header pins `-p oap`; never run `down -v` without checking
`docker volume ls` first.

### 3.2 Postgres/NATS port collisions
ai04 holds 5432 (guardrail-postgres). The stack now binds loopback-only
(`127.0.0.1:` prefix) for pg 5432 / NATS 4222+8222; remap the left side when
the port is taken (ai04 uses `127.0.0.1:15432:5432`).

### 3.3 `Dockerfile.web` shipped no API proxy
Bare nginx config served the SPA only; the built web app calls relative
`/api/v1` and the Go server has no CORS middleware → a browser could never
reach the API. Fixed: `/api/`, `/auth/`, `/a2a/` `proxy_pass` to
`http://server:8080`. **The web container must always run same-origin-proxied;
that is the supported topology.**

### 3.4 NATS config incompatible with pinned nats 2.10.22
Four separate startup/auth failures, all fixed in `deploy/nats/nats.conf`:
1. `log_level` / `logtime` are CLI flags, not config fields → server refused
   to start (`unknown field log_level`). Removed with a comment.
2. `cluster { listen; routes = [] }` + JetStream → JS clustering requires
   routes. Block commented out for single-node; uncomment WITH routes for HA.
3. Top-level `authorization { users = [] }` → every CONNECT rejected
   ("Authorization Violation"). Removed.
4. `verify_and_map: true` overrode URL credentials with the cert identity
   (CN `oap-nats.…` ≠ any user) → auth failed. Now `false` with a comment:
   revert to `true` only under full SPIRE-identity rollout.
5. During the auth hunt, ai04's copy ended up with LITERAL passwords baked
   into nats.conf. This turned out to be belt-and-braces, not required: the
   real root causes were 3 and 4, and `$VAR` expansion works fine in 2.10.22
   provided the nats container receives the env vars (compose passes all
   four). **The repo file keeps `$VAR` placeholders** — do NOT bake secrets
   into a committed config; rotate `.env` on the host instead.

### 3.5 Dex startup: entrypoint + trustedPeers
* `command: ["serve", …]` → "serve: executable not found" (image has an
  entrypoint script); must be `["dex", "serve", "/etc/dex/config.yaml"]`.
  Fixed in compose.
* `trustedPeers` held `{id, secret}` objects; dex v2.40 wants a list of
  client-ID strings → startup failed (`error unmarshaling 'trustedPeers'`).
  The block was decorative (oap-server is a separate static client) —
  removed with a comment showing the correct form.

### 3.6 Server NATS TLS material missing from compose
nats.conf requires mTLS (`verify: true`), but the server service had no
`NATS_URL=tls://oap-server:…` and no client cert mounts → server exited
`nats connect failed`. Fixed: compose sets the TLS URL + `NATS_CA_FILE/
CERT_FILE/KEY_FILE` env and mounts `deploy/nats/certs/
{oap-server-client-cert,oap-server-client-key,ca}.pem`; `gen-certs.sh` now
mints the `oap-server-client-*.pem` pair (`extendedKeyUsage=clientAuth`).

### 3.7 a2a tables absent on a fresh DB (server init crash)
`a2a/registry/store.go` and `a2a/manager/store_types.go` define DDL
constants but **never call EnsureSchema at startup**, and `deploy/migrations`
omitted the tables → fresh DB: `relation "a2a_agent_cards" does not exist`,
server restart-loop. Fixed: `deploy/migrations/003_platform_schema_addendum2.sql`
carries the DDL (extracted verbatim from the Go constants). The deeper fix —
invoking the existing schema constants from the stores at boot — is still
open (§5.2).

### 3.8 Policy seeder fails on fresh DB
`internal/policy/store_crud.go` scopes every read with `deleted = false`,
but migration 001 created `policies` without that column →
`policy seeder failed … column "deleted" does not exist`, no default
policies on a fresh install. Fixed: migration 003 adds
`ALTER TABLE policies ADD COLUMN IF NOT EXISTS deleted BOOLEAN NOT NULL DEFAULT false`.

### 3.9 Web healthcheck "unhealthy" while serving 200s
`wget --spider http://localhost/…` resolves IPv6-first inside the container;
nginx listens IPv4-only → docker reported `deploy-web-1 unhealthy` despite
every request succeeding. Fixed: healthchecks (compose + Dockerfile.web)
use `127.0.0.1`.

### 3.10 Post-login redirect loop
`OIDC_REDIRECT_URL` served triple duty: dex redirect_uri, token-exchange
redirect_uri, AND the post-login browser redirect. The callback therefore
302'd the browser back to `/auth/callback` (no `code`) → 400. Fixed: new
`POST_LOGIN_REDIRECT_URL` (default `/`, origin-relative) used by
`routes_auth.go`; `OIDC_REDIRECT_URL` keeps its provider-registration role.
Both documented in `.env.example`.

### 3.11 `OIDC_ISSUER_URL` value confusion
`.env.example` said `http://localhost:5556/dex` while compose's dex config
issues `http://dex:5556/dex`; go-oidc requires an exact match, and the
wrong value fails discovery ("all logins blocked" — this is also what the
repo's dex config.yaml header warns about). `.env.example` now documents
both contexts and defaults to the compose-correct value.

## 4. Browser login caveat (test topology)

dex's registered redirect URIs are `localhost:5173/8080`. From a remote
laptop, tunnel first:

```bash
ssh -N -L 5173:localhost:5173 -L 8080:localhost:8080 -L 5556:localhost:5556 ai04
# then browse to  http://localhost:5173  (NOT http://<ai04-ip>:5173)
# login: admin@oap.local / password     (test-only users)
```

## 5. Open items (decision-gated, NOT fixed here)

1. **Static users have no groups/org.** dex's staticPasswords emit no
   `groups`/`org_id` claims → sessions mint role `viewer` + empty org;
   org-scoped routes return `400 {"error":"org context required"}`.
   Options: (a) add a dev-mode default org/role in `Verifier.Verify`,
   (b) real LDAP/groups connector, (c) seed a tenant + org-claim plumbing.
   Also note `MapGroupsToRole` can never fire for static users regardless.
2. **Stores don't EnsureSchema.** Migration 003 papers the a2a gap; the
   principled fix is calling the existing DDL constants at startup (or
   wiring a real migration runner — Alembic is set NOT used for the Go
   stores, and `py/alembic` vs `deploy/migrations` remain two divergent
   schema sources; see data-model spec KL #2).
3. **`nats.conf` env expansion** — repo keeps `${VAR}` placeholders; bake
   literals only if a host proves expansion broken (§3.4.5).
4. **Retention purger non-functional** (data-model spec KL #6) — needs a
   deliberate soft-delete migration; unchanged.

## 6. Incident notes (what to NOT do on this box)

* Never `docker compose ... down -v` from `~/oap` without `-p oap` — see §3.1.
* ai04's `.env` and baked `nats.conf` contain live generated secrets. They
  must NEVER be copied back into the repo (repo is public).
* The `.env` on ai04 was once truncated to 0 bytes by a shell pipeline
  (`set -a; . ./.env` + malformed in-place rewrite). Rebuild from
  `.env.example` with fresh secrets if it ever empties; the running
  containers may hold the old values (`docker inspect`).
* Nested `ssh ai04 "…python -c \"…\""` quoting corrupts files silently.
  Pattern that works: write scripts locally, `scp` to `ai04:~/`, execute.

## 7. File-by-file repo changes from this deploy

| File | Change |
|---|---|
| `deploy/Dockerfile.web` | +API/auth/a2a proxy blocks; healthcheck →127.0.0.1; topology comment |
| `deploy/docker-compose.yml` | removed obsolete `version:`; project-name/porting header; loopback ports; nats 6222/7422 dropped; `dex serve` command; server NATS mTLS env+cert mounts; `POST_LOGIN_REDIRECT_URL`; web healthcheck 127.0.0.1 |
| `deploy/nats/nats.conf` | log fields removed; `verify_and_map:false`; empty `users=[]` removed; cluster block commented; explanatory comments throughout |
| `deploy/dex/config.yaml` | trustedPeers removed (with correct-syntax comment) |
| `deploy/nats/scripts/gen-certs.sh` | new `gen_server_client_cert` (oap-server-client pair), wired into main flow |
| `deploy/migrations/003_platform_schema_addendum2.sql` | NEW — a2a_agent_cards / a2a_tasks / a2a_artifacts + `policies.deleted` |
| `deploy/scripts/test-login.sh` | NEW — scripted browserless OIDC login smoke test (validated on ai04) |
| `deploy.sh` | compose calls pinned to `-p oap`; post-release health hint fixed `/health`→`/healthz` |
| `internal/config/config.go` | new `PostLoginRedirectURL` (env `POST_LOGIN_REDIRECT_URL`, default `/`) |
| `internal/api/routes_auth.go` | callback redirects to `PostLoginRedirectURL` |
| `.env.example` | compose-NATS note; issuer exact-match guidance; POST_LOGIN_REDIRECT_URL |
| `openspec/specs/data-model/spec.md` | KL #1 records migration 003; §Description lists 001+002+003 |
| `INDEX_MAP.md` / `HEADER_MAP.md` | this document registered per CLAUDE.md §4 |
