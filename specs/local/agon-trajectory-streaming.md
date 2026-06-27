---
title: Agon Verification Trajectory Streaming
status: complete
depends_on:
  - agon-adversarial-verification
affects:
  - internal/handler/execute.go
  - internal/handler/agon_transcript.go
  - internal/apicontract/routes.go
  - internal/cli/server.go
  - frontend/src/api/types.ts
  - frontend/src/components/TaskDetail.vue
effort: medium
created: 2026-06-27
updated: 2026-06-27
author: changkun
dispatched_task_id: null
---

# Agon Verification Trajectory Streaming

## Problem

Triggering agon verification gives only a toast and coarse timeline events
(`started`, the final verdict). Unlike a regular task — which streams its
agent trajectory turn-by-turn into the UI — an agon run is a black box while it
executes: the user cannot watch the proposer/critic debate unfold across forks
and rounds. They asked for the live fork trajectory in the Verification tab.

## What agon already persists (the source)

agon writes the debate **incrementally** to its session dir during the run
(no agon change needed):

- `<stateDir>/sessions/<id>/` — one dir per run; `<id>` is timestamp-prefixed, so
  the newest dir under `<stateDir>/sessions/` is the current/most-recent run.
- `forks/critic-<i>/rounds/r<n>-<role>.md` — the markdown body of each round
  (role = `critic` on odd rounds, `proposer` on even), written by
  `state.WriteRound` as each round completes.
- `transcript.jsonl` — appended per round (`state.AppendTranscript`); each record
  is `{ts, fork, round, role, path}` pointing at the round file.
- `start.json` / `end.json` / `summary.md` — run-level.

`<stateDir>` is `agonStateDir(primaryWorktree(task))` =
`<worktreesDir>/<taskID>/.agon`, which wallfacer computes deterministically.

## Design

### Backend: a transcript-read endpoint (polled)

`GET /api/tasks/{id}/agon/transcript` (new handler `AgonTranscript`):

1. Resolve the session dir: newest child of `<agonStateDir>/sessions/`. 404 with
   a clear "no agon run" body when none exists.
2. Parse `transcript.jsonl` for the ordered (fork, round, role, path) records;
   read each referenced `r<n>-<role>.md` body.
3. Return a structured response:

   ```json
   {
     "session_id": "...",
     "running": true,              // end.json absent => still in flight
     "forks": [
       { "index": 1, "rounds": [
         { "round": 1, "role": "critic",   "body": "## attack ...", "ts": "..." },
         { "round": 2, "role": "proposer", "body": "rebuttal ...",  "ts": "..." }
       ]}
     ]
   }
   ```

`running` is derived from the presence of `end.json`. The endpoint is cheap and
idempotent, so the frontend polls it (every ~2–3 s) while a run is in flight and
once more on completion. Tailing/SSE is a possible later optimization; polling a
per-round-updated file is "real-time enough" for a multi-minute, per-round
(~30 s–2 min) debate.

Read-only file access stays within the task's `.agon` dir (path-join the fork/
round from parsed records; never honor absolute paths from the jsonl).

### Frontend: live trajectory in the Verification tab

`TaskDetail.vue`, `data-main-tab-section="verification"` (today shows only the
test-agent `testResults`): add an **Agon trajectory** block above/below it.

- Fetch `GET /api/tasks/{id}/agon/transcript` when the verification tab is shown
  and `task.session_id` is present.
- Render forks as columns/sections; within each, rounds in order, each a
  collapsible entry labelled `Critic R1` / `Proposer R2` with the markdown body
  (reusing `renderResultMarkdown`).
- Poll every ~2.5 s while `running` (and stop on completion / tab blur / unmount).
- Empty/!running/no-session → a muted "No agon run yet" line; never error loudly.

`types.ts`: add `AgonTranscript` / `AgonFork` / `AgonRound` interfaces.

## Outcome (2026-06-27)

Both phases implemented. Backend: `AgonTranscript` reads the newest session under
the task's `.agon`, parses `transcript.jsonl`, reads each round markdown (with
`..`/absolute-path guards), returns forks→rounds + a `running` flag from
`end.json` presence. Frontend: the verification tab renders the fork/round debate
(latest round auto-open, pulsing "live" badge) and polls every 2.5s while
running; clicking Agon jumps to the tab and opens a 90s watch window so a
just-started run streams in. No agon change was needed — it persists the
trajectory incrementally during the run.

Deviation: polling (2.5s) rather than SSE tailing — adequate for per-round
(~30s–2min) updates; SSE remains a future optimization (Non-Goals).

## Non-Goals

- Token-by-token streaming of each agent (agon captures round output as a whole;
  per-round granularity is the unit). True SSE tailing is deferred.
- Surfacing proposer/critic raw tool calls (only the round markdown bodies).
- An agon change to push events — the read-the-artifacts approach needs none.

## Phasing / Acceptance Criteria

Phase 1 — backend. `AgonTranscript` handler + route; resolve newest session,
parse `transcript.jsonl`, read round files, return the structured shape.
Tests: a synthetic session dir yields ordered forks/rounds with bodies;
`running` reflects `end.json` presence; missing session → 404.

Phase 2 — frontend. Types, fetch + render in the verification tab, poll while
running. Acceptance: a task with an agon run shows its fork/round trajectory and
updates live as rounds land; `vue-tsc` clean.
