---
title: "Periodic cross-tree staleness scan"
status: drafted
depends_on: []
affects:
  - internal/handler/tasks.go
  - internal/handler/explorer.go
  - ui/js/spec-explorer.js
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
effort: small
---

# Periodic Cross-tree Staleness Scan

Advisory background check that catches drift the event-driven hooks
miss: code edits outside the spec-dispatch/chat-edit flow.

Complementary to the three event hooks (chat, dispatch, task-done).
Does **not** mutate spec status — only surfaces advisory badges.

---

## Coverage Gap

The event hooks cover:
- Chat-edit fan-out ([chat-edit-fanout.md](chat-edit-fanout.md)) —
  spec edits via planning chat.
- Task-done drift pipeline ([drift-pipeline.md](drift-pipeline.md)) —
  code edits via dispatched tasks.

They miss:
- A developer runs `vim internal/runner/execute.go`, commits. No task,
  no chat. Spec-level fan-out doesn't fire.
- A refactor touches many files that multiple specs cover, landed as
  a direct commit.
- Git history rewrites, rebases, cherry-picks that bring in foreign
  changes.

The periodic scan plugs this hole.

---

## Trigger

Three trigger paths, picked pragmatically:

1. **Workspace load** — run once when the server opens a workspace (or
   switches to a new group). Surfaces drift that accumulated while
   wallfacer was offline.
2. **Manual refresh** — UI button in the explorer header
   ("Rescan staleness"). Triggered by humans on demand.
3. **Periodic interval** — optional, gated behind a config flag
   (e.g., `WALLFACER_STALENESS_SCAN_INTERVAL=1h`). Off by default;
   workspace-load + manual covers most cases.

Cron-style background scans are deferred — they add a moving part
(goroutine lifecycle, cancellation) that isn't needed at current scale.

---

## Algorithm

For each non-archived `complete` spec in the tree:

1. Parse `affects`. For each entry, resolve to filesystem paths (file
   or directory).
2. Query `git log --since=<spec.updated> --name-only -- <paths>` in
   the workspace. If any commit touched any `affects` path since the
   spec's `updated` timestamp, the spec is a **staleness candidate**.
3. Emit an advisory flag in the spec-tree API response:
   `stale_candidate: true` with a reason (e.g.,
   `"affects path internal/runner/execute.go changed in commit abc123"`).

The explorer renders the flag as a subdued "⚠ stale candidate" badge
distinct from the real `stale` status. Clicking opens the focused view
with the reason inline and an "Accept → mark stale" action.

No status write happens automatically — a reviewer must accept or
dismiss.

---

## Why Advisory, Not Automatic

Auto-mutating `complete → stale` on every code touch would drown
users in false positives: every small refactor (formatting, renaming
a local variable, adding a comment) would mark dozens of specs stale.
The event-driven hooks already catch meaningful changes; this scan
catches the straggler case and asks the human to judge.

---

## Scope

### In scope
- Detection: matching `affects` against `git log`.
- Surfacing: advisory badge in explorer + focused view.
- Manual accept/dismiss actions.

### Out of scope
- Automatic status mutation.
- Git post-commit hooks (would require the user to install them).
- Rewriting history detection (rebases show up as fresh commits
  anyway — the `--since` query catches them).
- Validity of archived specs' `affects` — archived specs are skipped.

---

## UI

Spec explorer tree:

```
specs/
  ✅ sandbox-backends.md          ⚠ stale candidate (2 files)
  ✅ storage-backends.md
  ✔ container-reuse.md           ⚠ upstream drift (sandbox-backends)
```

The "upstream drift" badge comes from the drift pipeline; the
"stale candidate" badge comes from this scan. Same visual weight,
different reasons. Hovering shows which `affects` paths triggered the
flag.

Focused view banner for a flagged spec:

```
⚠ Stale candidate. 3 files in this spec's `affects` changed since
  the spec was last updated:
    - internal/runner/execute.go   (commit abc1234, 2 days ago)
    - internal/runner/container.go (commit def5678, 5 days ago)
  [Review Changes]  [Mark Stale]  [Dismiss]
```

"Mark Stale" writes `status: stale` via a new endpoint, committing the
transition. "Dismiss" bumps `updated: now` so the next scan ignores the
prior commits — essentially "I looked, it's fine."

---

## Acceptance

- On workspace load, every non-archived `complete` spec whose `affects`
  files changed since `updated` (per `git log --since`) is flagged as
  a stale candidate in the spec-tree API response.
- Archived specs are skipped.
- No frontmatter is auto-mutated.
- Manual "Rescan staleness" button triggers the same scan on demand.
- Unit test: write a spec with `affects: [foo.go]` and
  `updated: 2026-01-01`; commit a change to `foo.go` on `2026-02-01`;
  scan flags the spec. Same spec after `updated` bumped to `2026-03-01`
  no longer flagged.

---

## Open Questions

1. **Periodic interval default.** Off, manual, or every-N-hours? Tentative:
   **off, manual + workspace-load only**. Adds a cron-like loop only
   when real demand appears.
2. **Git history rewrites.** `git rebase` moves commits; their
   timestamps may predate the spec's `updated`. Tentative: match on
   commit **author date** via `--since`, which survives rebase.
   Reorderings via `--committer-date` would fire too aggressively.
3. **Bulk "dismiss all" action.** After a big refactor a user may see
   20 flagged specs. Offer a "dismiss all" that bumps `updated` on
   every flagged spec in one commit? Tentative: yes, gated behind a
   confirmation dialog.
4. **Performance at large scale.** `git log --since --name-only --`
   per spec is N queries; at S > 500 it might take seconds. Batch by
   running one `git log` over the workspace and mapping touched files
   back to `affectsToSpecs`. Same complexity as the task-done
   `AffectsImpactFromDiff` flow. Worth the refactor if S grows.
