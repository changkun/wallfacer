---
title: "Telemetry & Flow Observability — Closing the Signal-to-Code Loop"
status: drafted
depends_on:
  - shared/agent-abstraction.md
affects:
  - internal/metrics/
  - internal/runner/
  - internal/store/
  - internal/handler/
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Telemetry & Flow Observability — Closing the Signal-to-Code Loop

---

## Problem

Wallfacer can dispatch AI agents to write code, but it has no way to observe whether the resulting software *works correctly at runtime*. The system is blind after "task done":

1. **No runtime signals.** A task produces code. Whether that code emits errors, serves requests slowly, or crashes under load is invisible to Wallfacer.
2. **No feedback loop.** When a live-serve session (or a deployed service) misbehaves, the user must manually diagnose, then manually create a new task describing the problem. The observability data that explains the failure never reaches the agent that could fix it.
3. **No structured telemetry.** Wallfacer's internal metrics (Prometheus counters/histograms for HTTP handlers) are useful for ops, but there is no framework for collecting application-level telemetry from code the agents produce — traces, logs, metrics from the user's software.

The gap is: **runtime signals exist in the world but never flow back into the task board as actionable context for agents.**

---

## Goals

1. **Collect** runtime telemetry from software that Wallfacer builds and runs (via live-serve or external deployment).
2. **Correlate** telemetry signals back to the tasks/commits that produced the code.
3. **Surface** anomalies, errors, and performance regressions in the Wallfacer UI alongside the task board.
4. **Feed** selected signals into agent prompts so that coding agents can diagnose and fix runtime problems autonomously.
5. **Work differently** on local (single-machine, low-overhead) vs cloud (multi-tenant, standards-based) deployments.

---

## Non-Goals (v1)

- Replace dedicated APM/observability platforms (Datadog, Grafana, etc.).
- Require users to instrument their code manually.
- Full distributed tracing across microservice architectures.
- Real-time alerting or paging.

---

## Design

### Two Strategies: Local vs Cloud

The telemetry architecture diverges by deployment mode because the constraints are fundamentally different.

| Concern | Local | Cloud |
|---------|-------|-------|
| **Signal source** | Live-serve container on localhost | Deployed services (K8s pods, VMs) |
| **Collection** | Sidecar process in serve container + MCP server | OTEL Collector as cluster service |
| **Storage** | In-memory ring buffer + SQLite | Time-series DB (Prometheus/Mimir) + log store (Loki) |
| **Correlation** | Git commit SHA → task UUID (local store lookup) | Trace/span metadata labels (`wallfacer.task_id`, `wallfacer.commit`) |
| **Agent access** | MCP tool calls from agent container | API query from agent container |
| **Cost** | Near-zero (no external infra) | Scales with tenant count |

---

### Local Strategy

#### Architecture

```
┌─────────────┐     ┌──────────────────────────────────┐
│  Wallfacer   │     │  Serve Container                  │
│  Server      │     │                                    │
│              │◄────┤  User App ──► stdout/stderr        │
│  /api/telemetry    │           ──► :app-port            │
│              │◄────┤  otel-lite (sidecar)               │
│  Telemetry   │     │    - scrapes app metrics endpoint  │
│  Ring Buffer │     │    - captures structured logs      │
│  + SQLite    │     │    - OTLP push to wallfacer        │
│              │     └──────────────────────────────────┘
│  MCP Server  │◄──── Agent containers (tool calls)
└─────────────┘
```

#### Signal Collection (Local)

Three tiers of signals, each progressively more structured:

**Tier 1 — Passive log capture (zero config)**
Already partially exists: `GET /api/serve/logs` streams container stdout/stderr. Extend this to:
- Parse structured log formats (JSON lines, logfmt) when detected.
- Extract error-level entries into a separate error ring buffer.
- Tag each log line with a monotonic timestamp and the current git HEAD of the workspace.

**Tier 2 — HTTP probe (low config)**
If the serve session has a port configured, periodically probe it:
- `GET /` or a configured health endpoint.
- Record response status, latency, response size.
- Store as a time series in the telemetry ring buffer.
- Detect anomalies: sustained 5xx, latency spikes above a threshold, connection refused after restart.

