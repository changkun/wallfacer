---
title: TypeScript Migration
status: drafted
depends_on: []
affects:
  - ui/
  - ui/js/
  - ui/package.json
  - ui/biome.json
  - ui/vitest.config.js
  - Makefile
effort: large
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# TypeScript Migration

Migrate the entire wallfacer frontend from JavaScript to TypeScript, gradually,
without disrupting the existing no-bundler `<script>`-tag architecture or the
Go binary's `//go:embed ui` pipeline.

---

## Motivation

The frontend currently consists of ~92 hand-authored JS modules plus ~90
Vitest test files in `ui/js/`. All modules run in the browser global scope —
there are no ES modules or bundler. Over time this has led to:

- No compile-time guarantees about function signatures across files.
- Silent regressions when handlers change shape (events, API payloads, DOM
  contracts).
- Heavy reliance on test coverage and runtime checks to catch type errors
  that a type system would refuse to compile.
- Editor IntelliSense that works only to the extent the globals happen to
  be in scope.

TypeScript would catch these at build time with near-zero runtime cost, while
the Biome lint pass can stay exactly the same.

## Scope

**In scope**
- Introduce a TypeScript toolchain in `ui/` (tsconfig, esbuild, typecheck).
- Migrate every hand-authored file under `ui/js/` and `ui/js/lib/` and
  `ui/js/office/` from `.js` to `.ts`.
- Migrate tests under `ui/js/tests/` from `.js` to `.ts`.
- Add `ui/types/globals.d.ts` declaring cross-file globals incrementally as
  modules migrate.
- Wire TS compile + typecheck into `make build` and `make test`.
- Update Biome and Vitest to lint/run `.ts` alongside `.js`.

**Out of scope**
- Converting to ES modules (`import`/`export` in `<script type="module">`).
  That is a separate, equally large refactor (rewrite every file plus
  `scripts.html`, and probably adopt a bundler). Treat as a follow-up spec.
- Rewriting `ui/js/vendor/*` (third-party: highlight, marked, sortable, xterm).
- Rewriting `ui/js/generated/*` (emitted by `scripts/gen-api-contract.go`).
  These will get companion `.d.ts` files instead.
- Adopting a frontend framework (React/Vue/etc).
- Rewriting Go API types as shared TS types (a future spec could generate
  `.d.ts` from `internal/apicontract`, but this migration leaves
  `generated/types.js` as-is and adds `.d.ts` shims).

## Design

### Architecture: TS source in place, `.js` as committed build artifact

Source files live at the same paths as today but with a `.ts` extension:

```
ui/js/lib/clipboard.ts        # source of truth
ui/js/lib/clipboard.js        # build artifact, committed
```

**Both files are committed.** This follows the project's existing
precedent for generated artifacts — `ui/js/generated/routes.js` is
emitted by `scripts/gen-api-contract.go` and lives in git. Committing
the `.js` means a fresh `go build` works without the Node toolchain,
matching the behavior today. `make ui-ts` keeps the `.js` in sync
whenever the `.ts` changes; a future test will enforce freshness in CI
(mirroring the pattern in `internal/apicontract/generate_test.go`).

The `//go:embed ui` directive cannot be easily narrowed, so `.ts` source
files WILL be embedded into the Go binary alongside the compiled `.js`.
At current sizes this is ~200–300 KB of dead weight in the binary, which
is acceptable. The advisor flagged this; reassess after migration.

Rejected alternatives:

| Option | Why rejected |
|---|---|
| `ui/src/*.ts` → compile to `ui/js/*.js` | Requires restructuring directories AND adding TS simultaneously. Two risks at once. |
| `frontend/src/*.ts` → compile to `ui/js/*.js` | Most disruptive. Touches every path reference across Go handlers and templates. |
| JSDoc + `checkJs: true` | Gives ~70% of the type safety with 0% churn, but stops short of real refactors (generics, interfaces, narrow types). Rejected in favor of full TS. |
| Big-bang rewrite | 92 modules + 90 tests at once. Blocks all other frontend work for the duration, produces an un-reviewable diff. |

### Build pipeline

Two tools, one each for speed and correctness:

