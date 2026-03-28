# Task 7: Frontend Tests and Documentation

**Status:** Todo
**Depends on:** Task 5, Task 6
**Phase:** Phase 1 — Read-Only Browsing + Preview
**Effort:** Medium

## Goal

Write frontend unit tests for testable explorer logic and update all documentation to reflect the new file explorer feature. This completes Phase 1.

## What to do

### Frontend Tests

1. Create `ui/js/tests/explorer.test.js` using the Vitest + VM context pattern:

   ```javascript
   import { describe, it, expect, beforeAll } from "vitest";
   import { readFileSync } from "fs";
   import vm from "vm";
   // Follow existing test patterns in ui/js/tests/
   ```

2. Test cases for pure logic functions extracted from explorer.js:
   - Tree node creation from API response entries
   - Workspace root node initialization
   - Node expand/collapse state transitions
   - Any sorting or filtering logic on the client side

### Documentation

3. Update `docs/guide/board-and-tasks.md`:
   - Add a section about the file explorer panel
   - Describe how to open (header button, Ctrl+E)
   - Describe tree browsing (click to expand, lazy loading)
   - Describe file preview (click file, syntax highlighting, binary handling)
   - Describe resize behavior

4. Update `CLAUDE.md`:
   - Add the three explorer API routes to the API Routes section:
     - `GET /api/explorer/tree` — List one level of a workspace directory
     - `GET /api/explorer/file` — Read file contents from a workspace
   - Add `ui/js/explorer.js` and `ui/css/explorer.css` to relevant file listings
   - Note the `PUT` route placeholder for Phase 2

5. Update `docs/internals/api-and-transport.md` if it contains route documentation:
   - Add explorer endpoints with request/response format

6. Update `ui/partials/scripts.html` comment header if scripts are documented there.

## Tests

The frontend tests themselves ARE this task's deliverable. Run `cd ui && npx vitest@2 run` to verify they pass.

## Boundaries

- Do NOT implement Phase 2 features (editing)
- Do NOT add integration/E2E tests — unit tests for pure logic only
- Do NOT modify backend code
- Documentation should only cover Phase 1 (read-only browsing + preview)
