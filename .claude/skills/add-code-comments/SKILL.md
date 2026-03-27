---
name: add-code-comments
description: Go through the entire Go codebase, improve code comments, add logic explanations for non-trivial implementations, document uncommented exported symbols, and note any discovered bugs in BUGS.md without fixing them. Uses parallel sub-agents grouped by package area.
argument-hint: [package-filter...]
user-invocable: true
---

# Add Code Comments

Systematically review and improve code comments across the entire Go codebase.
If `$ARGUMENTS` is provided, limit scope to the listed packages/directories
(e.g., `internal/runner internal/store`). Otherwise, process everything.

## Objectives

1. **Add doc comments** to all exported types, functions, methods, and constants
   that lack them. Follow Go conventions (`// FuncName does ...`).
2. **Add inline comments** explaining non-trivial logic: complex conditionals,
   concurrency patterns, algorithmic steps, subtle edge-case handling, non-obvious
   side effects, and "why" behind design choices.
3. **Improve existing comments** that are vague, outdated, or misleading. Do not
   remove accurate comments.
4. **Note bugs** — if you discover what appears to be a bug (logic error, race
   condition, missing error check, off-by-one, etc.), do NOT fix it. Instead,
   append an entry to `BUGS.md` at the repo root with: file path, line range,
   description of the suspected bug, and why you think it is a bug.
5. **Do not change any logic** — only comments and `BUGS.md`. No refactoring, no
   formatting changes, no import reordering.

## Step 0: Inventory

1. Run `find . -name '*.go' -not -path './vendor/*' | wc -l` to gauge scope.
2. Read `CLAUDE.md` to refresh awareness of project conventions.
3. Read `BUGS.md` if it exists, so you don't duplicate entries.
4. Group Go packages into the batches defined in Step 1.

## Step 1: Parallel sub-agent batches

Launch sub-agents in parallel, grouped by related packages so each agent has
enough context to write meaningful comments. Each agent receives the full
instructions from the "Sub-agent instructions" section below.

**Batch 1** — Core infrastructure (launch all in parallel):

| Agent | Packages |
|-------|----------|
| `entry-points` | `main.go`, `doc.go`, `cmd/gen-clone/` |
| `cli` | `internal/cli/` |
| `apicontract-constants-sandbox` | `internal/apicontract/`, `internal/constants/`, `internal/sandbox/` |
| `envconfig-logger-metrics` | `internal/envconfig/`, `internal/logger/`, `internal/metrics/` |

**Batch 2** — Business logic (launch all in parallel):

| Agent | Packages |
|-------|----------|
| `store` | `internal/store/` |
| `handler` | `internal/handler/` — also read `internal/apicontract/routes.go` and `internal/store/` type definitions for context |
| `runner` | `internal/runner/` — also read `internal/store/` type definitions and `internal/sandbox/` for context |
| `gitutil-workspace` | `internal/gitutil/`, `internal/workspace/` |

**Batch 3** — Utility packages and prompts (launch all in parallel):

| Agent | Packages |
|-------|----------|
| `pkg-concurrency` | `internal/pkg/keyedmu/`, `internal/pkg/pubsub/`, `internal/pkg/syncmap/`, `internal/pkg/trackedwg/`, `internal/pkg/watcher/` |
| `pkg-io` | `internal/pkg/atomicfile/`, `internal/pkg/logpipe/`, `internal/pkg/ndjson/`, `internal/pkg/tail/` |
| `pkg-data` | `internal/pkg/cache/`, `internal/pkg/circuitbreaker/`, `internal/pkg/cmdexec/`, `internal/pkg/dagscorer/`, `internal/pkg/lazyval/`, `internal/pkg/pagination/`, `internal/pkg/set/`, `internal/pkg/sortedkeys/` |
| `prompts-scripts` | `prompts/`, `scripts/` |

If `$ARGUMENTS` filters to specific packages, only launch agents whose packages
overlap with the filter.

All batches can be launched simultaneously — there are no ordering dependencies
between them since agents only add comments and do not change logic.

