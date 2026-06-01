---
title: Generic Registry[T] + CRUD scaffold for agents and flows
status: drafted
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
