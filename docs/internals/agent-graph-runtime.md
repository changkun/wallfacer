# Agent Graph Runtime

Wallfacer embeds the topos runtime SDK (`latere.ai/x/topos`) as an in-process execution engine. A topos run executes a `topos.Region` inside the wallfacer server process and returns a final text plus a trace graph of what the run did. Wallfacer uses it for one thing: the native `topos` harness, which runs a task as a single agent (a one-node region) in the task's worktree.

The integration is experimental and opt-in. An ordinary task keeps the multi-turn subprocess turn loop documented in [Task Lifecycle](task-lifecycle.md); only a task explicitly pinned to the `topos` harness reaches this runtime. Current constraints: static API-key credentials only (Bearer/OAuth deferred), a run with no model credential is refused before it starts, no session resume, no MCP, and no test step, oversight, or stop in `waiting` between the in-process run and the merge.

## The Single Import Seam

`internal/agentgraph` is the package through which wallfacer runs topos. Everything the rest of the codebase consumes is a topos-free mirror type defined in the seam:

| Seam type | Mirrors | Purpose |
|---|---|---|
| `agentgraph.Runner` | `topos.Runner` | `Run(ctx, region, task)` executes a region and returns the result |
| `agentgraph.Result` | `topos.RunResult` | `Final` text + `Trace` graph |
| `agentgraph.Trace` / `Node` / `Edge` | `topos.Trace` | Renderable run graph; marshals to the same JSON shape |
| `agentgraph.Event` | `topos.Event` | One live observation (assistant text, tool use); `PayloadJSON` stays opaque |
| `agentgraph.ModelConfig` / `ModelMode` | `topos.ModelOptions` / `ModelKind` | Host-side model selection; mapped in exactly one function, `modelOptions` |

The root package `latere.ai/x/topos` is the supported runtime surface. `TestWallfacerImportsOnlyRootTopos` (`internal/agentgraph/boundary_test.go`) runs `go list` over every package, test imports included, and fails if a wallfacer package imports a topos subpackage (`latere.ai/x/topos/...`) without being the designated seam for it. The only designated seam today is `internal/adversarial`, for the adversarial review engine. This keeps the runtime an implementation detail: the engine can restructure internally without touching wallfacer, and no second seam can grow by accident.

The seam's entrypoint is `RunAgent`: one agent named `name`, with an optional system prompt, run on the model a `ModelConfig` selects, with tools rooted in an optional worktree and live events delivered to an optional observer. It returns `agentgraph.ErrNoModelCredential` for a `ModelConfig` that carries no credential, before a runner is built, so nothing executes in the working directory. `NewRunner` additionally refuses `topos.Options` that select no model, because the runtime would resolve those to its test model. `runOptions` (`model.go`) is the only place that names `topos.Options`.

## Model Resolution

`ModelMode` (`internal/agentgraph/model.go`) selects how a run reaches a model:

- `ModelModeLux` (`"lux"`): a provider reached through Lux, the model gateway. `BaseURL` points at a Lux endpoint and `APIKey` is a Lux virtual key (`lux_*`).
- `ModelModeDirect` (`"direct"`): a provider endpoint reached directly with a BYO key.
- `ModelModeFake` (`"fake"`): the runtime's deterministic, network-free test model. It is reachable only by naming it. The zero value of `ModelMode` selects nothing, and no missing credential resolves to this mode.

The runner derives the config in `Runner.agenticModelConfig` (`internal/runner/agentic.go`) from the same `.env` file the subprocess harnesses read:

1. No env file, unparseable env file, or no `ANTHROPIC_API_KEY`: `agentgraph.ErrNoModelCredential`. `Runner.Run` resolves the config before worktree setup, so the task fails with the `model_credential_missing` category and the fixed sentence `harness.ToposCredentialRequired` as its result, with no worktree created and nothing run. `sandboxUsable` (`internal/handler/sandbox_gate.go`) reports the `topos` harness unusable with the same sentence. A secret-store failure returns `envconfig.ErrSecretStore` and fails the task the same way under its own error text.
2. `ANTHROPIC_API_KEY` set and `ANTHROPIC_BASE_URL` set: `ModelModeLux` through the gateway. The base URL is the Anthropic door the container harness dials; the runner drops only its trailing `/anthropic` segment and keeps any base path, so `https://api.latere.ai/v1/models/anthropic` becomes the gateway root `https://api.latere.ai/v1/models`.
3. `ANTHROPIC_API_KEY` set, no base URL: `ModelModeDirect` against the provider.

