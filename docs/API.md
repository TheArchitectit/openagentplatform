# API

Base URL: `http://localhost:8080`

All endpoints are JSON over HTTP. Authenticated routes require a valid session
cookie obtained via the OIDC flow.

## Endpoints

### Health

```
GET /api/v1/health
```

Returns `{"status":"ok"}`.

### Agents

```
GET    /api/v1/agents       list agents
POST   /api/v1/agents       register a new agent
GET    /api/v1/agents/{id}  get one agent
PATCH  /api/v1/agents/{id}  update an agent
DELETE /api/v1/agents/{id}  deregister
```

### Sites

```
GET    /api/v1/sites
POST   /api/v1/sites
```

### Checks

```
GET    /api/v1/checks
POST   /api/v1/checks
GET    /api/v1/checks/{id}
PATCH  /api/v1/checks/{id}
DELETE /api/v1/checks/{id}
```

### Alerts

```
GET  /api/v1/alerts
POST /api/v1/alerts/{id}/acknowledge
POST /api/v1/alerts/{id}/resolve
```

### Policies & Patches

```
GET    /api/v1/policies
POST   /api/v1/policies
POST   /api/v1/policies/{id}/apply
GET    /api/v1/patches
```

### Scripts

```
GET    /api/v1/scripts
POST   /api/v1/scripts
POST   /api/v1/scripts/{id}/run
```

## Error format

```json
{
  "error": "validation_failed",
  "message": "email is required",
  "details": { "field": "email" }
}
```

| Status | Meaning               |
|--------|-----------------------|
| 400    | Validation error      |
| 401    | Not authenticated     |
| 403    | Forbidden             |
| 404    | Not found             |
| 409    | Conflict              |
| 500    | Server error          |

## Full specification

See the live Swagger UI at `/docs` (when the server is running) or
`/swagger.json` for the machine-readable spec.
