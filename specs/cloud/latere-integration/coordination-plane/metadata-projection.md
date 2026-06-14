---
title: Coordination Metadata Projection
status: drafted
depends_on:
  - specs/cloud/latere-integration/coordination-plane.md
affects:
  - internal/store/
  - internal/cli/web.go
  - frontend/src/
effort: large
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Coordination Metadata Projection

Phase 2 of the [coordination plane](../coordination-plane.md). Each signed-in,
opted-in instance taps its `store.TaskEvent` stream, redacts to an allow-list,
and pushes a derived read-model over the one outbound connection from
[connection-and-presence](connection-and-presence.md). The coordinator on
wf.latere.ai assembles those pushes into an org-scoped projection that powers
history, usage, and team-visibility dashboards.

This is a **projection, never a mirror** (anchor, "relay + projection"). It is
regenerable by replay, never written back to an instance, never the system of
record. Pull the plug on wf.latere.ai and every instance keeps working; only the
org dashboards go dark.

## 1. The allow-list

The leak surface is `TaskEvent.Data` (a `json.RawMessage`) and the sensitive
fields of the `Task` snapshot. The projection is **not** the event blob
forwarded. It is a typed envelope built field by field. `TaskEvent.Data` is
**never forwarded**, in any form, for any event type. The stream is the trigger
and the sequence source only.

### What crosses (enumerated)

Per-task projected record (`ProjectedTask`), one per task, updated on event:

| Field | Source | Justification against the boundary |
|-------|--------|-------------------------------------|
| `task_id` | `Task.ID` (uuid) | Opaque identifier, no content. Joins records across pushes. |
| `org_id` | `Task.OrgID` | The partition key. Scopes the projection to the org. |
| `created_by` | `Task.CreatedBy` (JWT `sub`) | Actor identity, already an org-known principal. Powers "who". |
| `title` | `Task.Title` | User-authored label, not source/diff/path. Makes the dashboard legible (anchor enumerates titles as IN). Residual risk: free text can incidentally contain a path or secret string; opt-in is the control, and the allow-list test pins that no other free-text field rides. |
| `status` | `Task.Status` | One of the fixed `TaskStatus` enum values. No content. |
| `kind` | `Task.Kind` | Fixed enum (task / idea-agent / planning / routine). Dimension. |
| `flow_id` | `Task.FlowID` | Slug. Dimension for "what kind of work". |
| `sandbox` | `Task.Sandbox` | Harness id (claude / codex). Dimension. |
| `model` | `Task.ModelOverride` / resolved | Model name (e.g. claude-opus-4-6). Enables cost-by-model rollups. |
| `usage` | `Task.Usage` | Token counts + `cost_usd`. The usage rollup. Aggregate numbers. |
| `usage_breakdown` | `Task.UsageBreakdown` | Per-`SandboxActivity` token/cost. Cost-by-activity. |
| `turns` | `Task.Turns` | Integer count. |
| `failure_category` | `RetryRecord.FailureCategory` | Fixed enum (timeout / budget / ...). No content. |
| `created_at` / `started_at` / `updated_at` / `retired_at` | `Task` timestamps | Timeline. No content. |
| `actor_sub` / `actor_type` | `TaskEvent.ActorSub` / `ActorType` | Who caused each transition; fixed `ActorType` enum. |

Per-event projected record (`ProjectedEvent`), the timeline derivation. For each
`TaskEvent`, exactly one typed scalar set is extracted by `EventType`; the
`Data` blob is dropped:

| EventType | Extracted | Dropped (never crosses) |
|-----------|-----------|--------------------------|
| `state_change` | the new `TaskStatus` | everything else in Data |
| `span_start` / `span_end` | `phase`, `label` (fixed-shape `SpanData`, no free text) | |
| `prompt_round` / `prompt_round_revert` | round number only | `prev_prompt`, `new_prompt`, `resume_hint` (these ARE the prompt; forbidden) |
| `output` | nothing (count only) | the agent output payload (forbidden) |
| `feedback` | nothing (count only) | the user feedback text (forbidden) |
| `error` | nothing, plus `failure_category` if classified | the error message / stack (may contain paths; forbidden) |
| `system` | nothing (count only) | the system message text |

