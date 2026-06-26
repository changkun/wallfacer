---
name: wf-spec-drive
description: Drive a spec through the spec-coordination lifecycle toward a target state, advancing the legal next transition(s) and reporting the new state each turn. Designed to run under a `/goal` so it continues autonomously across turns until the spec reaches the target (or a gate needs you). Pauses for confirmation at irreversible / outward-facing gates (dispatch to the board, archive, stale fan-out). Use when the user says "drive this spec to done", "take this spec through the lifecycle", or sets a goal to complete a spec.
argument-hint: <spec-file> [target-status=complete]
user-invocable: true
---

# Drive a Spec Through the Lifecycle

Advance the spec at the first argument toward a target state (default `complete`),
one or more **legal** transitions at a time, reporting the new state at the end of
every turn. This is the orchestrator: it reads reality, picks the next step, runs
the right `/wf-spec-*` sub-skill (or transition API call), and stops at gates.

It is built to be **re-invoked**: under a `/goal` the Stop-hook evaluator re-runs
you each turn with the remaining work, so each turn must (a) make concrete progress
and (b) end by stating the spec's current `status` in your message, so the
evaluator can judge whether the goal is met. Do not try to finish everything in one
turn — finish what's legal now, report status, and let the loop continue.

## The canonical lifecycle (authoritative: `internal/spec/lifecycle.go`)

Seven states. **`status` is the single source of truth; transitions must follow
the legal edges below — there is no `implemented`/`in_progress` state, and
`validated → complete` is ILLEGAL.** A spec reaches `complete` only through
`testing`, where a drift verdict is rendered.

```
vague      → drafted | archived
drafted    → validated | stale | archived
validated  → testing | stale
testing    → complete | stale | archived
complete   → stale | archived
stale      → drafted | validated | archived
archived   → drafted            (resurrection)
```

Server-automatic vs. agent-initiated:

| Edge | Who drives it |
| --- | --- |
| drafted → validated | agent (`validate` action) or folder-dispatch auto-promote |
| validated → testing | **server** on task-done (drift pipeline); or wrap-up for the direct path |
| testing → complete / stale | **server** drift verdict; or `force-complete`; or wrap-up's verdict |
| any → stale (fan-out) | **server** on task-done / chat-edit; agent may `stale` manually |
| any → archived, archived → drafted | agent (`archive` / `unarchive`) |

## How status changes (hybrid)

Prefer the **server transition API** (authoritative — it validates the edge,
runs drift / stale fan-out, and commits): `POST /api/specs/transition` with
`{ "action": "<action>", "path": "<workspace-relative spec path>" }`. Actions:
`dispatch`, `undispatch`, `validate`, `stale`, `dismiss-stale`, `force-complete`,
`archive`, `unarchive`. `wf-spec-dispatch` already uses this.

If the server is **not reachable**, fall back to editing the spec's `status`
frontmatter directly — but only along a **legal edge** above, and commit it like
the other skills do. Never write an illegal jump (e.g. `validated → complete`).

Note there is no API action to enter `testing` or to `complete` from `validated`
directly; the server enters `testing` on task-done, and `force-complete` only does
`testing → complete`. The direct-implement path (below) therefore walks
`validated → testing → complete` itself.

## Step 1: Read reality

1. Read the spec's frontmatter: `status`, `dispatched_task_id`, `depends_on`,
   `affects`, and whether it has a child-spec directory (non-leaf).
2. If `dispatched_task_id` is set, check the linked task's status (done /
   in_progress / failed) — `GET /api/tasks/{id}` or the board.
3. Establish the **target** (arg 2, default `complete`) and confirm the spec is
   not already there or past it.

## Step 2: Pick the next step (decision table)

Match the current `status` and choose the next action toward the target:

| Current | Condition | Next step |
| --- | --- | --- |
| `vague` | — | `/wf-spec-refine` (or `/wf-spec-create` follow-up) to make it concrete → `drafted`. Then re-loop. |
| `drafted` | large / many open questions | `/wf-spec-breakdown` (design) → child specs; then drive the lead child. **Gate?** No. |
| `drafted` | small, items clear | `/wf-spec-validate` (lint), then `validate` action → `validated`. |
| `validated` | leaf, build in one pass | `/wf-spec-implement` (direct, **autonomous mode** — no plan-mode pause under a goal), which finishes via `/wf-spec-wrapup` (testing→complete). |
| `validated` | wants board execution | **GATE**: dispatching to the board is outward — confirm with the user, then `/wf-spec-dispatch`. |
| `validated` | non-leaf with task children | dispatch/implement each leaf (drive children); parent completes when leaves do. |
| `testing` | task done, server idle | `/wf-spec-wrapup` renders the verdict → `complete` or `stale`. |
| `testing` | tester failed (`testing_pending`) | report; offer `force-complete` (a gate — confirm). |
| `complete` | — | At target. If the user wanted downstream specs driven, pick the next unblocked dependent. |
| `stale` | — | `/wf-spec-refine` → `drafted`/`validated`, then re-loop. |
| `archived` | — | Stop unless the user asked to resurrect (`unarchive` → `drafted`). |

Dependencies: before implementing/dispatching, confirm every `depends_on` is
`complete` (per the sub-skills' own gates). If a dependency is not complete and the
target requires it, drive the dependency first or report the block.

## Step 3: Gate check before acting

**Pause and ask the user** (do not execute) when the next step is:
- **Dispatch to the board** (creates outward work / a running task).
- **Archive** (retires the spec + descendants; relocates files).
- **Stale fan-out across a dependency tree** (marks other people's specs stale) —
  manual `stale` on a spec with dependents.
- **`force-complete`** (overrides the drift gate).

For all other steps (refine, validate, breakdown, implement-direct, wrap-up of a
spec you implemented this run), proceed without pausing.

## Step 4: Execute one stage, then report

Run the chosen sub-skill / API call. Do as much **non-gated, legal** progress as
fits this turn (e.g. validate → implement → wrap-up can be one turn for a small
leaf), but stop at the first gate, the target, or a genuinely long step.

End every turn with a status line the goal evaluator can read, e.g.:

```
Spec <path>: status <old> → <new>. Target: <target>. Next: <next step | GATE: <what> | DONE>.
```

## Running under a `/goal` (what is and isn't truly hands-off)

A user sets, e.g., `/goal spec <path> reaches status complete`. Each turn the
Stop-hook evaluator reads your transcript; if the spec is not yet at the target it
re-invokes you with what remains. You don't manage the loop — you make and report
progress each turn; the harness caps consecutive auto-continues (≈8) and the goal
auto-clears when met.

Be honest about the reach of an **unattended** loop (no human watching):

- **Fully autonomous** — the non-gated, in-session transitions: `refine`,
  `validate`, `breakdown`, `implement` (autonomous mode, no plan-mode pause), and
  `wrap-up` through the `testing` gate. A small validated leaf can therefore go all
  the way to `complete` unattended.
- **Stalls and needs a human** — every gate in Step 3 (dispatch to the board,
  archive, `force-complete`, stale fan-out). The evaluator can't approve them and
  can't run commands, so an unattended loop cannot pass them; it will burn blocks
  and the harness force-stops (~8). When you reach a gate, **state it plainly and
  stop asking-into-the-void**: report "GATE: <what> — needs you", so when the user
  returns they can unblock with one message.
- **Completes outside the loop** — a *dispatched* spec finishes asynchronously on
  the task board; the drift pipeline (server) renders its verdict on task-done.
  The goal loop can't wait that out (≈8 blocks) and the evaluator can't observe the
  task finishing. So a goal targeting a spec you dispatch will pause at the dispatch
  gate; after the board task is done, re-run `/wf-spec-drive` (or `/wf-spec-wrapup`)
  to pick up `testing → complete`.

Net: `/goal` + `/wf-spec-drive` runs the in-session path to `complete` hands-off,
and turns every outward/async step into a clearly-reported stop rather than a
silent hang. Don't claim more autonomy than that.

## Guidelines

- **One source of truth** — never invent a status; only ever move along the legal
  edges above. When unsure a transition is legal, prefer the server API (it
  rejects illegal edges) over a YAML edit.
- **Don't double-manage dispatched specs** — once a spec is dispatched, the server
  drives `validated → testing → complete/stale` on task-done. Don't hand-set its
  status; let the server, then run `/wf-spec-wrapup` only to enrich the Outcome.
- **Report, don't narrate** — the closing status line is the contract with the
  goal evaluator; keep it accurate.
- **Respect the gates** — autonomy stops at outward/irreversible actions.