**Tier 3 — OTLP ingest (opt-in)**
For apps that emit OpenTelemetry signals (or can be auto-instrumented):
- Wallfacer runs a lightweight OTLP receiver on a local port (gRPC or HTTP).
- The serve container gets `OTEL_EXPORTER_OTLP_ENDPOINT=http://host.containers.internal:<port>` injected automatically.
- Wallfacer receives traces, metrics, and logs via OTLP and stores them in the ring buffer.
- Correlation: OTLP resource attributes include `service.version` (set to git SHA) which maps back to task IDs.

The sidecar (`otel-lite`) is a small Go binary compiled into the serve container image. It handles Tier 2 probing and can optionally scrape a Prometheus `/metrics` endpoint if detected. For Tier 3, the OTLP receiver lives in the Wallfacer server process itself (not the container) to avoid complexity.

#### MCP Server for Agent Access (Local)

Agents need to query telemetry during task execution. The mechanism: an **MCP (Model Context Protocol) server** exposed by Wallfacer.

MCP tools provided:

| Tool | Description |
|------|-------------|
| `telemetry_errors` | Recent error log entries (last N minutes or since last deploy) |
| `telemetry_latency` | P50/P95/P99 latency for the serve endpoint |
| `telemetry_status_codes` | HTTP status code distribution |
| `telemetry_traces` | Slow or errored trace summaries (if OTLP tier active) |
| `telemetry_health` | Current health probe result |
| `telemetry_diff_since` | Telemetry changes since a given commit SHA |

When an agent is working on a task and live-serve is running, these tools let the agent inspect runtime behavior without the user copy-pasting logs. The agent can:
1. Run the app (via live-serve).
2. Observe errors via `telemetry_errors`.
3. Fix the code.
4. Re-observe to confirm the fix.

This closes the loop.

#### Alternative: Direct API Access (Local)

If MCP is not available or the agent runtime doesn't support it, the same data is accessible via HTTP:

| Route | Description |
|-------|-------------|
| `GET /api/telemetry/errors` | Recent errors with stack traces and timestamps |
| `GET /api/telemetry/latency` | Latency percentiles over time windows |
| `GET /api/telemetry/probes` | Health probe history |
| `GET /api/telemetry/traces` | OTLP trace summaries (when active) |
| `GET /api/telemetry/stream` | SSE: real-time telemetry events |

Agents in containers can reach these via the container network. The board manifest (already injected into agent prompts) would include a `telemetry_url` field pointing to the server.

#### Storage (Local)

- **Ring buffer**: Fixed-size in-memory buffer (configurable, default 10k entries) for recent telemetry. Fast reads, no disk overhead. Lost on server restart.
- **SQLite**: `~/.wallfacer/telemetry.db` for durable storage. Partitioned by day. Auto-pruned after retention window (`WALLFACER_TELEMETRY_RETENTION_DAYS`, default 7). Stores:
  - Error events (timestamp, level, message, stack trace, git SHA, task ID)
  - Probe results (timestamp, status, latency, endpoint)
  - OTLP span summaries (trace ID, span name, duration, status, attributes subset)
  - Metric snapshots (timestamp, metric name, value, labels)

#### Correlation (Local)

Every telemetry entry is tagged with the **workspace git HEAD** at the time of collection. Since Wallfacer tracks which task produced which commit, the mapping is:

```
telemetry entry → git SHA → task worktree → task UUID
```

This allows the UI to show: "These errors appeared after task X was merged" or "This latency regression correlates with commit Y from task Z."

---

### Cloud Strategy

#### Architecture

```
┌──────────────────┐     ┌─────────────────┐     ┌──────────────────┐
│  Tenant Pod       │     │  OTEL Collector  │     │  Wallfacer       │
│  (K8s)            │────►│  (DaemonSet or   │────►│  Cloud Server    │
│                   │     │   sidecar)       │     │                  │
│  User App         │     │  - OTLP receiver │     │  - Query API     │
│  + OTEL SDK auto  │     │  - label inject  │     │  - Anomaly detect│
│                   │     │  - export to:    │     │  - Agent context │
└──────────────────┘     │    - Mimir/Prom  │     └──────────────────┘
                          │    - Loki        │
                          │    - Tempo       │
                          └─────────────────┘
```

#### Signal Collection (Cloud)

In cloud mode, Wallfacer delegates telemetry collection to the **OpenTelemetry Collector**, deployed as a Kubernetes DaemonSet or sidecar.

**Auto-instrumentation**: K8s sandbox pods get OTEL auto-instrumentation injected via init containers or the OpenTelemetry Operator. This provides traces and metrics for common frameworks (Express, Gin, Django, Rails, Spring) with zero user config.

