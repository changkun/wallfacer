---
name: scan-and-extract
description: Scan the codebase (Go backend and/or JS frontend) for duplicated structures, algorithms, and abstractions, then extract each into a standalone reusable module under internal/pkg/ or ui/js/lib/. Uses parallel sub-agents — one per extraction.
argument-hint: [go|js|all] [package-filter...]
user-invocable: true
---

# Scan and Extract

Scan the codebase for common patterns, duplicated logic, and extractable
abstractions, then refactor each into a standalone reusable module.

- **Go backend** → `internal/pkg/<name>/`
- **JS frontend** → `ui/js/lib/<name>.js`

If `$ARGUMENTS` starts with `go`, `js`, or `all`, limit the scan to that layer.
Any remaining arguments filter to specific packages/directories. With no
arguments, scan both layers (`all`).

---

## Step 0: Inventory

1. Run `ls internal/pkg/` to see already-extracted Go packages.
2. Run `ls ui/js/lib/` to see already-extracted JS modules.
3. Read `CLAUDE.md` to refresh awareness of project structure.
4. These existing extractions are off-limits — do not re-extract them.

## Step 1: Scan for candidates

Launch an Explore agent (one per layer being scanned) to thoroughly search for
extractable patterns.

### Go candidates

Look for:

1. **Duplicated utility functions** — same or similar helpers in 2+ packages
   (string manipulation, slice operations, file helpers, sanitization)
2. **Common concurrency patterns** — repeated goroutine patterns, worker pools,
   rate limiters, debouncing, throttling not already in `internal/pkg/`
3. **Shared data structures** — ring buffers, priority queues, ordered maps,
   LRU variants not covered by existing packages
4. **Repeated algorithm patterns** — retry logic, polling loops, fallback
   chains, diffing, exponential backoff outside circuitbreaker
5. **Common I/O patterns** — directory traversal with filtering, streaming
   helpers, platform-aware file operations
6. **Shared validation/parsing** — URL parsing, path validation, JSON helpers,
   template rendering utilities
7. **HTTP/transport plumbing** — request/response helpers, middleware utilities

### JS candidates

Look for:

1. **Duplicated utility functions** — same or similar helpers in 2+ files
   (DOM manipulation, text formatting, escaping, date formatting)
2. **Common UI patterns** — modal lifecycle (open/close/dismiss/escape),
   tab switching, panel toggling, raw/preview toggles
3. **Repeated scheduling patterns** — requestAnimationFrame debouncing,
   setTimeout-based throttling, polling with backoff
4. **Shared fetch/SSE patterns** — streaming fetch with chunk accumulation,
   EventSource with reconnect, authenticated API wrappers
5. **Clipboard operations** — copy-to-clipboard with visual button feedback
6. **Form helpers** — select population, checkbox management, value collection
7. **Search/filter helpers** — query matching, text highlighting, fuzzy match
8. **localStorage wrappers** — get/set with defaults, JSON parse/stringify

For each candidate the agent must report:
- What the pattern/abstraction is
- Which files contain duplicated or related code (with line numbers)
- Whether extraction is worthwhile (used in 2+ files, non-trivial logic)

## Step 2: Filter candidates

Review the scan results and discard candidates that:
- Are only used in one file/package (single-location duplication is not worth it)
- Are trivial one-liners (e.g., `min(a, b)` or a single classList toggle)
- Are too domain-specific to generalize
- Would add more indirection than they save
- Already exist in stdlib, `internal/pkg/`, or `ui/js/lib/`

For each remaining candidate, define:
- **Module name** — short, descriptive (e.g., `sanitize`, `clipboard`, `scheduling`)
- **Exported API** — function signatures with doc comments
- **Source files** — which files to extract from and which call sites to update
- **Test plan** — what tests the new module needs

## Step 3: Extract in parallel

Launch one sub-agent per extraction, all in parallel.

### Go extraction agent prompt

