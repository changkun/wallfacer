# M8b: Tenant API

**Status:** Not started | **Date:** 2026-03-28

## Problem

Wallfacer's existing HTTP API is an internal transport layer between the browser UI and the Go server. It exposes everything — env vars, admin endpoints, server config — and authenticates with a single shared key. This API is not suitable for external programmatic access by tenants.

Cloud tenants need a stable, scoped, versioned API to integrate wallfacer into their workflows: creating tasks from CI/CD pipelines, polling task status from scripts, receiving webhook notifications when tasks complete. The internal API should remain free to evolve with the UI; the tenant API is a product contract.

## Design Principles

1. **Separate surface, same server.** The tenant API is a distinct set of routes (`/api/v1/...`) served by the same wallfacer instance. No new service to deploy.
2. **Read-heavy, write-light.** Tenants create tasks and submit feedback. Everything else is read-only inspection.
3. **No admin exposure.** Env vars, server config, sandbox images, admin endpoints are never reachable through the tenant API.
4. **Webhook-first for async.** Instead of requiring tenants to poll, push state changes via webhooks. Polling remains available as a fallback.
5. **Stable contract.** Versioned (`/v1/`), with a deprecation policy. Internal API changes do not break tenant integrations.

## API Surface

All tenant API routes are prefixed with `/api/v1/`. Authentication is per-tenant (see Authentication below).

### Tasks

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `POST` | `/api/v1/tasks/batch` | Create multiple tasks with dependency wiring |
| `GET` | `/api/v1/tasks` | List tasks (filterable by status, paginated) |
| `GET` | `/api/v1/tasks/{id}` | Get task details (status, prompt, goal, timestamps, cost) |
| `GET` | `/api/v1/tasks/{id}/events` | Task event timeline (cursor-paginated) |
| `GET` | `/api/v1/tasks/{id}/diff` | Git diff for task worktrees vs default branch |
| `GET` | `/api/v1/tasks/{id}/outputs/{filename}` | Raw agent output per turn |
| `GET` | `/api/v1/tasks/{id}/oversight` | Oversight summary |
| `GET` | `/api/v1/tasks/{id}/usage` | Per-turn token usage breakdown |
| `PATCH` | `/api/v1/tasks/{id}` | Update task (status, prompt, goal, timeout, dependencies) |
| `POST` | `/api/v1/tasks/{id}/feedback` | Submit feedback for waiting tasks |
| `POST` | `/api/v1/tasks/{id}/done` | Mark waiting task as done |
| `POST` | `/api/v1/tasks/{id}/cancel` | Cancel a task |
| `POST` | `/api/v1/tasks/{id}/resume` | Resume a failed task |
| `POST` | `/api/v1/tasks/{id}/sync` | Rebase task worktrees onto latest default branch |
| `DELETE` | `/api/v1/tasks/{id}` | Soft-delete a task |

### Usage & Status

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/usage` | Aggregated token and cost usage |
| `GET` | `/api/v1/tasks/summaries` | Cost dashboard for completed tasks |

### Webhooks

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/webhooks` | Register a webhook endpoint |
| `GET` | `/api/v1/webhooks` | List registered webhooks |
| `GET` | `/api/v1/webhooks/{id}` | Get webhook details |
| `PATCH` | `/api/v1/webhooks/{id}` | Update webhook (URL, events, active) |
| `DELETE` | `/api/v1/webhooks/{id}` | Remove a webhook |

### What is NOT exposed

These internal API areas are explicitly excluded from the tenant API:

- `GET/PUT /api/env` — Tenant cannot read or modify API keys / server env
- `GET/PUT /api/config` — Server configuration (autopilot, sandbox routing)
- `POST /api/env/test` — Credential validation
- `GET/PUT/DELETE /api/system-prompts/*` — System prompt overrides
- `GET/PUT /api/instructions` — Workspace AGENTS.md
- `POST /api/admin/*` — Admin operations
- `GET /api/debug/*` — Debug/monitoring endpoints
- `GET /api/containers` — Running container list
- `GET/POST/DELETE /api/images/*` — Sandbox image management
- `PUT /api/workspaces` — Workspace switching
- `GET/POST /api/git/*` — Direct git operations
- `POST /api/ideate`, `GET/DELETE /api/ideate` — Ideation sessions
- `POST /api/tasks/{id}/refine` — Refinement (internal workflow concern)

## Authentication

The tenant API uses per-tenant API keys, distinct from the internal `WALLFACER_SERVER_API_KEY`.

### API Key Model

```
Authorization: Bearer wf_live_<base64-encoded-key>
```

Keys are scoped to a single tenant and managed by the control plane (M8):

