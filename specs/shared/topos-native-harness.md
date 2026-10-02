---
title: Topos as the Only Harness
status: drafted
depends_on:
  - specs/shared/platform-native.md
  - specs/shared/platform-native/retire-agent-fleets.md
affects:
  - go.mod
  - internal/agentgraph/
  - internal/adversarial/
  - internal/handler/tasks_autoimplement.go
  - internal/runner/
  - internal/agentsession/
  - internal/harness/
  - internal/executor/
  - internal/oauth/
  - internal/envconfig/
  - internal/handler/
  - frontend/src/
  - docs/
effort: xlarge
created: 2026-06-30
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Topos as the Only Harness

Decision 2 of [platform-native](platform-native.md), in the maintainer's
words: "clean use of the Latere platform and the Topos harness. No Claude
Code, no Codex, but just allow model switches."

This spec replaces its 2026-06-30 version, which made Topos the default
harness while every CLI harness stayed selectable, on the runtime module
wallfacer pins today. That version's shipped work is recorded under
"Shipped"; its full text is in the git history of this file.

## Goal

Every agent wallfacer runs, the task's implementation, test verification,
title, commit message, oversight, chat and planning, runs on the rebuilt Topos
harness, embedded in-process for a local run. Its model comes from the Latere
platform's catalog or from a model provider the user signed in with, and the
user switches models. Wallfacer launches no coding CLI as a subprocess, and
the five CLI adapters, the subprocess executor and the per-CLI credential
plumbing are removed.

## Shipped

On the current pin, `latere.ai/x/topos v0.7.0`, the native harness exists as
an opt-in:

| Part | Evidence |
|---|---|
| `harness.Topos`, `InProcess`, `ErrInProcess`; listed and selectable in the composer, the task detail and Settings | `d996c97f`, `92916a79`; `internal/harness/topos.go` |
| `agentgraph.RunAgent`: one agent in-process, with the task worktree as its working directory and file tools confined to it | `bd5740cc`, `73ed429c`, `fb15e241`, `b53c98b9` |
| `runNativeTopos` and `driveToposRun`: live events onto the task timeline, final text and trace stored, the commit pipeline run | `df9df847`, `7e351855`, `291cc45d` |
| The commit message generated in-process for such a task | `7604ab29` |
| The trace in the task detail, with the Topos wordmark | `c7ce346f`; `AgentTrace.vue` |

What it lacks, and what this spec closes: test verification, oversight, usage
and cost, limits, a stop in `waiting`, sub-agent roles on the same harness,
and a credential other than a static key.

## The rebuilt module, as wallfacer would use it

Facts established by reading the module at v0.10.1 on 2026-10-02. "Built"
means code and a passing test exist; "specified" means a spec of the project
describes it and no code does.

| Need | What the module offers | State |
|---|---|---|
| Run one session in-process on a local directory | Assemble by hand: a directory session store, a session with a first user message, a host machine on the directory, a model connection, the built-in tools, a runner that drives the session to the end of a turn and reports the outcome and stop reason. The project's own command assembles the same parts in an internal package an embedder cannot import | Built; no embedding example and no published client package |
| A supported import set for embedders | Stated in the project's client spec; the promised example and the test that holds examples to the set are not built. An embedder also needs packages outside the stated set (prompts, manifest, the shared dialect IR) | Specified |
| The session log | Append-only, on disk under a directory the embedder chooses; survives a process restart; a resumed run closes any call left without a result as of unknown effect | Built |
| Events | One schema for local and hosted sessions: messages, tool use and result, model request with tokens and cost, status with stop reason, errors, thread start and end. Streaming deltas reach the embedder through an observer or a delta subscription and are never written to the log | Built |
| Model connection | A gateway root (its doors discovered), a door, or a provider base directly; the credential is one string per call, sent as a key header or a bearer | Built |
| Model switch | An event on the session; the next turn reconnects through a function the embedder supplies | Built; the embedder writes the connect function |
| Catalog and cost | An embedded catalog overlaid by the gateway's model list; cost is the gateway's figure, else catalog prices | Built |
| Tools and machine | Read, write, edit, bash, grep, glob, web fetch, to-do. File tools are confined to the working directory; commands run in the user's own shell unless a host sandbox driver is supplied | Built; no sandbox driver wired by default |
| Permissions | Plan, confirm (the default) and progressive modes. A call that needs approval ends the turn with a stop reason; the embedder appends the answer and drives again. There is no callback | Built |
| Budget, turn limit, interrupt, cancel | Per session; an interrupt event cancels the running call | Built |
| Checkpoints | Each turn's working tree committed to a private ref of the repository, leaving HEAD, index and branches alone; can be turned off | Built |
| Instructions | Project instruction files and skills read from the repository on attach; agent instructions set directly | Built |
| Subagents and threads | Spawn and message within one session, parallel, depth-bounded, optionally in a worktree of its own. The subagents a session may spawn are fixed before the turn, and a child sees only its task | Built, drafted spec |
| Fork | A library call that starts a session from another's log at a turn boundary, restoring files from the checkpoint; only the project's server calls it | Built as a library; no local or tool-driven fork |
| A deterministic model for tests | A scripted model driven by a YAML file of steps; a stub gateway at the HTTP level | Built |
| Stability | No statement; most specs drafted; roughly a release a day | Unknown |

