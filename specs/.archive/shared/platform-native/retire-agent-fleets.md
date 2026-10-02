---
title: Retire User-Authored Agents and Fleets
status: archived
depends_on:
  - specs/shared/platform-native.md
affects:
  - internal/flow/
  - internal/agents/
  - internal/agentgraph/
  - internal/runner/
  - internal/handler/
  - internal/apicontract/
  - internal/store/
  - frontend/src/
  - frontend/scripts/ui-shots/
  - docs/
effort: large
created: 2026-10-02
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Retire User-Authored Agents and Fleets

Decision 1 of [platform-native](../../../shared/platform-native.md). A removal, not a
rewrite. It follows the shape of the earlier
[idea-agent removal](../../local/remove-idea-agent-subsystem.md).

## Goal

Every task runs the one built-in pipeline. Nobody defines an agent, clones
one, or composes agents into a fleet, and no code path exists for a fleet to
run on. The agent-graph page, the two engines that only fleets reach, and the
two CRUD APIs behind the page are deleted.

## Why

- **The premise moved.** Defining an agent is the platform's job now, and
  arranging agents in advance is the opposite of where the product is going:
  an agent that spawns and forks what it needs at run time
  ([sessions-that-spawn-and-fork](../../../shared/platform-native/sessions-that-spawn-and-fork.md)).
- **The implementation does not hold.** Four execution paths sit behind one
  page with different guarantees. A fixed-sequence user fleet runs in the
  workspace folder and never commits. A delegating fleet merges with no test
  step, no oversight and no stop in `waiting`, and records no usage. The page
  and the guide describe both as the production path. The only path with the
  full set of guarantees is the built-in pipeline's turn loop.
- **It blocks the harness migration.** The delegating engine is written
  against a runtime API that the rebuilt module no longer has. Removing it
  first means the migration carries one in-process path, not two.

## Decisions

1. **The built-in pipeline stays, by its mechanism and not as a "flow".** The
   turn loop in `internal/runner/execute.go` runs implementation, then test,
   then the commit message, title and oversight roles. It does not go through
   the flow engine today and keeps working with the engine gone.
2. **The five built-in roles stay as internal plumbing.** `internal/agents`
   keeps the `Role` table (`impl`, `test`, `title`, `oversight`,
   `commit-msg`) and loses everything about user-authored roles.
3. **The native single-agent path stays.** `agentgraph.RunAgent`,
   `ModelConfig`, `Event`, `Result`, the import-boundary test,
   `runNativeTopos` and `driveToposRun` are the base the harness migration
   starts from. Only the fleet entry points leave `internal/agentgraph`.
4. **The run trace stays.** `AgentTrace.vue` and `GET /api/tasks/{id}/trace`
   show what a native run did. They are output, not authoring.
5. **Mission Control stays.** `internal/graph` and `GET /api/graph` build the
   spec and task graph. They share a word with this feature and nothing else.
6. **No data deletion.** Files a user wrote stay on disk, unread. Records keep
   their fields. See "User data".
7. **No replacement surface in this spec.** A task's instructions come from
   the repository's instruction files and the prompt. Choosing a harness per
   role stays available through the existing environment variables until the
   harness migration removes the choice.

## Surface removed

**HTTP**

- `GET/POST /api/flows`, `GET/PUT/DELETE /api/flows/{slug}`
- `GET/POST /api/agents`, `GET/PUT/DELETE /api/agents/{slug}`
- The `flow` field on task create and batch create, and `spawn_flow` on
  routine create and update.

**Backend**

- `internal/flow`, whole package: the engine, the registry and its legacy
  kind resolver, the user store, the built-in catalog, the launcher interface.
- `internal/agents`: the user store and its file watcher; `Role.PromptTmpl`
  and `Role.Harness`, which only user clones set.
- `internal/agentgraph`: `FromFlow`, `RunFlow`, `RunFlowWithModel`,
  `FromFlowGraph` and `graph.go`.