```sql
CREATE TABLE api_keys (
    id          UUID PRIMARY KEY,
    user_id     UUID REFERENCES users(id),
    key_hash    TEXT NOT NULL,          -- bcrypt/argon2 hash; plaintext never stored
    prefix      TEXT NOT NULL,          -- first 8 chars for identification (e.g., "wf_live_a3b2")
    name        TEXT,                   -- human-readable label
    scopes      TEXT[] DEFAULT '{}',    -- future: granular permissions
    created_at  TIMESTAMP DEFAULT NOW(),
    last_used   TIMESTAMP,
    expires_at  TIMESTAMP              -- optional expiry
);
```

- Tenants create/revoke keys via the UI or a control plane API.
- Keys are shown once on creation, then only the prefix is visible.
- The control plane injects the valid key set into each wallfacer instance on provision/wake.

### Request Flow

```
Client ──Bearer token──▶ Control Plane ──validates key──▶ routes to tenant instance
                              │
                         key_hash lookup
                         rate limit check
                         inject X-Tenant-ID header
                              │
                              ▼
                      Wallfacer Instance
                      (trusts X-Tenant-ID from control plane)
```

The wallfacer instance itself does not validate API keys — the control plane is the auth boundary. The instance receives requests with a trusted `X-Tenant-ID` header (same pattern as M8's `X-Forwarded-User`).

## Webhooks

### Webhook Registration

```json
{
    "url": "https://example.com/wallfacer-webhook",
    "events": ["task.done", "task.failed", "task.waiting"],
    "secret": "whsec_..."
}
```

The `secret` is used to sign payloads so receivers can verify authenticity.

### Webhook Events

| Event | Fires when |
|-------|-----------|
| `task.created` | A new task is created |
| `task.started` | Task moves to `in_progress` |
| `task.waiting` | Task needs user feedback |
| `task.done` | Task completed successfully |
| `task.failed` | Task failed |
| `task.cancelled` | Task was cancelled |

### Webhook Payload

```json
{
    "id": "evt_<uuid>",
    "event": "task.done",
    "timestamp": "2026-03-28T14:30:00Z",
    "data": {
        "task_id": "<uuid>",
        "status": "done",
        "prompt": "Add rate limiting to the API",
        "goal": "...",
        "cost_usd": 0.42,
        "duration_seconds": 180
    }
}
```

Signed with HMAC-SHA256:

```
X-Wallfacer-Signature: sha256=<hex-digest>
```

### Delivery Guarantees

- **At-least-once delivery.** Failed deliveries are retried with exponential backoff (1s, 5s, 30s, 5m, 30m) up to 5 attempts.
- **Webhook state:** Each webhook tracks `last_delivery_at`, `last_status`, `consecutive_failures`. After 10 consecutive failures, the webhook is auto-disabled with a notification.
- **Delivery log:** Last 100 deliveries per webhook are stored for debugging (status code, response time, payload hash).

### Webhook Storage

```sql
CREATE TABLE webhooks (
    id                   UUID PRIMARY KEY,
    user_id              UUID REFERENCES users(id),
    url                  TEXT NOT NULL,
    secret_hash          TEXT NOT NULL,
    events               TEXT[] NOT NULL,
    active               BOOLEAN DEFAULT TRUE,
    created_at           TIMESTAMP DEFAULT NOW(),
    last_delivery_at     TIMESTAMP,
    last_status          INT,
    consecutive_failures INT DEFAULT 0
);

CREATE TABLE webhook_deliveries (
    id          UUID PRIMARY KEY,
    webhook_id  UUID REFERENCES webhooks(id),
    event       TEXT NOT NULL,
    payload     JSONB NOT NULL,
    status_code INT,
    response_ms INT,
    attempted_at TIMESTAMP DEFAULT NOW(),
    succeeded   BOOLEAN
);
```

## Rate Limiting

Per-tenant rate limits enforced at the control plane:

| Limit | Default | Configurable |
|-------|---------|-------------|
| Requests per minute | 60 | Yes (per plan) |
| Task creates per hour | 30 | Yes |
| Webhook endpoints | 10 | Yes |
| Concurrent running tasks | Inherited from `WALLFACER_MAX_PARALLEL` | Per-tenant override |

Rate limit headers on every response:

```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 42
X-RateLimit-Reset: 1711641600
```

429 response when exceeded, with `Retry-After` header.

## Versioning & Stability

- The tenant API is versioned: `/api/v1/`. The internal UI API (`/api/tasks`, `/api/env`, etc.) is unversioned and can change freely.
- Breaking changes (field removal, semantic changes) require a new version (`/api/v2/`).
- Non-breaking additions (new fields, new endpoints) are added to the current version.
- Deprecated versions are supported for at least 6 months after the successor is stable.
- The response envelope includes a `api_version` field for client verification.

### Response Envelope

All tenant API responses use a consistent envelope:

```json
{
    "api_version": "v1",
    "data": { ... },
    "pagination": {
        "cursor": "...",
        "has_more": true
    }
}
```

Error responses:

```json
{
    "api_version": "v1",
    "error": {
        "code": "rate_limited",
        "message": "Rate limit exceeded. Retry after 30 seconds.",
        "retry_after": 30
    }
}
```

## Implementation

### Where It Lives

The tenant API handlers live in `internal/handler/tenantapi/`, separate from the internal handlers. They reuse the same `internal/store` and `internal/runner` packages but apply their own:

- Input validation (stricter than the internal API)
- Response serialization (envelope format, field filtering)
- Audit logging (who did what, when)

### Route Registration

```go
// internal/handler/tenantapi/routes.go
func Register(mux *http.ServeMux, store *store.Store, runner *runner.Runner) {
    mux.Handle("GET /api/v1/tasks", ...)
    mux.Handle("POST /api/v1/tasks", ...)
    // ...
}
```

The tenant API routes are registered on the same `http.ServeMux` as internal routes. The control plane routes tenant API requests (path prefix `/api/v1/`) to the instance; internal routes (`/api/tasks`, `/api/env`, etc.) are only reachable from the UI via the instance's internal network.

### Webhook Dispatcher

A background goroutine in the control plane consumes task state change events (via SSE from each instance or a shared event bus) and dispatches webhook payloads:

```
Instance SSE ──▶ Control Plane Event Router ──▶ Webhook Dispatcher
                                                      │
                                               ┌──────┼──────┐
                                               ▼      ▼      ▼
                                           Tenant A  Tenant B  Tenant C
                                           webhooks  webhooks  webhooks
```

This keeps webhook delivery out of the wallfacer instance — the instance emits events (which it already does via SSE), and the control plane handles fan-out, retries, and delivery tracking.

## Integration Examples

### CI/CD: Create Task from GitHub Action

```yaml
- name: Create wallfacer task
  run: |
    curl -X POST https://wallfacer.example.com/api/v1/tasks \
      -H "Authorization: Bearer ${{ secrets.WALLFACER_API_KEY }}" \
      -H "Content-Type: application/json" \
      -d '{
        "prompt": "Fix the bug described in issue #42",
        "goal": "All tests pass, issue resolved",
        "timeout": 600
      }'
```

### Script: Poll Until Done

```bash
TASK_ID=$(curl -s -X POST .../api/v1/tasks \
  -H "Authorization: Bearer $KEY" \
  -d '{"prompt": "Refactor auth module"}' | jq -r '.data.id')

while true; do
  STATUS=$(curl -s .../api/v1/tasks/$TASK_ID \
    -H "Authorization: Bearer $KEY" | jq -r '.data.status')
  case $STATUS in
    done|failed|cancelled) echo "Task $STATUS"; break ;;
    *) sleep 10 ;;
  esac
done
```

### Webhook: Slack Notification on Completion

```python
@app.route("/wallfacer-webhook", methods=["POST"])
def handle_webhook():
    verify_signature(request)
    event = request.json
    if event["event"] in ("task.done", "task.failed"):
        slack.post(f"Task {event['data']['task_id']}: {event['event']}")
    return "", 200
```

---

## Dependencies

| Dependency | Why |
|-----------|-----|
| **M8a: Authentication** | Tenant identity required for API key scoping |
| **M8: Multi-Tenant** | Control plane required for key management, rate limiting, and webhook dispatch |

This spec is a branch from M8. It can be implemented incrementally after the control plane exists — start with API key auth + task CRUD, then add webhooks.

## Implementation Order

1. **API key model** — Key creation/revocation in control plane, hash storage, prefix display
2. **Tenant API routes** — `internal/handler/tenantapi/` with task CRUD, response envelope
3. **Rate limiting** — Per-tenant request counting at the control plane
4. **Webhook registration** — CRUD for webhook endpoints
5. **Webhook dispatcher** — Event consumption, payload signing, delivery with retries
6. **Delivery log & auto-disable** — Observability and failure handling
7. **Client SDK / CLI integration** — Optional: `wallfacer api` subcommand for scripting

## Open Questions

1. **SSE for tenants?** Should the tenant API expose an SSE endpoint (`GET /api/v1/tasks/stream`) as an alternative to webhooks? Simpler for scripts that want real-time updates without setting up a webhook receiver. Downside: long-lived connections through the control plane proxy.
2. **Scoped API keys?** The `scopes` column is reserved but not used in v1. Future: read-only keys, task-create-only keys, workspace-scoped keys. Add when there's a concrete use case.
3. **Batch feedback?** Should `POST /api/v1/tasks/{id}/feedback` support submitting feedback to multiple waiting tasks at once? Useful for pipeline orchestration.