**Label injection**: The OTEL Collector's `k8s.attributes` processor enriches all signals with:
- `wallfacer.tenant_id` — from pod label
- `wallfacer.task_id` — from pod label (set by K8s sandbox backend)
- `wallfacer.commit` — from pod annotation (set during deploy)
- `wallfacer.workspace` — from pod label

**Storage backends** (per-tenant or shared, depending on isolation requirements):

| Signal | Backend | Query |
|--------|---------|-------|
| Metrics | Mimir or Prometheus (per-tenant namespace) | PromQL |
| Logs | Loki (per-tenant label) | LogQL |
| Traces | Tempo (per-tenant) | TraceQL |

**Cost control**: Per-tenant ingestion rate limits via OTEL Collector's `ratelimiting` processor. Retention policies per tier (free tier: 1 day, paid: 30 days).

#### Agent Access (Cloud)

Cloud agents query telemetry via the Wallfacer API, which proxies to the underlying storage backends:

| Route | Backend Query |
|-------|--------------|
| `GET /api/v1/telemetry/errors` | LogQL: `{tenant_id="X", level="error"}` |
| `GET /api/v1/telemetry/latency` | PromQL: `histogram_quantile(0.95, http_request_duration{tenant_id="X"})` |
| `GET /api/v1/telemetry/traces` | TraceQL: `{.wallfacer.task_id = "X" && status = error}` |

The tenant API (from `specs/cloud/tenant-api.md`) exposes these under the versioned API with per-tenant API key auth.

#### Correlation (Cloud)

Since pod labels carry `wallfacer.task_id`, correlation is direct — no git SHA lookup needed. The OTEL resource attributes provide the mapping. The Wallfacer query API can:
- Show all errors attributed to a specific task.
- Compare latency distributions before/after a task's commit.
- Trace a request through services and identify which task introduced the slow span.

---

### Signal-to-Code Feedback Loop

The core value proposition. Both local and cloud strategies feed into the same feedback loop:

```
Runtime signal (error, latency spike, crash)
    │
    ▼
Anomaly detection (threshold or ML-based)
    │
    ▼
Correlated to task/commit
    │
    ▼
Surfaced in UI (task card badge, notification)
    │
    ▼
User confirms "fix this" (or auto-dispatch if enabled)
    │
    ▼
New task created with telemetry context injected into prompt:
  - Error messages and stack traces
  - Relevant log lines (time window around the anomaly)
  - Latency data (before/after comparison)
  - The specific commit/file that correlates
    │
    ▼
Agent receives full context → writes fix → live-serve → observe again
```

#### Anomaly Detection

**v1 — Rule-based:**
- Error rate exceeds threshold (configurable, default: >5% of requests over 1 min window).
- Latency P95 exceeds threshold (default: 2x baseline).
- Health probe fails N consecutive times (default: 3).
- New error class appears (error message not seen in previous N minutes).

**v2 — Pattern-based:**
- Baseline established from first N minutes of stable operation.
- Statistical deviation detection (z-score on latency, error rate).
- Log anomaly detection (new log patterns via clustering).

#### Auto-Dispatch (Opt-In)

When `WALLFACER_TELEMETRY_AUTO_DISPATCH=true`, detected anomalies automatically create tasks:

1. Anomaly detected and correlated to commit/task.
2. Wallfacer creates a new task with:
   - Prompt: auto-generated from anomaly data (template: `internal/prompts/telemetry-fix.tmpl`).
   - Dependencies: the original task (so the fix builds on the same worktree).
   - Context: telemetry snapshot injected as task attachment or prompt prefix.
3. If autopilot is on, the task auto-promotes to in_progress.

Guard rails:
- Rate limit: max 1 auto-dispatched task per anomaly class per hour.
- Budget cap: auto-dispatched tasks share the same cost budget as manually created tasks.
- Circuit breaker: if 3 consecutive auto-fix tasks fail, disable auto-dispatch and notify the user.

---

## Data Model

### TelemetryEntry