Three consequences for this design:

1. **Wallfacer writes the assembly itself**, in its seam package, and the
   connect function that makes the model switch work. Nothing to import
   does it.
2. **Approvals end the turn.** A task that asks before writing stops in
   `waiting` with a pending call, and an answer continues it. That is what
   wallfacer's state machine already expects of a waiting task.
3. **The pin will move often.** The seam must stay the only importer, and a
   pin bump must be a reviewed change with the seam's tests, not a drift.

## Design

### One seam

`internal/agentgraph` keeps its rule: it is the only package that imports the
module, and its boundary test enforces that. It is renamed to
`internal/toposrun`, since the agent graph is gone, and it exposes a
module-free surface to the runner and the chat runtime:

- start or resume a session for a task or a chat thread, on a working
  directory, with instructions, a model, a credential source, a permission
  posture, a budget and a turn limit;
- send a message, an approval answer, an interrupt, a model switch;
- a stream of module-free events, including deltas;
- the outcome of a turn: status, stop reason, final text, usage and cost.

Session logs live under the task's state directory, one session per task for
as long as the task lives, so feedback on a waiting task is the next message
of the same session, and a restart resumes it.

### Models and credentials

The harness gets a model connection from one of two sources, as the umbrella
decided:

| Source | Connection | Credential |
|---|---|---|
| Latere sign-in | The Models capability at `https://api.latere.ai/v1/models`, doors discovered | A model key for the signed-in context, created through the identity service from the login, kept per context and renewed before it expires. The `latere` command line does exactly this today (its `modelkey` package); a second consumer makes it a candidate for extraction into `latere.ai/x/pkg` |
| Provider sign-in | The provider's base directly | What the provider allows a third-party harness to use. For Anthropic that is an API key: a Claude subscription token cannot be used. For OpenAI it is verified in phase 3 against what the module's dialect accepts |

The browser sign-in module (`internal/oauth`, handlers in
`internal/handler/auth.go`) and the keyring
([provider-secret-store](../.archive/local/provider-secret-store.md)) are
kept. What changes is where their credential goes: to the connection above,
not into a CLI's environment. The module signs in under client ids that
belong to other tools; settle that with each provider before shipping.

With neither source the run is refused before it starts, as it already is on
the current pin.

### The model switch

The composer offers the models the active source can reach, with the
platform's catalog figures (context window, prices) when signed in to Latere.
A task records its model. Changing it on a waiting task appends a model
change to the session, and the next turn runs on the new model. Chat offers
the same choice per turn. The per-role model settings collapse to two: the
model a task runs on, and the small model the one-shot roles use.

### Roles on one harness

| Role | Today | On the harness |
|---|---|---|
| Implementation | A CLI subprocess turn loop, or the native single agent | The task's session |
| Test verification | A CLI subprocess in the worktree | A second session on the same worktree with the verification instructions, so its tools and its findings stay apart from the implementation's log |
| Title, commit message | One-shot CLI calls | A single tool-less turn on the small model |
| Oversight | A one-shot CLI call over the activity | A single tool-less turn on the small model, reading the session's events |
| Chat and planning | A CLI subprocess per turn through `internal/agentsession` | A session per thread, on the workspace, resumed across turns |
| Adversarial review | `internal/adversarial`, a debate engine from the pinned module, with a Claude-only proposer and critics rotated across CLI harnesses for model diversity | Two models on the one harness. A reviewer session on a model different from the task's reads the task's diff and reports findings; the task's own session answers them; rounds and cost are bounded by the existing review settings. The diversity the engine bought by switching harnesses comes from switching models, which the neutral harness makes a configuration value. No separate review engine: `internal/adversarial` and its dependency on the pinned module's `adversarial` package are removed |

Every role's tokens and cost come from the session's model request events,
which closes the native run's usage gap.

### Permission posture

The CLI harnesses run today with their permission prompts disabled
(`--dangerously-skip-permissions` for Claude Code, `--full-auto` with a
workspace-write sandbox for Codex). Parity on day one: a task's session
allows file writes and commands in its worktree without asking. An
autonomy setting that asks before commands maps onto the confirm mode and
stops the task in `waiting`. Wiring the host sandbox driver, so commands are
confined to the worktree as Codex's sandbox did, is a follow-up with its own
spec once the driver is supported on macOS and Linux.