```
Extract <description> into a new `internal/pkg/<name>/` package.

## What to extract

<Detailed description of the functions/types to extract, with exact file paths
and line numbers from the scan results.>

## New API

<Exported function signatures with doc comments.>

## Steps

1. Read all source files and their tests.
2. Create `internal/pkg/<name>/<name>.go` with the extracted code.
3. Create `internal/pkg/<name>/<name>_test.go` with tests covering:
   - Happy path for each exported function
   - Edge cases and error paths
   - At least one test per original call site's usage pattern
4. Update all callers — remove duplicated code, import new package, update calls.
5. Remove now-empty source files or unused imports.
6. Run `go build ./...` to verify compilation.
7. Run `go test ./internal/pkg/<name>/ <affected-packages>` to verify.

## Rules
- Read files before modifying. Use Edit tool for changes.
- Prefer generics where it improves type safety.
- Maintain exact same behavior at all call sites.
- Keep the new package dependency-free or stdlib-only when possible.
- Add proper Go doc comments to all exported symbols.
- If a source file becomes empty after extraction, delete it.
- Don't change any unrelated code.
```

### JS extraction agent prompt

```
Extract <description> into a new `ui/js/lib/<name>.js` module.

## What to extract

<Detailed description of the functions to extract, with exact file paths
and line numbers from the scan results.>

## New API

<Function signatures with JSDoc comments.>

## Steps

1. Read all source files involved.
2. Create `ui/js/lib/<name>.js` with the extracted functions.
   - Use plain `function` declarations (no ES modules — these are global scripts).
   - Add a file-level comment explaining what the module provides.
   - Add JSDoc comments for each exported function.
3. Update `ui/partials/scripts.html`:
   - Add `<script src="/js/lib/<name>.js"></script>` BEFORE all non-lib scripts
     (after routes.js, grouped with other lib/ scripts).
4. Update all call sites:
   - Replace inline implementations with calls to the new lib functions.
   - Remove duplicated code from source files.
   - Leave a brief comment noting where the code moved (only at the old location).
5. Update test infrastructure:
   - Add the new module to the `LIB_DEPS` map in `ui/js/tests/lib-deps.js`:
     map each consuming script → its lib dependencies.
   - Verify all test files that load affected scripts import and use `loadLibDeps`.
6. Run `make test-frontend` and fix any failures.

## Rules
- Read files before modifying. Use Edit tool for changes.
- These are global scripts loaded via <script> tags, NOT ES modules.
- Maintain exact same behavior at all call sites.
- Keep modules small and focused — one concern per file.
- Don't change any unrelated code.
- Test contexts may stub lib functions — if a test already provides its own stub
  (e.g., `escapeHtml`), the lib version loaded via loadLibDeps will be overridden
  by the stub, which is fine.
```

## Step 4: Verify

After all agents complete:

**Go:**
1. Run `go build ./...` to confirm clean compilation.
2. Run `go vet ./...` as a sanity check.
3. Run `go test ./...` to confirm no regressions.
4. Fix any issues (stale imports, missing type params, etc.).

**JS:**
1. Run `make test-frontend` to confirm no regressions.
2. Fix any issues (missing lib-deps entries, test stubs, etc.).

## Step 5: Commit

Create one commit per extracted module:

- Go: `internal/pkg/<name>: extract <brief description>`
- JS: `ui: extract <brief description> into js/lib/<name>.js`

Do NOT push unless the user explicitly asks.

## Step 6: Summary

Report:
- Number of modules extracted (Go + JS)
- For each: module name, exported API, files that now use it, code removed
- Any candidates that were scanned but skipped, with reasons

## Guidelines

- **Cross-file duplication is the primary signal** — if code only appears in
  one file/package, extraction rarely pays off.
- **Go: prefer generics** — when the extracted API uses `any` parameters,
  consider whether generics would provide compile-time type safety.
- **JS: plain functions** — no classes, no ES modules. These are global scripts.
- **Minimal modules** — each extracted module should do one thing well. Don't
  create kitchen-sink utility packages.
- **No speculative extraction** — only extract patterns that exist today in 2+
  places. Don't extract "in case we need it later."
- **Preserve behavior** — extraction must be a pure refactor. No behavior changes.
- **Parallelism** — launch all extraction agents simultaneously. They operate on
  disjoint file sets so there are no write conflicts.
