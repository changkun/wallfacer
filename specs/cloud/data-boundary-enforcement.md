---
title: Data Boundary Enforcement
status: stale
depends_on:
  - specs/cloud/latere-integration/coordination-plane.md
affects:
  - frontend/src/telemetry.ts
  - frontend/src/main.ts
  - internal/cli/web.go
effort: medium
created: 2026-04-12
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Data Boundary Enforcement

## Problem

When wallfacer runs in cloud-hosted mode, the browser SPA emits OpenTelemetry
RUM (real user monitoring) spans. Those spans leave the user's machine: the SPA
exporter posts OTLP/HTTP batches to a same-origin route (`POST /v1/telemetry/`)
which the backend forwards to an in-cluster collector. This is the SPA-originated
data path off the machine. A second path now exists: the opt-in coordination
channel from a signed-in local instance to wf.latere.ai (see "Coordination
channel egress"). The data boundary governs both.

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
   `TelemetryEvent` Go type. Local instances DO phone home, but only over one
   path: the governed, opt-in coordination channel to wf.latere.ai (see
   "Coordination channel egress" below and
   [coordination-plane](latere-integration/coordination-plane.md)). That channel
   is itself allow-list-gated. A local-anonymous instance, and a signed-in
   instance that has not opted in to coordination, phone home NOTHING. The other
   egress, browser OTel RUM, is cloud-mode SPA only and is unchanged.
2. Telemetry is gated to cloud mode. `frontend/src/main.ts` calls
   `initTelemetry` only when `import.meta.env.PROD` and
   `window.__WALLFACER__?.mode === 'cloud'`. In local and local-first mode no
   exporter is created and no proxy is mounted, so egress is zero. The
   cloud-mode gate is itself the first boundary control.

## Scope

Pin down and review what the SPA exports in cloud mode, and keep it reviewable
over time.

In scope:
- Inventory the SPA egress path: the SPA exporter
  (`frontend/src/telemetry.ts`), its cloud-mode gate
  (`frontend/src/main.ts`), and the proxy mount
  (`internal/cli/web.go`, `POST /v1/telemetry/`).
- Scrub identifiers from span attributes the browser exports, primarily
  `http.url`, so task IDs and thread IDs do not ride RUM spans.
- Govern the coordination channel egress: the opt-in gate (no opt-in, no
  connection, zero bytes) and the allow-list as the only thing that crosses.
- Frontend and coordination-channel tests that assert the gates, the scrubbing,
  and the allow-list hold, so a regression is caught in CI.

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

### Coordination channel egress

The second egress path is the phone-home channel: a signed-in, opted-in local
instance holds one outbound connection to wf.latere.ai (the coordinator role of
wallfacerd) carrying presence, an allow-listed metadata projection, and
spec-comment events. The data boundary governs this channel too. Three controls:

1. Opt-in gate. The connection opens only when the instance is signed in AND has
   explicitly opted in to coordination. Otherwise the instance dials nothing and
   emits zero bytes to wf.latere.ai. A local-anonymous instance, and a signed-in
   instance that has not opted in, are byte-identical to today (no connection, no
   manifest, no projection push). The opt-in switch is the first boundary control
   on this path, the same role cloud-mode plays for RUM.
2. Allow-list as the boundary control. Only three payload classes cross:
   presence hints (which principal is online on which instance, focus), the
   enumerated metadata-projection fields (task counts, titles, statuses, actor
   subs, timestamps, usage totals), and spec-comment anchors and bodies. Source
   code, diffs, agent output, secrets, env vars, and repo paths NEVER cross. The
   projection is derived from the local `store.TaskEvent` stream, redacted to the
   allow-list before it leaves the process; nothing outside the allow-list is
   pushed.
3. Ownership of the lists. This spec owns the boundary RULE (only the three
   allow-listed classes cross) and the opt-in gate. The precise projection field
   list is owned by the metadata-projection leaf
   (`coordination-plane/metadata-projection.md`); the comment payload schema is
   owned by the spec-comments leaf (`coordination-plane/spec-comments.md`). This
   spec enforces that whatever those leaves enumerate is the only thing that
   crosses, and that an unlisted field is dropped before export.

### Coordination channel regression test

A test mirroring the RUM scrubber test, asserting the two invariants:

- Opted out (anonymous, or signed-in-but-not-opted-in): the channel sends
  nothing. No connection is opened and no payload is emitted, byte-identical to
  local-anonymous today.
- Opted in: the payload the channel does send is a subset of the allow-list.
  Feed a representative `store.TaskEvent` (and a spec comment) through the
  projection/redaction step and assert the emitted field set is a subset of the
  enumerated allow-list, and that source, diffs, agent output, secrets, env vars,
  and repo paths are absent. A regression is caught in CI if the gate is removed
  or an unlisted field starts crossing.

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
- The coordination channel opens only on sign-in; sign-in defaults a signed-in
  instance to opted-in (a deliberate product decision, revising the earlier
  off-by-default), and an explicit opt-out (`WALLFACER_COORDINATION=0` or the
  in-app toggle) closes it. A local-anonymous or opted-out instance emits zero
  bytes to wf.latere.ai. What a connected instance sends is a subset of the
  allow-list, with no source, diffs, agent output, secrets, env vars, or repo
  paths.
- A CI test fails if either gate is removed, if the scrubber stops covering
  identifier-bearing URLs, or if an unlisted field starts crossing the
  coordination channel.
- Changkun can point to this spec when users ask "what leaves my machine?" and
  answer: in cloud mode, browser RUM spans over one same-origin proxy route with
  identifiers scrubbed in `telemetry.ts`; and, only if signed in and opted in,
  presence plus an allow-listed metadata projection plus spec comments over the
  coordination channel. Opted out or anonymous, nothing leaves.