- **esbuild** transpiles `.ts` → `.js` in place. Fast (tens of ms), zero
  config, preserves the per-file global-scope semantics via
  `--format=iife --global-name=...` when needed (most files don't need a
  wrapper because they declare top-level `function`/`var` that end up in
  the script's global scope under the no-module model).
- **tsc --noEmit** runs the full type checker. Slow but correct. Runs in
  `make lint` and in CI.

`esbuild` is installed as a `devDependency` in `ui/package.json` along with
`typescript`. The Makefile orchestrates both. There is no `npm run build`
step exposed to end users — the Makefile is the single entrypoint.

### Global-scope semantics

Because the runtime model is still "every script dumped into the window
global scope" (not ES modules), each compiled `.js` file must expose the
same top-level names it does today. esbuild's default behavior when given a
`.ts` file with top-level `function foo(){}` is to emit exactly that — no
IIFE, no `export`. Verified with the pilot.

`ui/types/globals.d.ts` declares the names that other files rely on
without `import`. Example:

```ts
// ui/types/globals.d.ts
declare function copyWithFeedback(
  text: string,
  btn: HTMLElement | null,
  feedback?: string,
  duration?: number,
): void;
```

This file grows monotonically as migration progresses. When every module
has migrated, `globals.d.ts` is a complete type index of the frontend's
public API.

### Test loading

Tests use `vm.runInContext` against raw `.js` source read from disk (see
`ui/js/tests/lib-deps.js`). After migration they read the **compiled** `.js`
output, so:

- `make test-frontend` depends on `make ui-ts` to ensure compiled output
  exists before Vitest runs.
- `ui/js/tests/` files themselves migrate to `.ts`. Vitest natively runs
  `.ts` test files via esbuild, no extra config required.
- `lib-deps.js` becomes `lib-deps.ts` but continues to reference the
  compiled `.js` files (that is what `vm.runInContext` needs).

### Biome and Vitest config changes

```json
// ui/biome.json — lint both .js and .ts with the same rule set
"files": {
  "ignore": [
    "js/vendor/**",
    "js/generated/**",
    "css/vendor/**",
    "node_modules/**"
  ]
}
```

Biome 1.9 lints `.ts` natively and shares configuration with `.js`, so no
additional setup is needed. The compiled `.js` twins of migrated `.ts`
files remain lintable — esbuild's transpiled output is
Biome-compatible (verified on the clipboard pilot).

```js
// ui/vitest.config.js — accept .ts tests
test: {
  include: ["js/tests/**/*.test.{js,ts}"],
  coverage: {
    include: ["js/**/*.{js,ts}"],
    exclude: ["js/vendor/**", "js/tests/**"],
  }
}
```

### Makefile changes

Add a `ui-ts` target. Wire it into `build` and `test-frontend` so the
compiled `.js` artifacts are fresh before Go embeds them or Vitest loads
them.

```make
# Compile TypeScript sources to JavaScript alongside .ts files.
ui-ts:
	cd ui && npx --yes --package=typescript@5 --package=esbuild@0.25 \
		node scripts/build-ts.mjs

# Run strict type-check without emitting output.
typecheck-js:
	cd ui && npx --yes tsc@5 --noEmit

build: fmt lint ui-ts build-binary pull-images
lint-js: ...  # add typecheck-js dependency
test-frontend: ui-ts
	cd ui && npx --yes vitest@2 run
```

### Git tracking

Both `.ts` (source) and `.js` (build artifact) are committed, following
`ui/js/generated/routes.js` precedent. A follow-up CI test should assert
that `make ui-ts` produces no diff (mirroring
`internal/apicontract/generate_test.go`) — this catches stale compiled
JS in PRs before it ships.

After the full migration is complete, a separate spec can evaluate
switching to gitignored build output plus a mandatory pre-build step.

## Migration order

1. **Pilot** — `ui/js/lib/clipboard.ts` (850 B, leaf, pure DOM APIs, no
   cross-file globals). Demonstrates the full toolchain end-to-end.
2. **Rest of `ui/js/lib/`** — 11 small utility files with few cross-deps.
3. **Leaf app modules** — `theme.js`, `events.js`, `state.js`, `markdown.js`,
   `oversight-shared.js`, etc. (no or few callers).
4. **Mid-tier modules** — `api.js`, `render.js`, `tasks.js`, `modal-*.js`.
   Each conversion grows `globals.d.ts`.
5. **Entry modules** — `command-palette.js`, `planning-chat.js`,
   `spec-mode.js`, `terminal.js`, `explorer.js`.
6. **`ui/js/office/`** — pixel-art module. Self-contained; can migrate any
   time in parallel with step 4+.
7. **Tests** — convert `ui/js/tests/**/*.test.js` → `.test.ts`. Vitest
   handles `.ts` natively. Last, so test infrastructure stabilizes.
8. **Cleanup** — generate `.d.ts` companions for `generated/types.js`
   and `generated/routes.js`. Add a CI freshness test that runs
   `make ui-ts` and fails on diff. Evaluate whether to flip compiled
   `.js` files to gitignored at that point.

Every step is a standalone commit that keeps `make test` green.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| esbuild emits different top-level semantics than hand-written JS | Pilot verifies byte-for-byte equivalent top-level bindings. If esbuild ever wraps output in an IIFE, switch to `--format=esm` and add a post-processing pass, or use `tsc` emit with `target=es2022, module=none`. |
| `make build` breaks for devs who only run `go build` | Document in CLAUDE.md and `docs/guide/configuration.md` that `make build` is now required. `go build` still works but will embed stale JS. |
| Embedded binary size grows | Measured ~200–300 KB at 46 source files. Acceptable. Reassess after migration. |
| `globals.d.ts` becomes a maintenance burden | Short term, yes. Long term, it is the catalog of the frontend's global surface area — valuable documentation. Freezes when migration completes. |
| Tests that `readFileSync` a specific JS file break | Every place that reads `.js` continues to work because we keep compiled `.js` output. The path never changes. |
| Vitest config `include` pattern mismatches | Tested in the pilot. |

## Acceptance criteria

- `ui/tsconfig.json` exists with `strict: true`, `noEmit: true`, targets
  browser DOM + ES2022.
- `ui/types/globals.d.ts` exists and declares every cross-file global used
  by migrated files.
- `make ui-ts` compiles every `.ts` under `ui/js/` to `.js` in place.
- `make typecheck-js` passes with zero errors.
- `make build` produces a working binary that serves the compiled JS.
- `make test` passes — all backend + frontend tests green.
- Every hand-authored JS file under `ui/js/` (excluding `vendor/` and
  `generated/`) has been converted to `.ts`.
- `ui/js/tests/` uses `.ts` throughout.
- Biome lints `.ts` with the same rule set as `.js`.
- A CI test asserts that `make ui-ts` produces no diff (parity with the
  apicontract generator's freshness test).
- CLAUDE.md and `docs/guide/configuration.md` note the new build
  requirement.

## Not in this spec (follow-ups)

- **ES modules migration**: convert `<script>` tags to
  `<script type="module">`, replace globals with `import`/`export`, adopt a
  bundler (esbuild bundle mode or Vite). Larger scope, different risk
  profile.
- **Generated API types**: emit `internal/apicontract` as `.d.ts` so
  handlers and frontend share types. Worth a dedicated spec.
- **Strictness ratchet**: after initial migration, tighten `tsconfig` (e.g.
  `noImplicitAny`, `strictNullChecks` already on; consider
  `exactOptionalPropertyTypes`, `noUncheckedIndexedAccess`).

## Pilot (completed alongside this spec)

The pilot converts `ui/js/lib/clipboard.js` — a single 25-line function
with no cross-file references — and lands the full toolchain:

- `ui/tsconfig.json`
- `ui/types/globals.d.ts` (initially tiny)
- `ui/scripts/build-ts.mjs` (esbuild driver)
- `ui/package.json` devDeps and scripts
- Makefile `ui-ts`, `typecheck-js` targets
- Biome, Vitest, `.gitignore` updates
- `ui/js/lib/clipboard.ts` replacing `clipboard.js`

After the pilot lands, subsequent migrations are mechanical: rename,
annotate, adjust `globals.d.ts`, ship.