## Sub-agent instructions

Each sub-agent receives these instructions (adapt the package list per agent):

```
You are reviewing Go source files to improve code comments. Your assigned
packages are: <PACKAGE_LIST>

For additional context, also read these files (do NOT modify them):
<CONTEXT_FILES>

### Rules

1. READ every .go file in your assigned packages (including _test.go files).
2. For each file:
   a. Add Go doc comments to exported types, functions, methods, and package-level
      vars/consts that lack them. Use standard Go doc format:
      `// SymbolName does X.`
   b. Add inline comments for non-trivial logic:
      - Complex conditionals or switch cases — explain what each branch handles
      - Concurrency: goroutine launches, channel operations, mutex critical sections,
        sync.Once patterns, context cancellation — explain the synchronization intent
      - Algorithms or multi-step procedures — summarize the approach before the block
      - Error handling that is non-obvious (why an error is ignored, why a specific
        error is wrapped/returned differently)
      - Magic numbers or string literals that aren't self-documenting
      - "Why" comments for code that looks wrong but is intentional
   c. Improve existing comments that are vague ("handle error"), stale (reference
      removed fields), or misleading. Preserve accurate comments.
   d. Do NOT add comments that merely restate the code. Bad: `// increment i` / `i++`.
      Good: `// Retry up to 3 times because the container runtime occasionally
      // returns transient EBUSY errors on first mount.`
   e. For _test.go files: add comments explaining what each test case validates,
      especially table-driven test entries.

3. If you discover a suspected bug, DO NOT fix it. Instead, return it in your
   response under a "## Bugs found" heading with: file path, line number(s),
   description, and reasoning.

4. Do NOT change any code logic, imports, formatting, or variable names.
   Only add or edit comments.

5. Use the Edit tool for all changes. Make targeted edits — do not rewrite
   entire files.
```

Adapt `<CONTEXT_FILES>` per agent:
- `handler` agent: also read `internal/apicontract/routes.go`, `internal/store/task.go`, `internal/store/store.go`
- `runner` agent: also read `internal/store/task.go`, `internal/sandbox/sandbox.go`, `internal/constants/constants.go`
- `cli` agent: also read `main.go`, `internal/envconfig/envconfig.go`
- `gitutil-workspace` agent: also read `internal/store/task.go`
- All others: no extra context files needed

## Step 2: Collect bugs

After all sub-agents complete, collect any bugs they reported. Create or update
`BUGS.md` at the repo root with all findings, organized by package:

```markdown
# Suspected Bugs

Discovered during code comment review on <date>. These have NOT been fixed.

## internal/runner

- **file.go:123-125** — Description of the issue. Reasoning for why it's a bug.

## internal/store

- ...
```

If no bugs were found, do not create `BUGS.md`.

## Step 3: Verify

1. Run `go build ./...` to confirm no syntax errors were introduced.
2. Run `go vet ./...` as a sanity check.
3. If either fails, fix the comment that caused the issue (likely an unclosed
   comment or accidental code modification).

## Step 4: Commit

Stage all changed `.go` files and `BUGS.md` (if created). Create a single commit:

```
all: improve code comments and document non-trivial logic
```

Do NOT push unless the user explicitly asks.

## Step 5: Summary

Report to the user:
- Number of files reviewed and modified
- Highlights: packages with the most additions, notable non-trivial logic documented
- Number of suspected bugs found (if any), with a pointer to `BUGS.md`

## Guidelines

- **Read before writing** — never modify a file you haven't read in this session.
- **Comments only** — zero logic changes. If `go build` or `go vet` fails after
  your edits, you introduced a syntax error in a comment — fix it.
- **Quality over quantity** — a few insightful "why" comments are worth more than
  dozens of trivial "what" comments.
- **Match existing voice** — the codebase uses concise, direct comments. Don't
  write paragraphs where a sentence suffices.
- **Parallelism** — launch as many sub-agents simultaneously as possible. All
  agents only add comments so there are no write conflicts between packages.
