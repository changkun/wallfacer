---
title: Data Boundary Enforcement
status: drafted
depends_on: []
affects:
  - frontend/src/telemetry.ts
  - frontend/src/main.ts
  - internal/cli/web.go
effort: small
created: 2026-04-12
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Data Boundary Enforcement

## Problem

When wallfacer runs in cloud-hosted mode, the browser SPA emits OpenTelemetry
RUM (real user monitoring) spans. Those spans leave the user's machine: the SPA
exporter posts OTLP/HTTP batches to a same-origin route (`POST /v1/telemetry/`)
which the backend forwards to an in-cluster collector. This is the only data
path in the current architecture that carries SPA-originated data off the
machine.

The data boundary defines what is allowed to ride that path: span timings,
service name, HTTP method, and same-origin request URLs (which embed task IDs
and planning thread IDs). It must exclude source code, file contents, diffs,
agent output, secrets, env vars, and repo paths.

The browser instrumentations (document-load, fetch, XHR) attach attributes to
spans automatically. Without review, a `http.url` attribute can carry an
opaque-but-identifying value (a task ID, a thread ID) or, if a future route
embeds a repo path or filename in the URL, user content. For a privacy-focused
product, an unreviewed span attribute is a silent, hard-to-detect leak.

Two corrections to the prior framing of this spec:

1. There is no in-house `internal/cloud/` telemetry subsystem and no structured
   `TelemetryEvent` Go type. Local instances do not phone home. The egress is
   browser OTel RUM only.
2. Telemetry is gated to cloud mode. `frontend/src/main.ts` calls
   `initTelemetry` only when `import.meta.env.PROD` and
   `window.__WALLFACER__?.mode === 'cloud'`. In local and local-first mode no
   exporter is created and no proxy is mounted, so egress is zero. The
   cloud-mode gate is itself the first boundary control.

## Scope

Pin down and review what the SPA exports in cloud mode, and keep it reviewable
over time.

In scope:
- Inventory the single egress path: the SPA exporter
  (`frontend/src/telemetry.ts`), its cloud-mode gate
  (`frontend/src/main.ts`), and the proxy mount
  (`internal/cli/web.go`, `POST /v1/telemetry/`).
- Scrub identifiers from span attributes the browser exports, primarily
  `http.url`, so task IDs and thread IDs do not ride RUM spans.
- A frontend test that asserts the gate and the scrubbing hold, so a regression
  is caught in CI.

Out of scope:
- The proxy itself (`otel.TelemetryProxy` in `latere.ai/x/pkg/otel`) is a
  vendored, cross-repo, generic byte-forwarder (1 MiB body cap, forwards only
  `Content-Type`/`Content-Encoding`). It does not redact and cannot be made to
  from this repo. Redaction must happen in the SPA before export. Any
  proxy-side enforcement is an upstream `latere.ai/x/pkg/otel` change tracked
  separately.

## Design

### Egress inventory

The data path, end to end, in cloud mode:

1. `frontend/src/main.ts` gates RUM to cloud + prod + client.
2. `frontend/src/telemetry.ts` registers document-load, fetch, and XHR
   instrumentations and exports OTLP/HTTP to `/v1/telemetry/v1/traces` via a
   `BatchSpanProcessor`. It already sets `ignoreUrls: [/\/v1\/telemetry\//]`
   so the exporter does not instrument its own POSTs, and sets no
   `propagateTraceHeaderCorsUrls` so `traceparent` stays same-origin.
3. `internal/cli/web.go` mounts `otel.TelemetryProxy("/v1/telemetry")` on
   `POST /v1/telemetry/`, which forwards to `OTEL_EXPORTER_OTLP_ENDPOINT`.

The only span data that leaves the machine is the attribute set the
instrumentations produce.

### Attribute allowlist / redaction

The leak surface is span attributes, not a Go struct. The concrete identifiers
today come from same-origin request URLs the fetch and XHR instrumentations
capture as `http.url`. Confirmed URL shapes from the SPA stores include:

- `GET /api/tasks`, `POST /api/tasks`, `PATCH /api/tasks/{id}`,
  `DELETE /api/tasks/{id}`
- `GET /api/specs/tree`
- `/api/planning/threads/{id}` (id URL-encoded)
- `/api/config`, `/api/me`

These carry opaque task IDs and thread IDs in the path. No route embeds source,
diffs, or repo paths in the URL today (IDs and content travel in request
bodies, which OTel does not capture). The boundary control:

1. Install a `SpanProcessor` (or attribute hook) in `telemetry.ts` that rewrites
   `http.url` / `http.target` on every span before export: replace path
   segments that are IDs with a placeholder so the route shape is kept for
   dashboards (`/api/tasks/:id`) but the identifier is dropped.
2. Keep an explicit allowlist of attribute keys permitted to leave: span name,
   `service.name`, `http.method`, the scrubbed route template, and timing
   attributes. Drop or scrub anything else the instrumentations add.
3. Never enable `propagateTraceHeaderCorsUrls`, so `traceparent` is not sent to
   third-party hosts. This is already the case; the test pins it.

### Regression test

A frontend test (`frontend/src/telemetry.test.ts`) that:
- Asserts `initTelemetry` is only invoked in cloud mode (the gate in
  `main.ts`), so local-mode egress stays zero.
- Feeds representative URLs (`/api/tasks/abc123`,
  `/api/planning/threads/xyz`) through the scrubber and asserts IDs are
  replaced and route shape is preserved.
- Asserts the exported attribute set is a subset of the allowlist.

## Implementation

- `frontend/src/telemetry.ts`: URL/attribute scrubber and attribute allowlist
  applied before export.
- `frontend/src/main.ts`: cloud-mode gate (already present; covered by test).
- `frontend/src/telemetry.test.ts`: gate + scrubbing + allowlist tests.
- `internal/cli/web.go`: egress mount (inventory only; no change expected).
- Documentation in `docs/internals/data-and-storage.md` describing the cloud
  RUM path and what leaves the machine.

## Test Plan

- Unit (frontend): scrubber replaces ID path segments, preserves route shape.
- Unit (frontend): exported attribute set is a subset of the allowlist.
- Unit (frontend): RUM exporter inits only in cloud mode; local mode produces
  no exporter.
- Manual: in a cloud build, capture an outbound `/v1/telemetry/v1/traces`
  payload and confirm no raw task IDs, thread IDs, paths, or content.

## Success

- The SPA exports RUM only in cloud mode; local and local-first mode emit
  nothing.
- Span attributes that leave the machine are a reviewed, scrubbed set with no
  raw identifiers or content.
- A CI test fails if the gate is removed or the scrubber stops covering
  identifier-bearing URLs.
- Changkun can point to this spec when users ask "what leaves my machine?" and
  answer: in cloud mode only, browser RUM spans over one same-origin proxy
  route, with identifiers scrubbed in `telemetry.ts`.
