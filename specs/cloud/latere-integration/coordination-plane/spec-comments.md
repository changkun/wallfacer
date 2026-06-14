---
title: Inline Spec Comments
status: drafted
depends_on:
  - specs/cloud/latere-integration/coordination-plane.md
affects:
  - internal/handler/
  - internal/spec/
  - frontend/src/components/plan/
effort: large
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Inline Spec Comments

Capability 4 of the [Cloud Coordination Plane](../coordination-plane.md): teammates
on the same workspace comment on spec lines, see who commented, reply, and resolve,
in real time. This is the **one scoped exception** to relay-not-mirror. Comment
threads are **cloud-resident and authoritative in the coordinator** in v1, relayed
to connected peers. The coordinator stays non-authoritative for local task and spec
*data*; it owns only this collaboration artifact.

The locked decisions from the parent (do not re-open here):

- Storage is hybrid: cloud-now, git-export-later. v1 threads live in the coordinator.
- The v1 schema is export-friendly (stable ids, content-hash anchors) so the later
  git-export leaf is not a rewrite.
- Comments are attributed via `ActorSub`, the same actor model as task events.
- Teammates are joined by the cross-machine workspace identity (normalized git
  remote URL), not the per-machine path fingerprint.
- Comments ride the **one** coordination connection (the outbound WSS from each local
  instance to the coordinator). No separate transport.

This spec does not re-spec the connection, presence, or RBAC. It consumes them. The
connection, heartbeat, and registry come from `connection-and-presence.md`; the
admin/editor/viewer scopes come from `identity/multi-user-collaboration.md`'s RBAC
matrix.

## Scope

- A comment data model anchored to a spec file plus a line or section.
- Anchoring that survives the underlying spec changing in git (the hard part).
- Real-time relay of create/resolve/reply to peers viewing the same workspace/spec.
- A resolve/reopen workflow with a defined permission gate.
- Inline markers, a thread popover, and author avatars in the spec view.

Out of scope (named so they do not creep in): git-export materialization (a future
leaf, one paragraph below), CRDT co-editing of spec text (parent non-goal), comments
on tasks or planning chat (those have their own attribution surfaces).

## Data model

A **thread** is the anchored unit. A thread carries one or more **comments**; the
first comment is the thread root, replies attach by `parent`. Anchoring lives on the
thread, not the comment, so a reply never re-anchors.

Ids are **coordinator-minted ULIDs**: sortable, no database sequence, stable across
an NDJSON export with no remapping. That is the property the later git-export path
needs, so it is locked in v1.

```
Thread {
  id           string   // ULID, coordinator-minted, stable
  workspace_id string   // normalized git remote URL (cross-machine key)
  spec_path    string   // workspace-relative, e.g. "specs/cloud/x.md"
  anchor       Anchor   // see Anchoring; pins to a line or section
  author_sub   string   // ActorSub of the thread opener
  created_at   time.Time
  resolved     bool
  resolved_by  string   // ActorSub, empty until resolved
  resolved_at  time.Time
  status       string   // "active" | "resolved" | "orphaned" (anchor lost)
}

Comment {
  id         string    // ULID, coordinator-minted, stable
  thread_id  string    // -> Thread.id
  parent_id  string    // "" for the root comment; else a sibling Comment.id
  author_sub string    // ActorSub
  body       string    // markdown text, rendered client-side
  created_at time.Time
  edited_at  time.Time // zero until edited
}
```

`workspace_id` is the join key for org collaboration. A workspace with no git remote
is local-only and never gets a `workspace_id`, so it never appears in cross-machine
comments (parent open question 5).

`author_sub` / `resolved_by` are `ActorSub` values, resolved by the coordinator from
the principal JWT on the connection, never trusted from the wire. Comment bodies are
markdown rendered client-side with the existing `renderMarkdown` path.

The export-friendly contract: `{id, thread_id, parent_id, workspace_id, spec_path,
anchor, author_sub, body, timestamps, resolved*}` is a flat, append-friendly record.
The later git-export leaf serializes the same fields to
`.wallfacer/comments/<spec>.ndjson` with no id remap and no schema change. Export is
future; it is named here only to confirm the v1 schema does not block it.

## Anchoring across spec edits

The hard part. A thread pins to a line or section of a spec that then changes in git.
The anchor must recompute identically whether the coordinator holds the spec text or a
future git-export materializer holds it, so the anchor is computed against the
**canonical source markdown** (the post-frontmatter body, normalized), never the
rendered DOM. Specs carry no stable section ids today (`internal/spec/model.go` is
frontmatter plus an opaque `Body`; `FloatingToc.vue` derives headings from rendered
output at view time), so the anchor is reconstructed from content, not from ids the
file does not have.

### Anchor fields

```
Anchor {
  section_path []string // heading trail, e.g. ["Anchoring", "Anchor fields"]
  line_hash    string   // sha256 of the normalized anchored line or range (primary)
  prefix       string   // up to 3 normalized lines before (fuzzy context)
  suffix       string   // up to 3 normalized lines after (fuzzy context)
  line_hint    int      // last-known line number (advisory only, never trusted)
}
```

