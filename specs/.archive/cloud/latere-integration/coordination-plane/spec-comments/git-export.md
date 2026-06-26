---
title: Spec Comments Git Export
status: archived
depends_on:
  - specs/cloud/latere-integration/coordination-plane/spec-comments.md
affects:
  - internal/spec/
  - internal/handler/
  - frontend/src/components/plan/
effort: medium
created: 2026-06-14
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Spec Comments Git Export

The follow-up leaf that pays down the one cloud-lock-in introduced by
[spec-comments](../spec-comments.md). v1 comment threads are cloud-resident and
authoritative in the coordinator (the scoped exception to relay-not-mirror). This
leaf adds **export/import** so threads materialize into the repo and travel with
the project, restoring portability and offline access. It is deliberately `vague`:
the v1 schema was designed export-friendly so this lands without a rewrite, but
the reconciliation model needs design before it is `drafted`.

## Why it exists

- **Portability.** A team that later wants its review history in the repo (or to
  leave wf.latere.ai entirely) should not lose comments. Comments-in-git are
  yours, diffable, and survive the coordinator.
- **Offline.** A teammate with no coordinator connection (no network, no shared
  remote yet) can still read and author comments locally; the git copy is the
  durable substrate when the relay is unavailable.

## What export does

Materialize the coordinator's threads for a workspace into the repo as
append-friendly NDJSON, one file per spec:

```
.wallfacer/comments/<spec-path>.ndjson
```

Each line is one record in the **same flat v1 schema** (`Thread` / `Comment`,
ULID ids, `Anchor`), serialized verbatim, no id remap. Because ids are
coordinator-minted ULIDs and anchors are content-hash based with frozen
normalization (`internal/spec/` helpers, shared with the live path), the export
is a serializer over the existing records, not a transform.

Trigger options (decide when drafting): manual "export comments" action, on
resolve, or periodic. Manual-first is the likely default, so export is an
explicit, reviewable commit rather than churn on every keystroke.

## What import does

The inverse, for a repo whose `.wallfacer/comments/` holds threads the
coordinator does not have (a fresh clone, a teammate who authored offline, or a
re-onboarded org): read the NDJSON and push the threads up the coordination
connection. Dedup by ULID so re-importing is idempotent.

## Reconciliation model (the part that makes this non-trivial)

ULID ids make a merge id-stable: the same thread/comment has the same id in git
and in the coordinator, so reconciliation is per-record, not positional.

- **New on one side.** A thread present in git but not the coordinator (offline
  authoring) imports up; present in the coordinator but not git exports down.
- **Diverged.** The same comment edited both offline (git) and online (cloud).
  This is the real open question: last-write-wins by `edited_at`, or surface a
  conflict. Comment bodies are short and edits rare (the spec's own framing), so
  last-write-wins is the likely default, but it can silently drop an offline
  edit. Tombstone-on-delete (spec-comments open question 2) keeps the merge
  append-friendly.
- **Anchors.** Recompute on import against the current spec body with the shared
  normalization, the same reposition algorithm as the live path. A thread that
  orphans on import is marked orphaned, never dropped.

## Authority handoff

While connected, the coordinator stays the real-time authority (the relay needs a
single writer). Git is the durable, portable materialization that becomes the
working copy when offline or when no coordinator is configured. On reconnect, the
two reconcile by ULID per the model above. The precise handoff (does the cloud
copy stay after export, or does export "release" a thread to git) is open; the
default is **both keep their copy** and reconcile, so export is non-destructive.

## Non-goals

- Changing the v1 cloud-resident, real-time model. This leaf is additive.
- A CRDT merge of comment bodies. Last-write or conflict-surface, not character
  merge (inherited non-goal).

## Open questions

1. **Divergent-edit resolution.** Last-write-wins vs conflict-surface for a
   comment edited both offline and online. Lean last-write-wins given short, rare
   edits; confirm with the tombstone-on-delete decision.
2. **Export trigger.** Manual vs on-resolve vs periodic. Manual-first likely.
3. **Authority after export.** Both-keep-and-reconcile (default) vs release-to-git.
4. **File granularity.** One NDJSON per spec (proposed) vs one per workspace; the
   per-spec split keeps diffs local to the spec being reviewed.