```go
// TelemetryEntry is a single telemetry data point collected from a
// serve session or external deployment.
type TelemetryEntry struct {
    ID          uuid.UUID         `json:"id"`
    Timestamp   time.Time         `json:"timestamp"`
    Kind        TelemetryKind     `json:"kind"`          // "error", "probe", "metric", "trace_summary", "log"
    Source      TelemetrySource   `json:"source"`        // "serve", "external", "otel"
    SessionID   *uuid.UUID        `json:"session_id,omitempty"`  // serve session, if applicable
    TaskID      *uuid.UUID        `json:"task_id,omitempty"`     // correlated task
    CommitSHA   string            `json:"commit_sha,omitempty"`
    Severity    string            `json:"severity,omitempty"`    // "info", "warn", "error", "fatal"
    Message     string            `json:"message,omitempty"`
    Data        json.RawMessage   `json:"data,omitempty"`        // kind-specific payload
    Labels      map[string]string `json:"labels,omitempty"`
}

type TelemetryKind string

const (
    TelemetryError        TelemetryKind = "error"
    TelemetryProbe        TelemetryKind = "probe"
    TelemetryMetric       TelemetryKind = "metric"
    TelemetryTraceSummary TelemetryKind = "trace_summary"
    TelemetryLog          TelemetryKind = "log"
)

type TelemetrySource string

const (
    TelemetrySourceServe    TelemetrySource = "serve"
    TelemetrySourceExternal TelemetrySource = "external"
    TelemetrySourceOTEL     TelemetrySource = "otel"
)
```

### TelemetryAnomaly

```go
// TelemetryAnomaly represents a detected runtime anomaly correlated
// to a task or commit.
type TelemetryAnomaly struct {
    ID            uuid.UUID        `json:"id"`
    DetectedAt    time.Time        `json:"detected_at"`
    Kind          AnomalyKind      `json:"kind"`           // "error_spike", "latency_regression", "health_failure", "new_error_class"
    Severity      string           `json:"severity"`       // "low", "medium", "high", "critical"
    TaskID        *uuid.UUID       `json:"task_id,omitempty"`
    CommitSHA     string           `json:"commit_sha,omitempty"`
    Summary       string           `json:"summary"`
    Evidence      json.RawMessage  `json:"evidence"`       // supporting telemetry entries
    DispatchedTaskID *uuid.UUID    `json:"dispatched_task_id,omitempty"` // auto-created fix task
    Status        AnomalyStatus    `json:"status"`         // "open", "acknowledged", "fixing", "resolved", "dismissed"
}

type AnomalyKind string

const (
    AnomalyErrorSpike         AnomalyKind = "error_spike"
    AnomalyLatencyRegression  AnomalyKind = "latency_regression"
    AnomalyHealthFailure      AnomalyKind = "health_failure"
    AnomalyNewErrorClass      AnomalyKind = "new_error_class"
)

type AnomalyStatus string

const (
    AnomalyOpen         AnomalyStatus = "open"
    AnomalyAcknowledged AnomalyStatus = "acknowledged"
    AnomalyFixing       AnomalyStatus = "fixing"
    AnomalyResolved     AnomalyStatus = "resolved"
    AnomalyDismissed    AnomalyStatus = "dismissed"
)
```

---

## API (Local)

### Telemetry Query

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/telemetry/errors` | Recent error entries, filterable by time/task/severity |
| `GET` | `/api/telemetry/probes` | Health probe history |
| `GET` | `/api/telemetry/latency` | Latency percentiles over configurable time windows |
| `GET` | `/api/telemetry/traces` | OTLP trace summaries (when OTLP tier active) |
| `GET` | `/api/telemetry/metrics` | Custom metric snapshots |
| `GET` | `/api/telemetry/stream` | SSE: real-time telemetry events |

### Anomaly Management

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/telemetry/anomalies` | List detected anomalies |
| `PATCH` | `/api/telemetry/anomalies/{id}` | Update anomaly status (acknowledge, dismiss) |
| `POST` | `/api/telemetry/anomalies/{id}/dispatch` | Create fix task from anomaly |