- `internal/runner`: the flow-engine branch and the agentic branch of `Run`,
  `runAgenticFlow`, `flowBySlug`, `flow_launcher.go`, the `flows` and
  `flowEngine` fields and their wiring, the user flows and agents directory
  resolution and watchers.
- `internal/handler/flows.go`, `internal/handler/agents.go`, their route
  registrations and contract entries.
- `internal/pkg/yamldir` and `internal/pkg/yamlwatch`, if nothing else
  imports them once the two stores are gone.
- The public-path entries `/agent-graph`, `/agents`, `/workflows`, `/flows`
  in the server-key gate.

**Frontend**

- `AgentGraphPage.vue`, `AgentGraphCanvas.vue`, `AgentEditor.vue`,
  `lib/flowDraft.ts`, and their tests.
- The `/agent-graph` route and the three redirects to it; the **Agents**
  entry in the rail.
- The flow picker in `TaskComposer.vue`, the spawn-flow picker in
  `RoutinesPage.vue`, the flow display on `TaskCard.vue`.
- The flow and agent types in `api/types.ts`, and the strings in both
  language files.

**Verification and docs**

- The agent-fleets scene in `frontend/scripts/ui-shots/checks.mjs`, the
  `agents` and `flows` scenes in `snap.mjs`, any fleet seeding in `seed.mjs`,
  and the images they produce.
- `docs/guide/agent-graph.md`, its entry in `docs/guide/usage.md` (the docs
  navigation is generated from that file), the custom-agent paragraphs in
  `docs/guide/concepts.md`, the fleet sections of
  `docs/internals/agent-graph-runtime.md`, the "composable sub-agent roles"
  paragraph in the root `README.md`, and the regenerated
  `docs/internals/api-contract.json`.

## What stays, stated as a boundary

| Stays | Why |
|---|---|
| The turn loop and the five built-in roles | It is the product |
| `agentgraph.RunAgent`, `ModelConfig`, `Event`, `Result`, the boundary test | The native harness path |
| `runNativeTopos`, `driveToposRun`, `agenticEvent` | Same |
| `AgentTrace.vue`, the trace endpoint, `Task.Trace` | Run output |
| `internal/graph`, `GET /api/graph`, the Map page | Mission Control |
| `HarnessSelect.vue` and the per-task harness pin | Until the harness migration |
| `WALLFACER_SANDBOX_*` per-role variables | Same |

## User data

| What | After |
|---|---|
| `~/.wallfacer/flows/` and `~/.wallfacer/agents/` | Left on disk, never read. On startup, if either holds a file, one warning names the directory and says its contents are no longer used |
| `Task.FlowID` | Kept as a record field. The runner ignores it. A task whose value is set and is not `implement` gets one system event on its timeline when it runs: the fleet it named no longer exists and the built-in pipeline ran |
| `RoutineSpawnFlow`, `RoutineSpawnKind` | Kept as record fields, ignored. A routine spawns an ordinary task |
| A task that is `in_progress` on a fleet when the upgraded binary starts | Recovered the way any interrupted task is; it resumes on the built-in pipeline |
| `Task.Trace` on finished fleet runs | Still rendered |

New writers set none of the three fields.

## Removal order

Each step is one commit that builds and passes the Go and frontend suites.

1. **Frontend.** Remove the page, the canvas, the editor, `flowDraft`, the
   route and redirects, the rail entry, the composer and routine pickers, the
   card display, the types and strings. The frontend compiles without the
   backend changing.
2. **Verification scenes.** Remove the fleet scenes and seeding from the
   screenshot scripts; `make ui-test` passes.
3. **HTTP.** Remove the handlers, routes and contract entries; drop `flow`
   and `spawn_flow` from the request types; regenerate the contract file.
4. **Runner and engines.** Remove the two branches from `Run`, the fleet
   functions in `internal/runner` and `internal/agentgraph`, and the
   directory watchers. Add the startup warning and the timeline event.
5. **Packages.** Delete `internal/flow`; trim `internal/agents`; delete the
   two helper packages if unused.
