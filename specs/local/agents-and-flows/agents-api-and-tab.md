---
title: /api/agents endpoints and sidebar Agents tab (read-only)
status: complete
depends_on:
  - specs/local/agents-and-flows/extract-agents-package.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/agents.go
  - internal/cli/server.go
  - ui/partials/agents-tab.html
  - ui/partials/sidebar.html
  - ui/js/agents.js
  - ui/css/agents.css
  - ui/partials/scripts.html
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# /api/agents endpoints and sidebar Agents tab (read-only)

## Goal

Ship the user-visible payoff of the internal/agents extraction: a new
sidebar tab that lists every built-in agent (Title, Oversight, Commit
message, Refinement, Brainstorm, Implementation, Testing) with its
prompt template snippet, default sandbox/CLI, mount mode, and timeout.
Read-only for v1 — editing + user-created agents land in a follow-up
task. This is the Agents tab the user explicitly asked for as the
visible next step.

## What to do

1. `internal/apicontract/routes.go`: register
   - `GET /api/agents` — list all registered agents
   - `GET /api/agents/{slug}` — fetch one agent's full body
   (POST / PUT / DELETE land in the editable-agents follow-up.)
   Regenerate `ui/js/generated/routes.js` via `make api-contract`.

2. `internal/handler/agents.go`:
   ```go
   type AgentResponse struct {
       Slug        string   `json:"slug"`
       Name        string   `json:"name"`
       Description string   `json:"description,omitempty"`
       Activity    string   `json:"activity"`
       MountMode   string   `json:"mount_mode"`   // "none" | "read-only" | "read-write"
       SingleTurn  bool     `json:"single_turn"`
       TimeoutSec  int      `json:"timeout_sec,omitempty"`
       Builtin     bool     `json:"builtin"`
       PromptTmpl  string   `json:"prompt_tmpl,omitempty"` // only on GetAgent
   }

   func (h *Handler) ListAgents(w, r) // returns BuiltinAgents as []AgentResponse
   func (h *Handler) GetAgent(w, r)   // 404 if slug unknown
   ```
   `Description` / `TimeoutSec` are populated from the descriptor; the
   prompt body is only returned on the single-agent endpoint to keep
   the list lightweight. Wire the handlers in `internal/cli/server.go`.

3. `ui/partials/sidebar.html`: add a new nav entry **"Agents"**
   between the existing **"Board"** and **"Specs"** entries. Icon: a
   simple agent/person silhouette. Activating the tab swaps the main
   area to the agents view (mode routing follows the existing
   spec-mode / board-mode pattern in `ui/js/state.js`).

4. `ui/partials/agents-tab.html`: one flat panel listing each agent
   as a compact row:

   ```
   ┌────────────────────────────────────────────────┐
   │ [icon]  Implementation                         │
   │         claude · read-write · multi-turn       │
   │         Executes the task prompt and produces  │
   │         commits.                               │
   └────────────────────────────────────────────────┘
   ```

   Clicking a row opens an inline expanded panel with the full prompt
   template in a monospace viewer, the activity label, the timeout,
   and the mount mode explained in human terms ("Mounts workspace
   read-only" / "Mounts worktrees read-write with board context").
   A "Clone" button stub sits in the row — it renders but is disabled
   with tooltip "Editable agents ship next"; the editable-agents task
   wires it.

5. `ui/js/agents.js`:
   - `loadAgents()` — hits `/api/agents`, renders the row list.
   - `expandAgent(slug)` — hits `/api/agents/{slug}`, fills the
     inline panel.
   - Mounts from a small state machine keyed off the sidebar nav
     click.

6. `ui/css/agents.css`: compact row style matching the composer /
   routine-card chrome (same ink / rule / fs-10 tokens).
   `ui/partials/scripts.html`: `<script src="/js/agents.js">`.

7. Documentation: short section in `docs/guide/board-and-tasks.md`
   (or a new `docs/guide/agents.md` — pick whichever matches the
   current guide organisation) covering what the tab surfaces and
   noting that editing + user-authored agents are a follow-up.

## Tests

- `internal/handler/agents_test.go`:
  - `TestListAgents_ReturnsBuiltins` — asserts all seven built-in
    slugs appear, each with non-empty Name and a plausible MountMode
    string.
  - `TestGetAgent_ReturnsPromptBody` — fetches a single agent and
    asserts the prompt template body is non-empty.
  - `TestGetAgent_UnknownReturns404`.
- `ui/js/tests/agents.test.js`:
  - Renderer test: given a stubbed `/api/agents` response, the
    resulting DOM has the right number of rows and each row's
    sandbox chip matches the activity.
  - Expand-on-click wires the /api/agents/{slug} fetch with the
    right path.
  - Clone button is present and disabled (stub).

## Boundaries

- Do NOT add POST/PUT/DELETE endpoints. Editable agents live in a
  separate task.
- Do NOT add a filesystem loader for `~/.wallfacer/agents/`. The
  built-in list comes from the embedded registry that
  extract-agents-package shipped.
- Do NOT introduce a Flow concept or Flow picker here. This task
  only adds visibility into the agent catalog.
- Do NOT rename any existing sidebar nav item or reshuffle the
  board layout.