### Configuration

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/telemetry/config` | Get telemetry collection settings |
| `PUT` | `/api/telemetry/config` | Update telemetry settings (tiers, thresholds, retention) |

---

## MCP Server Specification

The MCP server is registered as a tool provider that Claude Code (and other MCP-compatible agents) can discover and call.

### Server Identity

```json
{
  "name": "wallfacer-telemetry",
  "version": "1.0.0",
  "description": "Runtime telemetry from Wallfacer live-serve sessions"
}
```

### Tools

```json
[
  {
    "name": "telemetry_errors",
    "description": "Get recent error log entries from the running application. Returns structured errors with timestamps, messages, and stack traces.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "since_minutes": { "type": "integer", "default": 15 },
        "severity": { "type": "string", "enum": ["warn", "error", "fatal"] },
        "limit": { "type": "integer", "default": 50 }
      }
    }
  },
  {
    "name": "telemetry_latency",
    "description": "Get HTTP latency percentiles for the serve endpoint.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "window_minutes": { "type": "integer", "default": 5 },
        "endpoint": { "type": "string", "default": "/" }
      }
    }
  },
  {
    "name": "telemetry_health",
    "description": "Get current health probe result and recent probe history.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "history_count": { "type": "integer", "default": 10 }
      }
    }
  },
  {
    "name": "telemetry_status_codes",
    "description": "Get HTTP response status code distribution over a time window.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "window_minutes": { "type": "integer", "default": 5 }
      }
    }
  },
  {
    "name": "telemetry_traces",
    "description": "Get slow or errored trace summaries from the running application (requires OTLP tier).",
    "inputSchema": {
      "type": "object",
      "properties": {
        "min_duration_ms": { "type": "integer", "default": 1000 },
        "status": { "type": "string", "enum": ["error", "ok", "all"], "default": "error" },
        "limit": { "type": "integer", "default": 20 }
      }
    }
  },
  {
    "name": "telemetry_diff_since",
    "description": "Compare telemetry before and after a commit. Shows error rate delta, latency delta, and new error classes.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "commit_sha": { "type": "string", "description": "Git commit SHA to compare against" },
        "window_minutes": { "type": "integer", "default": 10 }
      }
    }
  }
]
```

### MCP Server Integration

The MCP server runs as part of the Wallfacer server process (not a separate binary). It listens on a configurable port (`WALLFACER_MCP_PORT`, default: auto-assigned) and is advertised to agent containers via:

1. **Environment variable**: `MCP_SERVERS` JSON injected into task containers pointing to the wallfacer MCP endpoint.
2. **Board manifest**: The `telemetry_mcp_url` field in the board manifest that agents already receive.

For Claude Code specifically, the MCP server config is written to `/workspace/.claude/mcp.json` in the container so Claude auto-discovers it.

---

## UI

### Telemetry Panel

A new panel accessible from the toolbar (alongside serve and terminal). Shows:

- **Timeline view**: Error events, probe results, and anomaly markers on a time axis. Correlated task IDs shown as clickable links.
- **Error list**: Recent errors grouped by message/class. Each entry shows count, first/last seen, correlated task.
- **Latency chart**: Simple sparkline of P50/P95 over time. Overlaid with commit markers showing when task changes were deployed.
- **Anomaly cards**: Active anomalies with severity badges. Actions: acknowledge, dismiss, "Fix this" (creates task).

### Task Card Integration

When telemetry anomalies are correlated to a task:
- The task card shows a small indicator badge (orange for warnings, red for errors).
- Clicking the badge opens the telemetry panel filtered to that task.
- The task detail view includes a "Runtime" tab showing telemetry correlated to this task's commits.

### Serve Integration

The existing serve log panel (from live-serve spec) gains a "Telemetry" tab that shows Tier 1-3 signals alongside raw logs.

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_TELEMETRY_ENABLED` | `false` | Enable telemetry collection (all tiers) |
| `WALLFACER_TELEMETRY_TIER` | `1` | Maximum telemetry tier (1=logs, 2=probes, 3=OTLP) |
| `WALLFACER_TELEMETRY_RETENTION_DAYS` | `7` | Days to retain telemetry data in SQLite |
| `WALLFACER_TELEMETRY_RING_SIZE` | `10000` | In-memory ring buffer size (entries) |
| `WALLFACER_TELEMETRY_PROBE_INTERVAL` | `10` | Seconds between health probes (Tier 2) |
| `WALLFACER_TELEMETRY_PROBE_ENDPOINT` | `/` | Health check endpoint path |
| `WALLFACER_TELEMETRY_OTLP_PORT` | `0` | OTLP receiver port (0 = auto-assign, Tier 3) |
| `WALLFACER_TELEMETRY_AUTO_DISPATCH` | `false` | Auto-create fix tasks from anomalies |
| `WALLFACER_TELEMETRY_ERROR_THRESHOLD` | `0.05` | Error rate threshold for anomaly detection (fraction) |
| `WALLFACER_TELEMETRY_LATENCY_MULTIPLIER` | `2.0` | Latency spike multiplier vs baseline |
| `WALLFACER_MCP_PORT` | `0` | MCP server port (0 = auto-assign) |