`CLAUDE_DEFAULT_MODEL` supplies the model id; the provider is always `anthropic` today. The refusal is centralized in `modelOptions`: a real-mode config missing a credential returns `ErrNoModelCredential`, and a credential under an unset or unknown mode is an error as well, so no input degrades to another model.

The test model stays available to tests through an explicit opt-in. Tests of the seam pass `ModelConfig{Mode: ModelModeFake}`. Tests of the runner set the unexported `Runner.allowTestModel` field, which makes `agenticModelConfig` return that config when no key is configured. No `RunnerConfig` field, env-file key, or environment variable sets the field, so a production runner always refuses.

Only the static `x-api-key` credential is wired. Bearer-style credentials (`ANTHROPIC_AUTH_TOKEN` gateway tokens, `CLAUDE_CODE_OAUTH_TOKEN`) require a per-call `BearerSource` and are deferred; a Claude subscription login therefore does not light up the in-process runtime, only a raw API key or Lux key does.

## Dispatch in the Runner

`Runner.Run` (`internal/runner/execute.go`) runs every task on the built-in pipeline and picks one of two executions for it:

1. **Native topos harness.** A task whose resolved harness is in-process (`harness.InProcess(r.sandboxForTask(task))`, true only for `topos`) runs through `runNativeTopos`: a single agent as a one-node pinned region via `agentgraph.RunAgent`. Test runs (`task.IsTestRun`) are excluded: they stay on the turn loop, where the testing activity resolves to the default subprocess harness. The dispatch happens **after** worktree setup so `topos.Options.Workdir` can point at the task's real worktree (`firstWorktreePath`); an empty worktree falls back to the topos temp-dir sandbox. The periodic oversight worker is skipped for native runs, which produce their own live trace and never enter the turn loop.
2. **Turn loop.** Every other task runs the subprocess turn loop: implementation turns, then the test, title, oversight and commit-message roles.

Because `harness.Default()` is still `Claude` (`internal/harness/registry.go`), the runtime only executes tasks explicitly pinned to the `topos` harness. The native path runs in the task worktree and commits and merges it through `Runner.Commit`. It has no test step or oversight and does not stop in `waiting`: `driveToposRun` walks the task from `in_progress` through `waiting` and `committing` to `done` in one pass, with no review point between the run and the merge. Those are the parity gaps with the turn loop.

`Task.FlowID` is not read for dispatch. A stored task whose `FlowID` names something other than `implement` (a user-authored fleet from an earlier release) runs the same way as any other, and `noteRemovedFleet` (`internal/runner/removed_fleet.go`) writes one `system` event of kind `fleet:removed` on its timeline. The notice is written once per task: a resumed, retried, or test run of the same task finds the earlier event and writes nothing.

### driveToposRun

The native path runs through `driveToposRun` (`internal/runner/agentic.go`), which:

- applies the task timeout (`task.Timeout` minutes, falling back to `constants.DefaultTaskTimeout`);
- forwards live trace events onto the task timeline. The topos observer is called synchronously on the run's goroutines, so it must not block: events are pushed into a 256-slot buffered channel and drained into the store by a separate goroutine, dropping on overflow rather than backpressuring the run;
- on error, respects an already-cancelled task, classifies the failure (`classifyFailure`), attempts `tryAutoRetry`, and otherwise fails the task with an error event;
- on success, persists the final text (`UpdateTaskResult` with stop reason `end_turn`) and the JSON-marshaled trace (`UpdateTaskTrace`) **before** transitioning, so the durable record is complete the moment the task reaches done;
- walks `in_progress -> waiting -> committing`, runs `Runner.Commit` to stage, commit, rebase, merge, and clean up the task worktrees, then transitions to `done`. A commit failure transitions from `committing` to `failed`; the state machine forbids a direct `in_progress -> done` transition.

## Task.Trace and the Trace Endpoint

`store.Task.Trace` (`internal/store/models.go`) holds the JSON-marshaled trace as an opaque `*string`, so the store never depends on topos types. Nil for every task a subprocess harness ran. A native run records one node and no edge; traces stored by earlier multi-agent runs carry handoff edges and are served unchanged.