### Events

One mapping, from the session schema to the task timeline and usage, serves
the local run and the [hosted executor](../cloud/latere-integration/topos-remote-executor.md),
because both produce the same events. Deltas drive the live text in the task
sheet and in chat. The trace view renders a session's threads.

### Checkpoints

Off in the first phases: wallfacer's own pipeline commits a task's work, and
turn checkpoints would add private refs to the user's repository that nothing
in wallfacer reads yet. They come on with forking, which restores files from
them; see [sessions-that-spawn-and-fork](platform-native/sessions-that-spawn-and-fork.md).

## Phases

Each phase leaves the tree building and the suites green. Phase 0 is in
progress elsewhere.

| Phase | Content | Why first |
|---|---|---|
| 0 | [Retire fleets](platform-native/retire-agent-fleets.md) and [remove GitHub](../.archive/shared/platform-native/remove-github-integration.md) | The delegating-fleet engine is written against the old module's API and cannot move; removing it first means the seam carries one path |
| 1 | Move the seam to the rebuilt module: the task's implementation session, Latere and provider credentials, events and usage, approvals, interrupt and resume, the scripted model in tests. The `topos` harness choice now means the rebuilt harness. The review is rebuilt as two models on the seam in the same phase, because `internal/adversarial` imports the old module and one module path can be pinned at one version only | The base everything else stands on |
| 2 | Every other role on the seam: test verification, title, commit message, oversight, chat and planning. The model switch in the composer and chat. Provider credential shapes verified | After this nothing needs a CLI |
| 3 | Remove the CLI harnesses: the five adapters and the fake one in `internal/harness`, the subprocess executor and its per-CLI launchers, CLI binary discovery, per-role harness settings, the harness picker and logos, the CLI checks in `wallfacer doctor`, and the docs. `Task.Sandbox` stays as a record field and is ignored | Last, so no release has fewer working paths than the one before |

Phase 3 ships with a release note that says plainly what changed for a user
who ran wallfacer on a Claude or Codex subscription: the Claude path is now a
Latere sign-in or an Anthropic API key.

## Acceptance

1. `go.mod` pins a rebuilt release of the module, the seam is its only
   importer, and the boundary test holds.
2. A task signed in to Latere runs on a catalog model, commits in its
   worktree, and shows usage and cost from the session.
3. A task signed in with a provider runs on that provider's model, with the
   Claude case using an API key.
4. Switching the model of a waiting task makes its next turn run on the new
   model; chat switches per turn.
5. With no credential, a task and a chat turn are refused before anything
   runs, and the interface offers both sign-ins.
6. Restarting wallfacer during a task resumes its session without applying
   an event twice.
7. Interrupting a running task stops it at the next step; a task waiting on
   an approval continues when it is answered.
8. Title, commit message, oversight, test verification, chat and planning all
   run without starting a subprocess; a test asserts the executor is not
   called.
9. A review runs its reviewer on a model different from the task's, and
   `internal/adversarial` no longer exists.
10. After phase 3, `internal/harness` and `internal/executor` hold no CLI
   adapter, and the frontend has no harness picker.
11. The Go suite, the frontend suite and `make ui-test` pass, with the
    scripted model in every test that runs an agent.

## Asks of the Topos project

Wallfacer can do all of the above without them; each removes code wallfacer
would otherwise carry.

- A published embedding example and a test that holds it to the supported
  import set, with the set covering what an embedder actually needs.
- A connect helper for the model switch, so each embedder does not write one.
- A stability statement for the embedding surface.
- For [sessions-that-spawn-and-fork](platform-native/sessions-that-spawn-and-fork.md):
  subagents an agent defines at run time, a spawn that inherits the parent's
  context, a fork an agent can request, and fork in a local runner.

## Open questions

1. **The OpenAI credential shape.** Whether a ChatGPT sign-in token reaches a
   model through the module's OpenAI dialect, or only through the provider's
   own client's endpoint. Decides whether the OpenAI provider path is a
   sign-in or an API key.
2. **Client ids.** Whether wallfacer needs OAuth client registrations of its
   own with each provider.
3. **The small model and the reviewer model.** Which catalog models the
   one-shot roles and the reviewer default to, and what they use on a
   provider sign-in, where only one provider's models may be available. A
   reviewer on the same provider's other model keeps some diversity; the
   reviewer on the task's own model is refused.
4. **Pin policy.** How often the pin moves and what a bump must pass.

## Out of scope

- Hosted execution: [topos-remote-executor](../cloud/latere-integration/topos-remote-executor.md).
- Spawn and fork as a product surface: [sessions-that-spawn-and-fork](platform-native/sessions-that-spawn-and-fork.md).
- A host sandbox driver.