`section_path` is the human-stable heading trail. `line_hash` is the primary exact
key. `prefix` / `suffix` are the fuzzy reposition windows. `line_hint` is a hint for
ordering and UI scroll, never a source of truth.

### Normalization (must be identical cloud and git)

Before hashing or comparing, each line is normalized so the coordinator and a future
git-export path compute the same hash:

1. Strip trailing whitespace.
2. Collapse runs of internal whitespace to a single space.
3. Apply Unicode NFC.

Markers are content, not noise: list bullets and heading hashes are kept, only
whitespace and Unicode form are normalized.

The hash is `sha256(normalized_line)` (or the joined normalized range for a
multi-line anchor). Normalization is part of the schema contract: changing it later
invalidates stored hashes, so it is frozen with v1.

### Reposition algorithm (on each spec load for a workspace/spec)

For every active thread on the spec, recompute the anchor against the current body:

1. **Exact, unique.** `line_hash` matches exactly one line in the body. Reattach
   there, refresh `line_hint`. Done.
2. **Exact, ambiguous.** `line_hash` matches more than one line. Pick the candidate
   whose `prefix`/`suffix` context best matches, above the similarity threshold.
   Reattach, refresh hint.
3. **Fuzzy within section.** No exact hash match. Locate the section by
   `section_path` (heading slug match, tolerant of a renamed leaf heading by matching
   the trail prefix). Within that section, score lines by combined `prefix`/`suffix`
   similarity. If the best score clears the threshold, reattach and update
   `line_hash` to the new line.
4. **Orphan.** Nothing clears the threshold. Mark `status = "orphaned"`, keep the
   thread visible pinned at the last-known section heading with a "re-place"
   affordance (the author or any editor drops it onto the right line, which rewrites
   the anchor). Never silently drop a thread; never silently mis-attach to a wrong
   line.

Reposition runs in the coordinator when it relays a thread set for a spec, and is
re-run client-side against the loaded body so the marker lands on the rendered line.
Both sides use the same normalization, so they agree.

### Threshold (decided, tunable)

The policy is **prefer-orphan over mis-attach**: a visible "re-place" affordance is
safer than a silently wrong pin. Concrete starting values, tuned later on real edit
traffic:

- **Step 2 (exact-ambiguous disambiguation):** pick a candidate only if its
  combined `prefix`/`suffix` context similarity is >= **0.6** and beats the
  runner-up by a clear margin; otherwise fall through.
- **Step 3 (fuzzy within section):** reattach only if the best line's combined
  `prefix`/`suffix` similarity is >= **0.8** (token-level Jaccard over normalized
  lines); otherwise orphan.

Similarity is computed on the frozen-normalized lines so cloud and the future
git-export path score identically. These numbers are deliberately conservative
(high bar to move a pin) and live in one place so tuning is a constant change, not
a redesign.

The genuinely-open residual is the **orphan re-place UX**: who is nudged when a
thread orphans, and how a stale thread is surfaced without nagging. Tracked below.

## Real-time relay

Two hops, kept distinct.

**Hop 1, instance to coordinator (the relay).** Create, reply, resolve, and reopen
events ride the one outbound WSS from `connection-and-presence.md`. The coordinator is
authoritative: it mints the ULID, stamps `author_sub` / `resolved_by` from the
connection principal, applies the resolve permission gate, then fans the event out to
**other instances registered on the same `workspace_id`**. Fan-out is
presence-aware: an instance only needs the push if one of its connected browsers is
focused on that spec, so the coordinator filters by the focus hints presence already
reports (parent presence capability). An instance not serving anyone on that spec gets
the event lazily on next load.