`GET /api/tasks/{id}/trace` (`internal/handler/tasks_trace.go`, `TaskTrace`) reparses the stored string into the thin frontend shape: `nodes` (id, name, role, status `running|done|failed`, grants, sandbox) and `edges` (from, to, kind `delegate|deliver|next`). The stored JSON uses capitalized keys with no tags; `json.Unmarshal` matches case-insensitively, so it binds directly to the lowercase wire fields. A task with no trace returns 200 with empty arrays, never null, so the client renders nothing without special casing. `AgentTrace.vue` draws the graph in the task detail view.

## Live Traces on the Task Timeline

`agenticEvent` (`internal/runner/agentic.go`) maps an `Event` onto a task-timeline event, returning `ok=false` for anything that should not surface (lifecycle bookkeeping, empty payloads). Exactly three topos event names surface:

| Topos event | `kind` | Rendered line |
|---|---|---|
| `AssistantMessage` | `assistant` | `<agent>: <text>` (dropped when text is empty) |
| `SubagentStart` | `delegate` | `delegated to <agent>` (a single-agent run emits none) |
| `PostToolUse` | `tool` | `<agent> used <tool>` (dropped when the tool name is missing) |

Each surfaces as a `store.EventTypeSystem` event whose data carries `result` (the human-readable line, so the events tab reads naturally), `source: "agentgraph"` (marks it as a trace event), `kind`, `node` (the trace node id, the join key back to graph nodes), `agent`, and `text`. The agent label prefers `AgentID` and falls back to `Node`. This is what makes an in-process run visible as it proceeds, not only as a trace graph at the end.

## The Topos In-Process Harness

`internal/harness/topos.go` registers topos as a full registry citizen so the config UI selector, default resolution, and per-task pinning treat it uniformly with the five CLI harnesses. Its execution is not subprocess-shaped, so every subprocess-facing method is a guard:

- `harness.InProcess(id)` reports whether an id runs in-process; the runner consults it to choose the in-process path over `BuildArgv` + the executor. Today only `Topos` qualifies.
- `BuildArgv` returns `ErrInProcess`; any caller that reaches it has routed an in-process harness down the subprocess path by mistake.
- `ParseEvent` returns `KindUnknown` (events come from the topos observer, not NDJSON stdout), recording rather than crashing on an accidental subprocess-path call.
- `AuthEnv` returns an empty map: credentials resolve in-process through the model config, not via subprocess env injection.
- `Capabilities` reports `SupportsSystemPrompt` and `EmitsUsage` only. No resume, no MCP: a native topos task cannot continue a previous session, and MCP servers configured for CLI harnesses do not apply.

### Sub-Agent Roles of an In-Process Task

A task's sub-agent roles inherit its harness, and the executor cannot launch an in-process one (`HostBackend.Launch` returns "unsupported agent"). `Runner.runAgent` (`internal/runner/agent.go`) therefore routes by what the role needs:

| Role | What runs it for a `topos` task |
|---|---|
| Title, oversight, test oversight, commit message (prompt-only, mount mode none) | `launchInProcess`: a one-agent `agentgraph.RunAgent` with no worktree. The role's binding parses the run's final text like a subprocess result. Title and oversight request the title model (`CLAUDE_TITLE_MODEL`, falling back to `CLAUDE_DEFAULT_MODEL`). |
| Testing (a test run) | The configured default subprocess harness. `sandboxForTaskActivity` (`internal/runner/container.go`) skips any tier that names an in-process harness for the testing activity, because verification is driven by the subprocess turn loop. |
| Implementation | `runNativeTopos`, not `runAgent`: the whole task runs as the native agent described above. |

`launchInProcess` refuses any role that mounts the workspace, with an error naming the harness; no caller requests one, so the refusal is a guard. An in-process role run needs the same model credential as the task itself and fails with `ErrNoModelCredential` without one. It reports no usage to the runner, so these roles add nothing to the task's usage ledger.

A chat thread's title has no task to inherit a harness from. `Runner.GenerateAgentSessionTitle` (`internal/runner/title.go`) takes the env file's title harness (`WALLFACER_SANDBOX_TITLE`), then its default harness (`WALLFACER_DEFAULT_SANDBOX`), then `harness.Default()`, and runs the title role through `runAgent` with that harness as the role's pin. A `topos` default therefore reaches `launchInProcess` like the title of a `topos` task, and a subprocess default takes the executor path with the same spec as before.
