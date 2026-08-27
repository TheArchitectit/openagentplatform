# Platform Foundation — API Schema Export

> **Phase:** 0 (Foundation) — API documentation
> **STATUS: COMPLETE** — OpenAPI YAML embedded and served via Swagger UI
> **Source:** authored 2026-08-25 from code (`internal/schema/`)
> **App Path:** `internal/schema/`, `internal/schema/openapi.yaml`
> **Source files:** `openapi.go`, `openapi.yaml`

---

## Description

`internal/schema/` serves the platform's API contract as OpenAPI. It embeds a
checked-in `openapi.yaml` (`//go:embed`), converts it to JSON on demand
(`sigs.k8s.io/yaml`), and mounts three documentation endpoints on a
`chi.Router`:

- `GET /docs` — Swagger UI HTML (CDN-hosted assets)
- `GET /docs/swagger` — raw OpenAPI YAML
- `GET /docs/openapi.json` — OpenAPI spec as JSON (yaml → json)

The package is a thin wrapper around an embedded file. It is the single source
of truth for the API contract; `MountSwagger` is called from server bootstrap.

**Note:** this spec covers `internal/schema/` (the OpenAPI export). It is NOT
the `schema-health` spec, which covers the `scripts/schema-health-check.sh`
schema-drift checker.

## User Story

**As** an API consumer,
**I want** a machine-readable OpenAPI spec and a Swagger UI at `/docs`,
**so that** I can understand the API contract without reading source code.

---

## Requirements

### 1. Embedded Contract

1.1. `openapi.yaml` is checked into the repo and embedded at compile time via
`//go:embed`. It is the single source of truth; there is no runtime generation.

1.2. `toJSON()` converts the YAML to JSON lazily (`sync.Once`) and caches the
result for the process lifetime. On conversion failure it returns an error and
the `/docs/openapi.json` endpoint returns `500 {"error":"spec_conversion_failed"}`.

### 2. Documentation Endpoints

2.1. `MountSwagger(r chi.Router)` registers the three `/docs/*` routes. It is
called once from server bootstrap.

2.2. `GET /docs` serves HTML that loads Swagger UI from a CDN and points it at
`/docs/openapi.json`.

2.3. `GET /docs/swagger` serves the raw YAML with
`Content-Type: application/yaml; charset=utf-8`.

2.4. `GET /docs/openapi.json` serves the converted JSON with
`Content-Type: application/json; charset=utf-8`.

### 3. Contract Maintenance

3.1. The YAML is hand-maintained; every new API endpoint requires a manual edit
to `openapi.yaml` plus a route in the server. There is no automatic generation
from handlers.

3.2. **Known limitation:** drift between the YAML and the actual handlers is
possible. The `schema-health` script (`scripts/schema-health-check.sh`) checks
for schema-drift in the DB layer, but there is no automated check that the
OpenAPI YAML matches the live routes.

---

## Known Limitations

- **Hand-maintained contract.** Endpoints are not generated from code, so the
  YAML can silently drift from the implementation.
- **No versioning in the URL.** `/docs/openapi.json` serves the latest contract;
  there is no pinned-version endpoint for consumers.
- **Swagger UI is CDN-hosted.** The page requires network access to load
  `unpkg.com/swagger-ui-dist`; it has no offline bundle.

---

## Cross-References

- `schema-health` spec — the DB schema-drift checker (different package)
- `endpoint-agent` spec — agent-side API contract is documented there
- `data-model` spec — the entities referenced by the OpenAPI paths