Org-level spec-tree shape, pushed from `collectSpecTree` (handler/specs.go), not
the task stream: per spec node, the `Title`, `Status` (fixed `spec.Status`
enum), and `Progress` counts; plus tree shape (parent/child edges, node count).
The spec **body** (markdown) never crosses, only titles, statuses, and counts.

### What is EXCLUDED (never crosses, by name)

From `Task`: `Prompt`, `PromptHistory`, `Result`, `StopReason`, `SessionID`,
`SnapshotDiffs`, `WorktreePaths`, `BranchName`, `CommitHashes`,
`BaseCommitHashes`, `CommitMessage`, `RefineSessions`, `CurrentRefinement`,
`CustomPassPatterns`, `CustomFailPatterns`, `LastTestResult`,
`PendingTestFeedback`, and the entire `Environment` block (`ContainerImage`,
`ContainerDigest`, `APIBaseURL`, `InstructionsHash`, repo paths in
`WorktreePaths` keys).

From the event stream: the raw `TaskEvent.Data` blob for every event type.

The boundary rule (data-boundary-enforcement): source, diffs, agent output,
secrets, env vars, and repo paths NEVER cross. The exclusion list above is the
concrete realization of that rule for the `Task` schema. It carries a regression
test (below).

## 2. The push mechanism

### Tap

A projection tap subscribes to the store via `Subscribe()` (the
`pubsub.Sequenced[TaskDelta]` feed). Each delta carries a clone of the mutated
`Task` and a sequence number. The tap reads the relevant `TaskEvent`s for the
delta where per-event timeline records are needed.

### Redact

For each delta the tap builds `ProjectedTask` (and any `ProjectedEvent`s) field
by field per the allow-list. No struct is forwarded wholesale. A single
`redact(*Task) ProjectedTask` function is the one place fields are copied, so
the allow-list is enforced in one auditable spot, the same shape as the RUM
scrubber in data-boundary-enforcement.

### Push, batch, debounce

The tap holds no second connection. It hands the redacted envelope to the
connection-and-presence transport (one outbound WSS). Debounce: use
`SubscribeWake()` (capacity-1, coalesces rapid bursts) to drive a flush loop
that batches all `ProjectedTask` updates accumulated since the last flush into
one message (default flush interval ~1s, configurable). A burst of N events on
one task collapses to one push of its latest projected state. Each push carries
the source `seq` (the delta sequence number) so the coordinator can detect gaps.

### Replay to rebuild on reconnect

Two paths, distinguished by whether the gap is recoverable from the bounded
delta buffer:

- **Warm reconnect (gap small).** On reconnect the instance sends its last
  acked `seq`. It calls `DeltasSince(seq)`; if the buffer still holds those
  deltas, it replays just the delta tail, redacted, and resumes. Cheap.
- **Cold rebuild (gap-too-old, or coordinator restart).** `DeltasSince` returns
  the gap-too-old signal, or the coordinator lost its in-memory cursor. The
  instance re-derives the **full** projection from current state (the task list,
  and `LoadEvents` where per-event records are needed), pushes a snapshot tagged
  as a full rebuild, and the coordinator replaces that instance's slice of the
  org projection wholesale. This is what makes the projection regenerable: the
  read-model is always reconstructible from local truth, never the source of
  truth itself.

The coordinator treats every push as idempotent on `(instance_id, task_id,
seq)`: a replayed delta or a full rebuild overwrites, never double-counts.

## 3. The coordinator-side read-model

Org-scoped. The coordinator maintains, per `org_id`, a projection assembled from
every connected instance's pushes:

- **Live task index.** `ProjectedTask` keyed by `task_id`, the union across the
  org's instances. Powers team-visibility (who is running what, current
  statuses) and history (retired tasks).
