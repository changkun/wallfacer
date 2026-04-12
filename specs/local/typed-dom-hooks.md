---
title: Typed DOM Hooks
status: vague
depends_on:
  - specs/local/typescript-migration.md
affects:
  - ui/partials/
  - ui/js/
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Typed DOM Hooks

Idea placeholder — not designed yet.

## Problem

HTML (Go templates in `ui/partials/`), CSS (Tailwind utility classes), and
JS/TS (`ui/js/`) are coupled implicitly. Renaming an `id` or a class in a
partial silently breaks every `getElementById` / `querySelector` call and
every CSS rule that depended on it. The compiler can't help; only runtime
tests catch regressions, and coverage is incomplete.

## Rough idea

- At build time, parse every partial under `ui/partials/` and extract:
  - Every `id="..."` attribute
  - Every `data-js-*` attribute (reserved prefix for JS hooks)
- Emit a generated `.d.ts` (and matching `.ts`) with typed string
  constants — e.g. `export const ID_TASK_CARD = "task-card"`.
- Require TS code to import those constants instead of using raw
  string selectors. Renaming a partial triggers a type-check failure
  everywhere the old name was referenced.
- Adopt a convention: `data-js-*` attributes for JS hooks, class names
  for CSS styling only. CSS renames never break JS; JS hook renames
  never break CSS.

## Open questions

- Generator lives where? Go (like `apicontract`) or Node (co-located
  with the TS build)?
- How to handle dynamically-constructed IDs (e.g. `task-${id}`)? Emit
  a template helper rather than a constant?
- Does this extend to `data-*` attributes used for state (drag sources,
  sortable handles), or only for selectors?
- Freshness test — mirror `internal/apicontract/generate_test.go` so CI
  catches stale hook constants.

## Depends on

TypeScript migration — this idea only pays off once the frontend is
TS, because the type checker is what enforces the contract.

## Not in scope

Component framework adoption (Svelte, Lit, React). This spec is the
minimal contract-enforcement layer that could precede or substitute
for a framework.
