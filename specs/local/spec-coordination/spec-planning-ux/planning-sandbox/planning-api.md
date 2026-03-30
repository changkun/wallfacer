---
title: Planning sandbox API endpoints
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox/planner-core.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/
  - internal/cli/server.go
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Planning sandbox API endpoints

## Goal

Add HTTP API endpoints for starting, stopping, and querying the planning sandbox. These follow the ideation endpoint pattern (`POST /api/ideate`, `DELETE /api/ideate`, `GET /api/ideate`) but adapted for the planning container lifecycle.

## What to do

1. In `internal/apicontract/routes.go`, add three new routes in a `// --- Planning ---` section:
   ```go
   {Method: http.MethodGet, Pattern: "/api/planning", Name: "GetPlanningStatus",
       JSName: "status", Description: "Get planning sandbox status.", Tags: []string{"planning"}},
   {Method: http.MethodPost, Pattern: "/api/planning", Name: "StartPlanning",
       JSName: "start", Description: "Start the planning sandbox.", Tags: []string{"planning"}},
   {Method: http.MethodDelete, Pattern: "/api/planning", Name: "StopPlanning",
       JSName: "stop", Description: "Stop the planning sandbox.", Tags: []string{"planning"}},
   ```

2. Run `make api-contract` to regenerate `ui/js/generated/routes.js`.

3. Create `internal/handler/planning.go` with three handler methods:

   ```go
   // GetPlanningStatus reports whether the planning sandbox is running.
   func (h *Handler) GetPlanningStatus(w http.ResponseWriter, r *http.Request) {
       running := h.planner.IsRunning()
       httpjson.Write(w, http.StatusOK, map[string]any{
           "running": running,
       })
   }

   // StartPlanning starts the planning sandbox container.
   // If already running, returns 200 with running=true (idempotent).
   func (h *Handler) StartPlanning(w http.ResponseWriter, r *http.Request) {
       if h.planner.IsRunning() {
           httpjson.Write(w, http.StatusOK, map[string]any{"running": true})
           return
       }
       if err := h.planner.Start(r.Context()); err != nil {
           http.Error(w, err.Error(), http.StatusInternalServerError)
           return
       }
       httpjson.Write(w, http.StatusAccepted, map[string]any{"running": true})
   }

   // StopPlanning stops the planning sandbox container.
   func (h *Handler) StopPlanning(w http.ResponseWriter, r *http.Request) {
       h.planner.Stop()
       httpjson.Write(w, http.StatusOK, map[string]any{"stopped": true})
   }
   ```

4. In `internal/handler/handler.go`, add a `planner *planner.Planner` field to the `Handler` struct. Add it to the constructor.

5. In `internal/cli/server.go`, register the three handlers in the `handlers` map:
   ```go
   "GetPlanningStatus": h.GetPlanningStatus,
   "StartPlanning":     h.StartPlanning,
   "StopPlanning":      h.StopPlanning,
   ```
   Add `"StartPlanning"` to `bodyLimits` with `handler.BodyLimitDefault`.

6. Update docs:
   - Add the three routes to `CLAUDE.md` API Routes section under a `### Planning` heading.
   - Add to `docs/internals/api-and-transport.md` if it has an endpoint listing.

## Tests

- `TestGetPlanningStatus`: When planner is not running, returns `{"running": false}`. When running, returns `{"running": true}`.
- `TestStartPlanning`: Returns 202 with `{"running": true}`. Calling again returns 200 (idempotent).
- `TestStopPlanning`: Returns 200 with `{"stopped": true}`. Calling when not running is a no-op (200).
- `TestStartStopPlanning`: Start → verify running → Stop → verify not running.
- Verify route generation: `make api-contract` produces valid `routes.js` with `Routes.planning.status()`, `Routes.planning.start()`, `Routes.planning.stop()`.

## Boundaries

- Do NOT implement conversation/chat message endpoints — that's the planning-chat-agent sub-design spec.
- Do NOT implement log streaming for the planning container — that will come with the chat agent.
- Do NOT add UI elements — that's the spec-mode-ui-shell sub-design spec.
- Do NOT implement workspace-switch handling — that's the server-wiring task.
