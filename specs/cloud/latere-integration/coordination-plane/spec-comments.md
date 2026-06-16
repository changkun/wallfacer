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
updated: 2026-06-16
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
  status       string   // "active" | "resolved" | "orphaned" | "outdated" (see Status lifecycle)
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
  commit_sha   string   // git commit the body was at when anchored (advisory, "" if uncommitted/unknown)
  blob_sha     string   // git blob hash of the spec file at author time (advisory, "" if unknown)
}
```

`section_path` is the human-stable heading trail. `line_hash` is the primary exact
key. `prefix` / `suffix` are the fuzzy reposition windows. `line_hint` is a hint for
ordering and UI scroll, never a source of truth.

### Advisory git metadata (`commit_sha`, `blob_sha`)

`commit_sha` and `blob_sha` record the git state the body was in when the anchor was
computed. They are **advisory**, the same trust level as `line_hint`: never the
source of truth, and the content hash resolves whether or not they are present. A
spec is usually commented against the working tree, where the on-screen text may have
no committed blob at all, so both fields may be empty and the anchor still resolves on
`line_hash`. They exist for three additive things:

1. **View as of.** "This comment was made at `abc123f`" in the popover, and, when the
   commit is locally reachable, a "view spec as of that commit" affordance
   (`git show <commit_sha>:<spec_path>`, run client-side).
2. **Outdated signal.** If `blob_sha` differs from the spec file's current blob, the
   file changed since the comment was made; the popover shows an "outdated" hint even
   when the anchored line itself still matches.
3. **Exact reposition fast-path.** When `commit_sha` is reachable in the local clone,
   the client maps the anchored line through the real `git diff <commit_sha>..HEAD --
   <spec_path>` hunks, an exact reposition that beats fuzzy matching. When the commit
   is not reachable (a teammate who has not pulled it, a shallow clone, the git-export
   consumer), the client falls back to the content-hash algorithm below. The git path
   is never required; it only improves accuracy when the history is present.

A commit or blob SHA is an opaque content-addressed ref, not source, not a diff, not a
path. It carries the same data-boundary status as `line_hash` (already an allow-listed
anchor field) and is allow-listed on the same basis: it names a version identity,
never spec contents. This keeps content-hash as the primary, portable anchor (the
locked decision) and adds the git association the user asked for as advisory metadata
on top, not a replacement.

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

For every active thread on the spec, recompute the anchor against the current body.
When the thread's advisory `commit_sha` is reachable in the local clone, try the
**git fast-path first**: map the anchored line through `git diff <commit_sha>..HEAD --
<spec_path>` and, if the hunk math lands cleanly on a single line, reattach there and
refresh `line_hash` / `line_hint`. Otherwise (commit unreachable, or the diff is
ambiguous) fall through to the content-hash steps, which are the portable path the
git-export consumer also uses:

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
4. **Orphan.** Nothing clears the threshold. Mark `status = "orphaned"` and remove the
   thread from the inline markers, so a lost anchor never keeps highlighting the spec
   (see Status lifecycle). The thread is not dropped and never silently mis-attached to
   a wrong line; it moves to the triage list, where a human re-places it onto the right
   line (rewriting the anchor back to `active`), or marks it `resolved` or `outdated`.

Reposition runs **client-side**, against the loaded spec body, because only the client
holds the spec source (the coordinator never does, per the parent's relay-not-mirror
invariant). The coordinator stores and relays the anchor fields verbatim; it never
recomputes them. Two clients viewing the same spec at the same git state compute the
same result because the normalization and hash are frozen and shared (the
`internal/spec/` helper and the future git-export path run identical code), so a thread
lands on the same line for everyone without a server-side authority on position.

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
view reveals resolved threads inline. Orphaned and outdated threads do not render
inline at all; they live in the triage list (next section), so the spec view is never
cluttered by threads whose anchor is lost.

## Status lifecycle and the spec highlight

`status` is the single field the UI keys on to decide whether a spec is "highlighted"
(a gutter marker, a header count that says "this spec has open comments"):

| Status | Set by | Inline marker? | Counts toward spec highlight? | In triage list? |
|---|---|---|---|---|
| `active` | reposition reattached the anchor | yes, on the anchored line | yes (when unresolved) | no |
| `resolved` | a human resolved it (addressed) | muted, hidden until "Show resolved" | no | no |
| `orphaned` | reposition lost the anchor | no | **no** | yes |
| `outdated` | a human triaged it as no-longer-relevant | no | no | no (terminal, archived) |

The load-bearing rule the user asked for: **a thread stops highlighting the spec the
moment its anchor is lost.** An orphaned thread does not sit on the spec as a stale
marker; it leaves the inline view entirely and surfaces only in the triage list. So
opening a spec is never flagged "has comments" because of a comment whose text is
gone. The header badge counts only `active && !resolved` threads.

`resolved` and `outdated` are both human verdicts that clear a thread, distinguished by
intent: `resolved` means "the point was addressed" (and can be reopened to `active` if
it re-anchors), `outdated` means "this no longer applies, file it away" (terminal; the
comment is kept for the audit trail and the future git-export, never re-anchored).
Both are reachable from the triage list; `resolve` is also reachable inline on an
active thread (the Resolve workflow above).

### The triage list

A per-repo "Comments needing attention" view, pulled from the cloud-authoritative
store (the same relay that delivers inline threads delivers the orphaned set; no new
transport). It lists every `orphaned` thread for the repo, plus any thread displaced by
a spec-lifecycle change (next section), each with: the original anchored-text snapshot,
the last-known `section_path`, the author and age, and the source spec. Per entry, a
human (editor/admin, or the thread author) can:

- **Re-place** onto a line in the current spec body. Recomputes the anchor from the
  dropped line and sets `status = active`; the thread rejoins the inline markers.
- **Resolve** (addressed) or **Mark outdated** (no longer applies). Either removes it
  from the list and from any spec highlight.

The list is the durable home for "we lost where this went, a human should look." It is
opt-in attention (a tab and a count), not a nag overlaid on the spec.

## Spec lifecycle (stale, archived, renamed)

Anchor loss is text-level. A spec also changes at the **document level**: its
frontmatter `status` moves to `stale` or `archived`, or the file is renamed / split /
superseded (the parent `intent` and `spec-archival` flows). Threads must not be
destroyed by any of these; they follow the spec.

| Spec change | Thread handling |
|---|---|
| `status: stale` | Threads stay `active`, still anchor and still highlight. A stale spec is live (just flagged as drifting); its open comments are exactly the signal that drift needs attention. No thread status change. |
| `status: archived` | The spec is hidden from the live graph (`SpecFocusedView` already treats archived as hidden). Its threads leave the inline view with it but are kept and surface in the triage list, markable `resolved` / `outdated`. Unarchiving the spec (the existing git-revert path) restores its `active` threads inline. |
| File renamed / split / superseded | `Thread.spec_path` goes stale and the anchor cannot resolve (the old path is gone). The thread orphans into the triage list anchored to the old path; a human re-places it onto the successor spec (rewriting both `spec_path` and the anchor) or marks it `outdated`. Automatic git-rename following stays a git-export-era concern (parent open question), but the manual re-place path works now and never loses the thread. |

The common thread: a document-level change can displace a comment just as a text edit
can, and both funnel to the same triage list with the same verdicts (re-place,
resolve, outdated). The spec's inline highlight only ever reflects threads that are
`active` on the current body.

## UI

All UI lives in `frontend/src/components/plan/` (the spec view), built on
`SpecFocusedView.vue`'s rendered body (`renderedBody`, `bodyRef`).

- **Inline markers.** A small comment marker in the gutter of the line a thread
  anchors to. Markers are positioned by mapping each thread's repositioned line onto
  the rendered DOM, the same `bodyRef`-watch pattern the mermaid and TOC passes already
  use (re-run on `renderedBody` and `bodyRef` change, since the out-in crossfade
  replaces `<main>`). Only `active`, anchored, unresolved threads render a marker;
  orphaned, outdated, resolved, and archived-spec threads never do. The unresolved-count
  badge in the header reflects exactly that set, so a spec is never flagged "has
  comments" for a comment whose anchor is lost.
- **Triage panel.** A per-repo "Comments needing attention" view (a tab and a header
  count, never an overlay on the spec) lists orphaned and lifecycle-displaced threads
  pulled from the cloud store, each with its original text snapshot, last-known section,
  author, and source spec. Per entry: re-place onto a current line, resolve, or mark
  outdated. This is where a human clears the threads the inline view deliberately stops
  showing.
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
  durable comment store. The relayed thread set for a repo includes orphaned and
  lifecycle-displaced threads (not just inline-anchored ones) so the client can build
  the triage list without a second fetch.
- `internal/spec/`: the normalization and anchor helpers (compute `line_hash`,
  `prefix`/`suffix`, the advisory `commit_sha`/`blob_sha` capture, and the client-side
  reposition algorithm, content-hash steps plus the optional `git diff` fast-path,
  against a spec body). Shared by the client and the future git-export path (not the
  coordinator, which holds no source), so it lives with the spec model, not in a
  handler.
- `frontend/src/components/plan/`: markers, popover, avatars, the "Show resolved"
  toggle, and the selection-to-comment affordance on `SpecFocusedView.vue`.

### Durable store (Postgres, new infra dependency)

Comments are cloud-**authoritative** (the one relay-not-mirror exception), so their
system of record must be **durable**, not the shared Valkey cache: a cache evicts
under memory pressure and would silently drop system-of-record data. Authoritative
threads therefore persist in **Postgres** (`latere-pg`). Valkey is used only for the
real-time relay (pub/sub fan-out, presence-aware delivery), never as the store.

This was a new infra dependency (wallfacer was filesystem-storage, with no database
on `latere-pg` unlike auth/cella/fs/lux/web) and is now **provisioned**: a
`wallfacer` database on `latere-pg` plus a `wallfacer-db` secret exposing
`WALLFACER_DATABASE_URL`, wired into the wallfacerd deployment (the same pattern
every other service uses). The implementation owns the schema, migrations, and the
read/write path against that database; the relay still rides Valkey. The store is
shared with [metadata-projection](metadata-projection.md)'s rollup tier.

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
4. After an edit that destroys the anchored text, the thread is marked `orphaned`,
   **leaves the inline markers, and stops counting toward the spec's comment
   highlight**, so the spec is never flagged as having comments because of a lost
   anchor. It is never silently dropped or mis-attached; it appears in the triage list
   for a human to re-place, resolve, or mark outdated.
5. A viewer cannot resolve, reopen, or reply (403 at the coordinator); an editor and
   the thread author can resolve and reopen; the change relays to peers.
6. Resolved threads collapse to a muted marker and are hidden until "Show resolved" is
   toggled; reopen restores the active marker.
7. The client anchor helper in `internal/spec/` and the future git-export serializer
   compute the same `line_hash` for a given normalized line, verified by a
   shared-fixture test (the portability property the git-export path depends on). The
   coordinator stores and relays the hash but never computes it (it holds no source).
8. A comment popover shows the commit it was made at; when that commit is locally
   reachable the spec can be viewed as of it; when the file blob changed, an outdated
   hint shows even if the anchored line still matches. None of this is required for the
   anchor to resolve: a comment made against uncommitted working-tree text (empty
   `commit_sha`/`blob_sha`) still anchors on content.
9. Archiving a spec removes its threads from the inline view and keeps them in the
   triage list; unarchiving restores its `active` threads inline. A renamed or
   superseded spec's threads orphan into the triage list and can be re-placed onto the
   successor spec.
10. Marking an orphaned thread `outdated` (or `resolved`) removes it from the triage
    list and from any spec highlight; the thread is retained (not hard-deleted) for the
    audit trail and the future git-export.

## Open questions

1. **Orphan re-place UX (residual risk).** The anchoring thresholds are decided
   (prefer-orphan, see "Threshold (decided, tunable)"), and the surfacing model is now
   decided too: orphaned threads leave the inline view and collect in the triage list
   (Status lifecycle), so a lost anchor never nags on the spec. What stays open is
   whether anyone is **proactively notified** when a thread orphans (nobody by default,
   or the thread author?), and triage-list ergonomics once the list is long (grouping,
   bulk mark-outdated, age sort). No anchor survives an arbitrary rewrite of its text;
   the triage list is the floor, its at-scale ergonomics need design.
2. **Comment edit and delete.** v1 allows author edit (`edited_at`). Whether to allow
   hard delete (versus tombstone for the audit trail) is deferred; tombstone is the
   likely answer to keep the export append-friendly.
3. **Retention.** How long the coordinator keeps resolved threads before the
   git-export path can offload them is a coordinator-store decision, shared with
   `metadata-projection.md`.
4. **Cross-spec move.** A spec file renamed or split in git. The thread's `spec_path`
   goes stale; the thread now orphans into the triage list and a human can re-place it
   onto the successor spec (Spec lifecycle), so it is never lost. What stays deferred is
   **automatic** recovery (following git rename detection to re-anchor without human
   action), a git-export-era concern.