### Cloud-Specific Configuration

Cloud configuration lives in the tenant config (not env vars):

| Setting | Description |
|---------|-------------|
| `telemetry.otel_collector_endpoint` | OTEL Collector OTLP endpoint |
| `telemetry.retention_days` | Per-tenant retention (overrides default) |
| `telemetry.ingestion_rate_limit` | Signals per second per tenant |
| `telemetry.auto_instrument` | Enable OTEL auto-instrumentation injection |

---

## Implementation Phases

### Phase 1 — Ring buffer + structured log parsing (Local Tier 1)

| File | Change |
|------|--------|
| `internal/telemetry/` (new package) | `RingBuffer`, `Entry`, `Store` types |
| `internal/telemetry/logparser.go` | JSON lines and logfmt parser, error extraction |
| `internal/telemetry/sqlite.go` | SQLite persistence, retention pruning |
| `internal/store/models.go` | `TelemetryEntry`, `TelemetryAnomaly` types |

**Depends on:** Nothing. Can start immediately.
**Effort:** Medium.

### Phase 2 — Health probes (Local Tier 2)

| File | Change |
|------|--------|
| `internal/telemetry/probe.go` | HTTP prober goroutine, result recording |
| `internal/telemetry/anomaly.go` | Rule-based anomaly detection (error rate, latency, health) |

**Depends on:** Phase 1 (ring buffer), live-serve (serve session with port).
**Effort:** Low.

### Phase 3 — Correlation engine

| File | Change |
|------|--------|
| `internal/telemetry/correlate.go` | Git SHA → task ID mapping, anomaly → task attribution |
| `internal/store/models.go` | `TelemetryAnomaly` with `TaskID`, `CommitSHA` |

**Depends on:** Phase 1.
**Effort:** Low.

### Phase 4 — Telemetry API + UI

| File | Change |
|------|--------|
| `internal/apicontract/routes.go` | Register telemetry routes |
| `internal/handler/telemetry.go` | Query handlers (errors, probes, latency, anomalies) |
| `ui/js/telemetry.js` | Telemetry panel, anomaly cards, task card badges |

**Depends on:** Phase 1-3.
**Effort:** Medium.

### Phase 5 — MCP server

| File | Change |
|------|--------|
| `internal/mcp/` (new package) | MCP server implementation (JSON-RPC over stdio or HTTP) |
| `internal/mcp/tools.go` | Tool implementations wrapping telemetry queries |
| `internal/runner/` | MCP config injection into agent containers |

**Depends on:** Phase 4 (telemetry API). Agent abstraction spec (for clean injection).
**Effort:** Medium.

### Phase 6 — OTLP receiver (Local Tier 3)

| File | Change |
|------|--------|
| `internal/telemetry/otlp.go` | Lightweight OTLP HTTP/gRPC receiver |
| `internal/telemetry/otlp_translate.go` | OTLP → `TelemetryEntry` translation |
| `internal/runner/serve.go` | Inject `OTEL_EXPORTER_OTLP_ENDPOINT` into serve containers |

**Depends on:** Phase 1, live-serve.
**Effort:** Medium. OTLP proto handling is the main complexity.

### Phase 7 — Auto-dispatch

| File | Change |
|------|--------|
| `internal/telemetry/dispatch.go` | Anomaly → task creation, guard rails (rate limit, circuit breaker) |
| `internal/prompts/telemetry-fix.tmpl` | System prompt template for auto-dispatched fix tasks |

**Depends on:** Phase 3 (correlation), Phase 5 (MCP, so agents can query telemetry).
**Effort:** Low.

### Phase 8 — Cloud OTEL integration

| File | Change |
|------|--------|
| `internal/telemetry/cloud.go` | OTEL Collector config generation, PromQL/LogQL/TraceQL query client |
| `internal/sandbox/k8s.go` | Pod label injection (`wallfacer.task_id`, etc.) |
| K8s manifests | OTEL Collector DaemonSet, auto-instrumentation operator config |

**Depends on:** K8s sandbox spec, tenant API spec.
**Effort:** Large.

---

## Key Patterns Reused

