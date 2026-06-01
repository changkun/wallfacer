---
title: Generic Registry[T] + CRUD scaffold for agents and flows
status: archived
depends_on:
  - specs/local/backend-redundancy-cleanup.md
affects:
  - internal/agents/
  - internal/flow/
  - internal/handler/agents.go
  - internal/handler/flows.go
  - internal/pkg/registry/
  - internal/pkg/crud/
effort: large
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Generic Registry[T] + CRUD scaffold for agents and flows

`internal/agents/Registry` and `internal/flow/Registry` differ only in
their value type (`Role` vs `Flow`). Pass-1 cleanup extracted the
watcher (`yamlwatch`), the YAML directory loader (`yamldir`), the
slug validator (`slugutil`), and atomic file writes (`atomicfile`).
The remaining duplication is the **Registry type itself** and the
**HTTP CRUD scaffold** that wraps it.

## Why this is a single spec

This is one cohesive refactor: the registry abstraction enables the
CRUD scaffold, and the CRUD scaffold is what makes the abstraction
worth doing. Splitting them would leave the registry abstract but
still re-implement the handlers in each package.

## Part A — `internal/pkg/registry.Registry[T]`

Replace both `agents.Registry` and `flow.Registry` with a generic:

```go
type Registry[T any] struct {
    order   []string
    byKey   map[string]T
    slugOf  func(T) string
}

func New[T any](slugOf func(T) string, items ...T) *Registry[T]
func (r *Registry[T]) Get(slug string) (T, bool)
func (r *Registry[T]) List() []T
func (r *Registry[T]) Merge(builtins, user []T) error  // rejects user-shadowing-builtin
```

Each package keeps its own thin alias / constructor so call sites
stay readable:

```go
type Registry = registry.Registry[Role]
func NewRegistry(roles ...Role) *Registry {
    return registry.New(func(r Role) string { return r.Slug }, roles...)
}
```

`flow.Registry` also carries `ResolveLegacyKind`, `ResolveForTask`,
and `ResolveRoutineFlow` — three near-identical lookups by different
key fields. After Part A lands, fold them into one
`Resolve(t *store.Task, picker func(*store.Task) string)` per the
pass-1 analysis.

## Part B — `internal/pkg/crud` HTTP scaffold

`internal/handler/agents.go` (245 LOC) and
`internal/handler/flows.go` (283 LOC) mirror each other handler-for-
handler. Every method follows the same shape:

```
decode → validate → IsBuiltin? → exists? → write user file → reload registry → respond
```

Lift the shape into a small generic:

```go
package crud

type Config[T, Req any] struct {
    Registry      *registry.Registry[T]
    SlugOf        func(T) string
    IsBuiltin     func(string) bool
    ToValue       func(Req) T
    Validate      func(Req) error
    BuiltinError  string  // "%q is a built-in; pick a different slug"
    WriteUser     func(slug string, v T) error
    DeleteUser    func(slug string) error
    Reload        func() error
}

func RegisterCRUD[T, Req any](mux *http.ServeMux, base string, cfg Config[T, Req])
```

The handlers in `agents.go` and `flows.go` collapse to a few lines of
config + one `crud.RegisterCRUD` call. Built-in error strings, slug
conflict checks, and write-then-reload sequencing become
single-sourced.

## Migration order

1. Land Part A first. Each package keeps its public type alias so
   existing imports compile.
2. Land Part B. Delete the per-handler scaffolding in agents.go /
   flows.go.
3. The next user-YAML CRUD subsystem (system prompts, routines as
   YAML) gets both for free.

## Tests

- Existing `agents_test.go` and `flow/*_test.go` cover the registry
  behaviour — they should continue to pass without changes.
- Existing `agents_crud_test.go` and `flows_crud_test.go` cover the
  HTTP layer — same.
- Add small unit tests for the new `registry` and `crud` packages
  exercising the type-parameter boundaries.

## Out of scope

- Promoting `internal/prompts` (system prompt CRUD) to use the same
  abstractions. The prompts surface is small enough to benefit but
  has a different file shape (text templates, not YAML). Consider as
  a follow-up after Part B beds in.
- The cache-token bug in `/api/stats` (see sibling spec
  `taskusage-cache-fix.md`).

## Outcome — archived, decision: keep parallel implementations (2026-06-01)

A closer look while implementing the other backend-only redundancy-
cleanup specs concluded the abstraction would be heavier than the
duplication it removed.

**Part A (Registry[T]):**

- `agents.Registry.Get` returns the value directly; `flow.Registry.Get`
  deep-clones via `cloneFlow`. A shared `Registry[T]` would need a
  clone hook (or do nothing) — the asymmetry forces one consumer to
  re-implement what the abstraction was supposed to share.
- `flow.Registry` has three Resolve* methods that can't live on a
  package-aliased generic type; they need a wrapper. Adding a wrapper
  to expose Get/List preserves the type name but doesn't actually
  unify the storage.
- The Registry bodies in both packages are ~30 LOC each. Extracting
  to a generic with a clone hook + two wrapper types + Resolve*
  forwarding lands somewhere between 80 and 120 LOC of new
  infrastructure to replace 60 LOC of duplicate state management. Net
  negative.

**Part B (CRUD scaffold):**

- The CRUD shape (decode → validate → IsBuiltin → exists → write →
  reload → respond) IS duplicated across `agents.go` and `flows.go`,
  but the plug-ins needed for a generic `RegisterCRUD[T, Req]`
  (Registry, SlugOf, IsBuiltin, ToValue, Validate, BuiltinError,
  WriteUser, DeleteUser, Reload, Describe) total ~10 callbacks per
  consumer. The wiring would be ~30 LOC per consumer plus 100+ LOC
  of generic scaffolding — roughly LOC-neutral against today's 528
  LOC across the two handler files, but harder to read because every
  branch lives behind a function pointer.

**What did get done out of this spec:**

The smaller `ResolveForTask` / `ResolveRoutineFlow` consolidation in
`internal/flow/registry.go` — flagged in the pass-1 analysis — landed
as a focused micro-refactor. Both functions now delegate to a single
`resolveByExplicitOrLegacy` helper parameterised by the field
extractors that differ between them. That's the genuine sharing
opportunity in this area; the larger generics extraction was not.

**Re-open conditions:**

If a third user-YAML CRUD subsystem appears (system prompts as YAML,
routine definitions as YAML, etc.) and the duplication crosses three
parallel implementations, the abstraction's overhead amortises and
this spec becomes worth doing.