**Hop 2, instance to browser.** The local instance relays the event to its browsers
over the **existing SSE on `/api/tasks/stream`**, the same channel presence events
use. No new browser socket (consistent with multi-user-collaboration's "WebSocket is
not introduced" decision). A new SSE event type:

| Event | Direction | Payload |
|---|---|---|
| `spec-comment` | coordinator -> instance -> browser | `{op: "create" \| "reply" \| "resolve" \| "reopen", thread, comment?}` |

The browser applies the op to its in-view thread set and re-runs client-side
reposition for the affected spec. A thread created while a peer is mid-scroll appears
as a new marker without a reload.

## Resolve workflow

Resolution maps onto the existing RBAC scopes from
`identity/multi-user-collaboration.md` (admin / editor / viewer), not a new gate:

- **Resolve a thread:** any **editor** or **admin**, plus the thread **author** even
  if only an editor on their own thread. Viewers cannot resolve.
- **Reopen:** symmetric, same set. Reopen clears `resolved` / `resolved_by` /
  `resolved_at` and sets `status` back to `active`.
- **Reply:** editors and admins; viewers read-only.
- **Edit a comment body:** the comment author only (sets `edited_at`). Admins do not
  edit others' words; they resolve or reopen.

The coordinator enforces the gate (it holds the authoritative state); the instance and
browser enforce the same gate for UI affordance only, never as the security boundary.
The gate check reuses the scope wrappers, it does not redefine them.

**Resolved rendering.** A resolved thread collapses its marker to a muted/checked
state and its popover defaults to collapsed. A "Show resolved" toggle in the spec
view reveals resolved threads inline. Orphaned threads render distinctly (a warning
marker at the last-known section) so they are not mistaken for resolved.

## UI

All UI lives in `frontend/src/components/plan/` (the spec view), built on
`SpecFocusedView.vue`'s rendered body (`renderedBody`, `bodyRef`).

- **Inline markers.** A small comment marker in the gutter of the line a thread
  anchors to (or at the section heading for an orphaned thread). Markers are
  positioned by mapping each thread's repositioned line onto the rendered DOM, the
  same `bodyRef`-watch pattern the mermaid and TOC passes already use (re-run on
  `renderedBody` and `bodyRef` change, since the out-in crossfade replaces `<main>`).
  An unresolved-count badge sits in the header.
- **Thread popover.** Clicking a marker opens a popover anchored to the line: the
  comment list (root plus replies, threaded by `parent_id`), a reply box for
  editors/admins, and a Resolve/Reopen button gated by the workflow above. Selecting
  text in the body and choosing "Comment" opens a new-thread popover that captures the
  anchor from the selected line range.
- **Author avatars.** Each comment row shows the author's avatar and name, resolved
  from `author_sub` via the same member/avatar source presence and attribution use
  (`/api/org/members` cache). Service or unknown actors render the muted chip, same as
  the timeline.
- **Presence tie-in.** Peers currently focused on the same spec are already in the
  presence list; a teammate's live reply lands in the open popover via the
  `spec-comment` SSE event without a refresh.

## Backend touch points

- `internal/handler/`: the SSE `spec-comment` event on the existing tasks stream, and
  the instance side of the relay (forward browser-initiated create/reply/resolve up
  the coordination connection, forward coordinator events down to browsers). The
  authoritative store is the coordinator, not the instance, so the instance adds no
  durable comment store.
- `internal/spec/`: the normalization and anchor-recompute helpers (compute
  `line_hash`, `prefix`/`suffix`, run the reposition algorithm against a spec body).
  This is shared by the coordinator relay and the future git-export path, so it lives
  with the spec model, not in a handler.
- `frontend/src/components/plan/`: markers, popover, avatars, the "Show resolved"
  toggle, and the selection-to-comment affordance on `SpecFocusedView.vue`.

The coordinator-side authoritative store (where threads persist, retention) is a
coordinator concern shared with `metadata-projection.md`'s read-model store decision;
it is referenced here, not specified.

## Future: git-export (separate leaf)

[git-export.md](spec-comments/git-export.md) (vague) adds export/import so threads
materialize into the repo (`.wallfacer/comments/<spec>.ndjson`) and travel with the
project, restoring portability and offline access. The v1 schema above (ULID ids,
content-hash anchors, flat records, frozen normalization) is designed so that path
is a serializer, not a rewrite.

## Acceptance criteria

1. A teammate on the same git-remote workspace, focused on the same spec, sees a new
   comment marker within 2 seconds of a peer creating it, with no reload.
2. A comment is attributed to the creator's `ActorSub`; the author avatar and name
   render in the popover; an unknown actor renders the muted chip.
3. After a spec edit that moves the anchored line within its section, the thread
   re-anchors to the correct line (exact-hash or fuzzy path), and `line_hash` updates.
4. After a spec rewrite that destroys the anchored text, the thread is marked
   `orphaned`, stays visible at its last-known section, and is never silently dropped
   or mis-attached.
5. A viewer cannot resolve, reopen, or reply (403 at the coordinator); an editor and
   the thread author can resolve and reopen; the change relays to peers.
6. Resolved threads collapse to a muted marker and are hidden until "Show resolved" is
   toggled; reopen restores the active marker.
7. The coordinator computes the same `line_hash` for a given normalized line as the
   anchor helper in `internal/spec/`, verified by a shared-fixture test (the property
   the future git-export path depends on).

## Open questions

1. **Orphan re-place UX (residual risk).** The anchoring thresholds are decided
   (prefer-orphan, see "Threshold (decided, tunable)"). What stays open is the
   re-place experience: who is nudged when a thread orphans, and how a stale thread
   is surfaced without nagging. No anchor survives an arbitrary rewrite of its text;
   the re-place affordance is the floor, and its UX needs design.
2. **Comment edit and delete.** v1 allows author edit (`edited_at`). Whether to allow
   hard delete (versus tombstone for the audit trail) is deferred; tombstone is the
   likely answer to keep the export append-friendly.
3. **Retention.** How long the coordinator keeps resolved threads before the
   git-export path can offload them is a coordinator-store decision, shared with
   `metadata-projection.md`.
4. **Cross-spec move.** A spec file renamed or split in git. The thread's `spec_path`
   goes stale; recovering it (follow git rename detection) is a git-export-era concern,
   noted not solved.