6. **Docs and index.** Guide, internals, README, changelog (`### Removed`),
   and the spec bookkeeping below.

## Acceptance

1. `go build ./...` has no reference to `internal/flow`.
2. A task created through the API or the composer runs the built-in pipeline
   and commits in its worktree, as before.
3. A stored task whose `flow_id` names a removed fleet runs the built-in
   pipeline and carries the timeline event.
4. A stored routine with a spawn flow fires and spawns an ordinary task.
5. A task pinned to the native harness still runs, commits, and shows its
   trace.
6. `GET /api/flows` and `GET /api/agents` answer 404.
7. The rail has no Agents entry; `/agent-graph` and its former redirects
   render the not-found page.
8. With a file present in `~/.wallfacer/flows/`, startup logs the warning
   once and reads nothing from it.
9. The Go suite, the frontend suite, `bunx vue-tsc --noEmit` and
   `make ui-test` pass.

## Accepted loss

- A custom system prompt per role, and a harness pin per custom role.
- Fixed-sequence fleets other than the built-in pipeline.
- Delegating fleets: model-driven hand-off between authored agents, the
  orchestrator-worker and mesh topologies, the hand-off depth bound.

Nothing replaces the first two. The third is replaced in kind, later, by an
agent that spawns and forks at run time.

## Bookkeeping

- [agent-graph-e2e-design](../../local/agent-graph-e2e-design.md):
  withdrawn and archived with this spec. Its two findings (a fixed-sequence
  fleet that never commits; copy that misstates what delegating fleets do)
  are closed by the removal.
- [first-run-onboarding](../../../local/first-run-onboarding.md): its second
  blocking decision resolves the other way. Agents and fleets are not part of
  a first run.
- `specs/shared/console-redesign/agent-graph.md`: its subject disappears.
  Archive it with a banner when step 1 lands; the umbrella then counts ten
  shipped children and one retired.
- Archived Outcomes of `unified-agent-graph-ui`, `topos-runtime-integration`
  and `agents-and-flows` each get one line pointing here.
- [hosted executor](../../../cloud/latere-integration/topos-remote-executor.md):
  its sentence that delegating flows stay local becomes moot.

## Out of scope

- The harness migration and the removal of the CLI adapters.
- Any new authoring surface.
- What a task's agent may spawn at run time.

## Outcome

Archived 2026-10-02 as complete. Shipped in seven commits on main
(`6587657c` frontend, `e29c532d` screenshot scenes, `a4663953` HTTP,
`45a38047` runner, `4804c399` packages, `0ee84715` docs, `bfae478b` a comment
follow-up).

Where it differed from the text above:

- **A local not-found page.** The console had no catch-all route, so a
  removed path rendered nothing. `LocalNotFoundPage.vue` now renders inside the
  shell, and the last-route restore skips a stored path that no longer
  resolves, so a user last on `/agent-graph` lands on the board.
- **Hard loads of the old paths** on a keyed instance without an identity get
  401 like any other unknown path, since they left the public UI list; only
  in-app navigation reaches the not-found page.
- **`spawn_kind` went too**, with its allow-list and the two routine response
  fields; both request fields now answer 400 as unknown.
- **`agents.Role.Harness` stays.** The chat-title fix pins the title role's
  harness through it, so it is now a per-call harness pin for a built-in role.
  `agents.Registry` and its constructors went, since only fleet code used them.
  A test holds every built-in role to a runner binding.
- **The overrides `WALLFACER_FLOWS_DIR` and `WALLFACER_AGENTS_DIR` went** with
  the directory resolution. The startup warning checks the default locations,
  so a user who had set an override gets no warning.
- **The removed-fleet notice is once per task**, deduplicated on its kind, so a
  retry or resume does not repeat it.
- **Order:** the routine change landed with the HTTP step, because routines
  read the flow registry; the guide images went with the guide.
- **More docs than listed:** the routines, board, configuration and automation
  guides, and seven internals pages.
- `make ui-test` was not run by the implementing agent; the frontend CI job
  runs it on the push.
