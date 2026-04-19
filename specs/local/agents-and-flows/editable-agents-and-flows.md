---
title: User-editable agents and flows (follow-up)
status: validated
depends_on:
  - specs/local/agents-and-flows/runner-flow-integration.md
affects:
  - internal/agents/store.go
  - internal/flow/store.go
  - internal/apicontract/routes.go
  - internal/handler/agents.go
  - internal/handler/flows.go
  - internal/runner/agent_bindings.go
  - ui/partials/agents-tab.html
  - ui/partials/flows-tab.html
  - ui/partials/board.html
  - ui/js/agents.js
  - ui/js/flows.js
  - ui/js/tasks.js
effort: large
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# User-editable agents and flows (follow-up)

## Goal

Turn the read-only Agents and Flows tabs into read-write surfaces.
Users can clone a built-in, edit the copy, and save it under
`~/.wallfacer/agents/*.yaml` or `~/.wallfacer/flows/*.yaml`. Built-in
definitions remain read-only — cloning is the only path to a custom
definition. Engine reads the merged registry (built-in + user) at
request time so edits take effect without a restart.

## What to do

1. On-disk store (per package):
   - `internal/agents/store.go`: YAML loader/watcher reading from
     `WALLFACER_AGENTS_DIR` (default `~/.wallfacer/agents/`).
   - `internal/flow/store.go`: same shape for flows. Both use the
     existing `fsnotify` watcher pattern (`internal/pkg/watcher` or
     a new small helper if the shape differs) to invalidate the
     in-memory registry cache.

2. API:
   - `POST /api/agents` `{slug, name, description, prompt_tmpl,
     activity, mount_mode, single_turn, timeout_sec, model?}` →
     creates a user-authored agent. Slug uniqueness enforced
     across built-in + user.
   - `PUT /api/agents/{slug}` — only for user-authored; 409 for
     built-in.
   - `DELETE /api/agents/{slug}` — only for user-authored.
   - Mirror triple for flows.

3. UI:
   - Agents tab: "Clone" button on built-in rows now enabled —
     opens an inline editor with the cloned descriptor. Save
     writes through the POST endpoint; edit-and-save on a
     user-authored agent writes through PUT.
   - Flows tab: "Clone" button on flow cards. Step chain editor
     is a reorderable list with per-step agent dropdown
     (autocomplete against `/api/agents`). Optional + InputFrom +
     RunInParallelWith are single-row controls.

4. Validation:
   - Agent slug: kebab-case, 2–40 chars.
   - Flow step references: every `AgentSlug` must resolve in the
     merged registry. 422 on dangling reference.
   - No self-reference in `RunInParallelWith`; parallel siblings
     must all be within the same flow.

5. Agent CLI pin + composer retirement of the Agent selector:
   - Add an optional `CLI string` field to the agent descriptor
     (values: `"claude"`, `"codex"`, empty for "inherit"). The
     runner's sandbox resolver gains a new top tier that reads
     this pin ahead of the existing activity / task / env /
     default tiers.
   - The editor in the Agents tab exposes the CLI as a dropdown so
     users can bind an agent to a specific CLI when cloning
     (`impl-codex`, `impl-claude`, etc.).
   - Retire the composer's "Agent" dropdown (`#new-sandbox` in
     `ui/partials/board.html`) now that CLI choice lives on the
     agent definition each flow step references. Preserve the
     workspace-wide default via `WALLFACER_DEFAULT_SANDBOX` — the
     composer no longer needs a per-task override because users
     who want a different CLI clone the agent (or the flow) and
     pick the CLI there.
   - Remove the `sandbox` field from the `POST /api/tasks` body.
     Legacy clients that still send it get a 400 with a pointer
     to the flow editor.

6. Documentation: extend `docs/guide/board-and-tasks.md` (or the
   new Agents / Flows guide) with the clone-to-customize flow,
   the file layout under `~/.wallfacer/`, and the env-var knobs.
   Call out the composer change explicitly so users who relied on
   the per-task Agent override know where it went.

## Tests

- `internal/agents/store_test.go`:
  - Load from a tempdir, watch for changes, invalidate cache.
  - Built-in slug collision rejected.
- `internal/flow/store_test.go` — same shape.
- `internal/handler/agents_test.go` + `flows_test.go`:
  - POST / PUT / DELETE round-trips for user-authored.
  - 409 on mutation of a built-in.
  - Agent response carries the new `cli` field.
- `internal/runner/sandbox_resolver_test.go`: the agent CLI pin
  wins over the task's legacy `Sandbox` field (treated as
  workspace default until retirement).
- `ui/js/tests/tasks-coverage.test.js`:
  - POST body no longer includes `sandbox` after the composer
    retires the Agent selector.
- `ui/js/tests/` updates for the clone / edit flows.

## Boundaries

- Do NOT support a visual DAG editor; step chain stays linear in
  the editor (parallel siblings are a multi-select on each step).
- Do NOT touch the cloud-track storage layer — agents and flows
  live on the host filesystem for the local product.
- Do NOT change the engine or runner behaviour. User-authored
  definitions flow through the same execution path.
- Do NOT migrate existing routines' `spawn_kind` to `spawn_flow`
  in this task. That's the sibling
  `routine-spawn-flow-migration` follow-up.
- Do NOT rewire the sandbox resolver's per-activity env-var tier
  (`WALLFACER_SANDBOX_IMPLEMENTATION` etc.). The agent CLI pin
  layers *above* the activity tier so activity-level env
  overrides continue to work for user installs that rely on
  them.