- **Usage rollups.** Aggregates that mirror the local `usageResponse` shape
  (handler/usage.go: `Total`, `ByStatus`, `BySubAgent`, `TaskCount`) but at org
  scope, plus a by-`created_by` (per-member) and by-`model` cut. Derived purely
  from projected `usage` / `usage_breakdown`, no raw content needed.
- **Spec-tree snapshot.** Per workspace (joined on git-remote identity from
  connection-and-presence), the latest pushed tree shape + titles + statuses +
  progress counts.

### Retention (decided)

- **Live projected task index:** keep the current/last-known `ProjectedTask` per
  task indefinitely while its instance is known to the org (it is small, bounded
  by task count, and is the team-visibility surface).
- **Per-event timeline records (`ProjectedEvent`):** rolled up into the
  per-task and usage aggregates, then the raw projected events are discarded
  after a **90-day** window. History dashboards beyond 90 days read from the
  rollups, not raw events.
- **Usage rollups:** kept long-term (daily/weekly buckets) since they are small
  and are the billing/visibility value. Default cap: 13 months of buckets.
- On instance disconnect the projection is retained (history does not vanish
  when a laptop closes); it is refreshed on the next reconnect via the rebuild
  path.

### Storage substrate (decided)

**A thin coordinator-owned store on wf.latere.ai**, co-located with the
wallfacerd coordinator. Rejected alternatives and why:

- **Identity org metadata.** Consume-don't-absorb cuts the other way here:
  stuffing wallfacer's derived task/usage read-model into the Identity service
  would make Identity carry wallfacer-domain data it does not own. The
  coordinator owns wallfacer's own concepts, so it owns their projection.
- **FS (fs.latere.ai).** A file data plane, not a queryable store; dashboards
  need aggregation queries (group-by member / model / status / time bucket) that
  a blob plane does not serve.

The store is whatever the coordinator process already has available
(co-located embedded DB or a small managed instance); it holds only the
projected read-model, never source or content, so its blast radius is the
allow-listed metadata only. It is rebuildable from instance pushes, so it is a
cache of record, not a system of record. Retention is the policy decided above
(live index indefinitely, raw projected events 90 days, rollups 13 months); a
durable store keeps the rollups cheaply, confirming those windows.

## 4. Dashboards on wf.latere.ai

Strictly from the projection. Nothing reads back into an instance.

- **Team activity.** Org members and their current/recent tasks: title, status,
  kind, actor, timestamps. The live presence list (from
  connection-and-presence) overlays "online now" on this view.
- **Usage rollups.** Org spend and token usage over time, broken down by member
  (`created_by`), by status, by sub-agent activity, and by model. Mirrors the
  local usage view at org scope.
- **History.** Retired/completed tasks over the retention window, with
  per-task timelines derived from the projected event scalars (status
  transitions, span phases, counts of output/feedback rounds), never the
  content of those events.
- **Spec visibility.** The org's spec tree shape with titles, statuses, and
  progress counts, per shared workspace.

The dashboards live under the wf.latere.ai SPA (`frontend/src/`), gated to cloud
mode, reading a coordinator API over the projection. No dashboard surfaces any
excluded field; a dashboard that needs source, a diff, or a prompt is out of
scope by construction (that data never left the instance).

## Test plan

- **Allow-list regression (the boundary control).** A test feeds a `Task`
  populated with content in every excluded field (Prompt, Result, SnapshotDiffs,
  Environment, CommitMessage, ...) plus events with content-bearing `Data`,
  runs `redact`, and asserts the projected envelope contains none of those
  bytes and is a subset of the enumerated allow-list keys. Same shape and intent
  as the RUM scrubber test.
- **Replay parity.** A warm replay from `DeltasSince` and a cold full rebuild
  from task state produce the same projected slice for a given instance.
- **Debounce.** N events on one task within the flush interval coalesce to one
  push of the latest state.
- **Idempotency.** Re-pushing the same `(instance_id, task_id, seq)` does not
  double-count usage rollups.
- **Opt-out / anonymous.** With coordination opt-in off (or anonymous), the tap
  is not started and nothing is pushed (egress zero), mirroring the
  data-boundary cloud-mode gate.