| Pattern | Source | Reused For |
|---------|--------|------------|
| Ring buffer | (new) | In-memory telemetry with bounded memory |
| SQLite persistence | `internal/store/` patterns | Durable telemetry storage |
| SSE streaming | `internal/handler/stream.go` | `GET /api/telemetry/stream` |
| Anomaly → task creation | `internal/runner/` auto-retry | Auto-dispatch from anomalies |
| Container env injection | `internal/runner/container.go` | OTLP endpoint + MCP config injection |
| Prometheus metrics | `internal/metrics/` | Wallfacer's own telemetry (meta-level) |
| Board manifest | `internal/runner/manifest.go` | `telemetry_mcp_url` field for agent access |

---

## Potential Challenges

1. **OTLP proto dependency.** The OTLP protobuf schemas are large. Options: (a) vendor the proto files and generate Go code, (b) use `go.opentelemetry.io/proto/otlp` directly, (c) accept only OTLP/HTTP JSON (no gRPC proto needed). Option (c) is simplest for v1.

2. **MCP server specification maturity.** MCP is evolving. The implementation should be minimal and version-pinned, with a clear abstraction boundary so the transport can be swapped without touching tool logic.

3. **Noisy signals.** Structured log parsing will misclassify some log lines. The error extraction heuristic should be conservative (only extract lines with explicit error/fatal markers) and allow user-defined patterns.

4. **SQLite contention.** High-frequency telemetry writes could bottleneck on SQLite. Mitigate with WAL mode, batched inserts (flush ring buffer every N seconds), and a separate DB file from the main store.

5. **Agent prompt bloat.** Injecting telemetry context into prompts must be selective — full stack traces for 100 errors would blow the context window. The MCP tools should return summarized, deduplicated data with an option to drill into specific entries.

6. **Cloud multi-tenancy isolation.** OTEL Collector must enforce tenant label injection (tenants cannot spoof another tenant's labels). Use admission webhooks to validate pod labels match the tenant namespace.

7. **Auto-dispatch feedback loops.** A buggy auto-fix could introduce new errors, triggering more auto-dispatch. The circuit breaker (3 consecutive failures → disable) and rate limiter (1 per anomaly class per hour) mitigate this, but the UX must make it easy to halt the loop.

8. **Serve session dependency.** Tier 1-2 signals only flow when live-serve is running. For external deployments (not managed by Wallfacer), users need to configure OTLP export to point at Wallfacer's receiver — this is Tier 3 territory and requires documentation.

---

## Open Questions

1. **MCP vs tool-use API?** Claude Code supports MCP natively, but Codex sandbox may not. Should the telemetry tools also be exposed as regular tool-use functions in the agent system prompt (alongside the MCP server)? This would provide a fallback for non-MCP agents.

2. **Telemetry for non-serve workloads?** The spec focuses on live-serve (web servers). What about CLI tools, batch jobs, or test suites? CLI output is already captured in task logs — should the telemetry system also parse test output (assertion failures, coverage drops) as telemetry signals?

3. **Baseline computation window?** Anomaly detection needs a baseline. For a freshly started serve session, there is no baseline. Options: (a) no anomaly detection for first N minutes, (b) use configurable static thresholds until enough data accumulates, (c) use the previous session's baseline if the workspace fingerprint matches.

4. **Shared telemetry across tasks?** If Task A builds a server and Task B adds a feature, whose telemetry is it? The correlation engine uses git blame / commit SHA, but overlapping changes complicate attribution. May need a "shared" attribution category.

5. **OTEL Collector deployment model (cloud)?** DaemonSet (one per node, shared) vs sidecar (one per pod, isolated). Sidecar is simpler for tenant isolation but doubles pod resource usage. DaemonSet needs tenant-aware routing.

6. **Privacy and data sensitivity?** Telemetry may contain PII from application logs (user emails, IPs, etc.). Should the telemetry pipeline include a scrubbing/redaction step? For cloud, this is likely required. For local, the user owns all the data.

7. **Integration with external observability?** Users with existing Grafana/Datadog setups may want to *read* signals from those systems rather than collect their own. Should the telemetry API support pluggable backends (read from Prometheus remote-read, Loki, etc.) in addition to the built-in SQLite/ring buffer?

---

## What This Does NOT Require

- No changes to existing task execution or turn loop (telemetry is a parallel concern).
- No mandatory user code instrumentation (Tier 1-2 work without any app changes).
- No external service dependencies for local mode (SQLite + in-process).
- No changes to existing Prometheus metrics endpoint (that remains for Wallfacer's own ops metrics).
