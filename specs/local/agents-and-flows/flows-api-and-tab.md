---
title: /api/flows endpoints and sidebar Flows tab (read-only)
status: complete
depends_on:
  - specs/local/agents-and-flows/flow-data-model.md
  - specs/local/agents-and-flows/agents-api-and-tab.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/flows.go
  - internal/cli/server.go
  - ui/partials/flows-tab.html
  - ui/partials/sidebar.html
  - ui/js/flows.js
  - ui/css/flows.css
  - ui/partials/scripts.html
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# /api/flows endpoints and sidebar Flows tab (read-only)

## Goal

Surface the flow registry through a new sidebar tab so users can see
the built-in flows (`implement`, `brainstorm`, `refine-only`,
`test-only`), their step chains, and the agent each step references.
Read-only; editable flows are a separate follow-up.

## What to do

1. `internal/apicontract/routes.go`: register
   - `GET /api/flows` — list all registered flows
   - `GET /api/flows/{slug}` — fetch one flow
   Regenerate `ui/js/generated/routes.js`.

2. `internal/handler/flows.go`:
   ```go
   type StepResponse struct {
       AgentSlug         string   `json:"agent_slug"`
       AgentName         string   `json:"agent_name,omitempty"`
       Optional          bool     `json:"optional,omitempty"`
       InputFrom         string   `json:"input_from,omitempty"`
       RunInParallelWith []string `json:"run_in_parallel_with,omitempty"`
   }

   type FlowResponse struct {
       Slug        string         `json:"slug"`
       Name        string         `json:"name"`
       Description string         `json:"description,omitempty"`
       SpawnKind   string         `json:"spawn_kind,omitempty"`
       Builtin     bool           `json:"builtin"`
       Steps       []StepResponse `json:"steps"`
   }
   ```
   `AgentName` is filled in by resolving each step's `AgentSlug`
   against the agents registry — saves the UI a second round-trip
   for display labels.

3. `ui/partials/sidebar.html`: add a **"Flows"** nav entry right
   below **"Agents"** (shared visual group — both surface the
   runtime's composable pieces).

4. `ui/partials/flows-tab.html`: each flow renders as a vertical
   card with the step chain visualised as a flow of chips:

   ```
   ┌───────────────────────────────────────────────────┐
   │ Implement                                          │
   │ default flow for task execution                    │
   │ refine? → impl → test → commit-msg ‖ title ‖ ovrs │
   └───────────────────────────────────────────────────┘
   ```

   The `‖` glyph indicates parallel-sibling execution.
   Optional steps get a trailing `?`. Hovering a chip shows the
   agent's summary tooltip via the `/api/agents/{slug}` payload.

5. `ui/js/flows.js`:
   - `loadFlows()` → GET /api/flows, render the list.
   - `expandFlow(slug)` → GET /api/flows/{slug}, show the full
     description + step descriptions.
   - Chip click cross-navigates to the Agents tab with the
     corresponding agent expanded.

6. `ui/css/flows.css`: compact card chrome, shared tokens with
   the agents tab CSS.

7. Short docs update in the Agents guide (or a new Flows section
   if you split them).

## Tests

- `internal/handler/flows_test.go`:
  - `TestListFlows_ReturnsBuiltins`.
  - `TestGetFlow_ResolvesAgentNames` — response's `AgentName`
    fields are non-empty for every step.
  - `TestGetFlow_UnknownReturns404`.
- `ui/js/tests/flows.test.js`:
  - Renderer test: given a stubbed /api/flows response with one
    parallel-sibling group, the DOM shows the `‖` separator only
    at the parallel-group boundary.
  - Chip click wires to the correct agent detail.

## Boundaries

- Do NOT add POST/PUT/DELETE endpoints.
- Do NOT load user flows from disk.
- Do NOT wire the composer to consume flows. That's the
  `composer-flow-picker` task.
- Do NOT render the flow step chain as a draggable DAG editor.
  v1 is a static chip list; a visual editor lands in a
  follow-up.
