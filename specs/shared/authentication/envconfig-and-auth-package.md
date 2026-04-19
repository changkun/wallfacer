---
title: WALLFACER_CLOUD env flag and internal/auth re-export
status: archived
depends_on: []
affects:
  - internal/envconfig/
  - internal/auth/
  - go.mod
  - go.sum
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# WALLFACER_CLOUD env flag and internal/auth re-export

## Goal

Add the `WALLFACER_CLOUD` feature flag to `internal/envconfig/` and create
`internal/auth/` as a thin re-export of `latere.ai/x/pkg/oidc`. This is the
foundation all other Phase 1 tasks build on.

## What to do

1. Add `latere.ai/x/pkg/oidc` to `go.mod`. Run `go mod tidy`. The module is a
   vanity path served by latere.ai's `go-import` meta; it resolves to
   `github.com/latere-ai/pkg`.
2. Extend `internal/envconfig/envconfig.Config` with:
   ```go
   Cloud bool `env:"WALLFACER_CLOUD"`
   ```
   Parse `"true"`/`"1"`/`"yes"` (case-insensitive) as true, everything else
   as false. Match the existing boolean-parsing pattern in that file (grep for
   other bool fields in `Parse`).
3. Plumb the new field through whatever struct the handler layer reads config
   from on startup (check how `HostMode` flows today, `handler.GetConfig`
   already reads `h.runner.HostMode()`; pick the equivalent surface for a
   plain env-sourced flag).
4. Create `internal/auth/auth.go` as a thin re-export, matching
   `~/dev/latere.ai/latere-ai/internal/auth/auth.go`:
   ```go
   // Package auth re-exports latere.ai/x/pkg/oidc so internal packages import
   // a single path. Nil-safety semantics come from the platform package:
   // oidc.New returns nil when required env is unset.
   package auth

   import "latere.ai/x/pkg/oidc"

   type (
       Client  = oidc.Client
       Config  = oidc.Config
       User    = oidc.User
       Session = oidc.Session
   )

   var (
       LoadConfig   = oidc.LoadConfig
       New          = oidc.New
       ClearSession = oidc.ClearSession
   )
   ```
5. Do NOT wire any routes or handlers in this task. Later tasks consume the
   package.

## Tests

- `internal/envconfig/envconfig_test.go`:
  - `TestParse_CloudTrue`, `WALLFACER_CLOUD=true` → `cfg.Cloud == true`.
  - `TestParse_CloudFalse`, `WALLFACER_CLOUD=false` → `cfg.Cloud == false`.
  - `TestParse_CloudUnset`, missing var → `cfg.Cloud == false` (default).
  - `TestParse_CloudTruthyVariants`, `"1"`, `"yes"`, `"TRUE"` all parse true.
- `internal/auth/auth_test.go`: minimal smoke test that `auth.New(auth.Config{})`
  returns nil (matches platform's graceful-degrade contract), keeps the
  re-export honest.

## Boundaries

- Do not register any HTTP routes. Do not touch `internal/handler/`.
- Do not add `jwtauth`, Phase 1 is OIDC browser login only.
- Do not add auth config to the Settings UI / `.env` editor in this task;
  `WALLFACER_CLOUD` is set via shell env, not via the in-app `.env`.
- Do not add `org_id` or `principal_id` anywhere.

## Outcome

Delivered. `latere.ai/x/pkg/oidc` wired into `go.mod`; `internal/auth/auth.go`
re-exports `Client`, `Config`, `User`, `Session`, `LoadConfig`, `New`,
`ClearSession` exactly as specified. `internal/envconfig/envconfig.go` parses
`WALLFACER_CLOUD` with the project's existing truthy-variant rules
(`true`/`1`/`yes`, case-insensitive).

### What shipped
- `internal/auth/auth.go` (~30 LOC) + smoke test `internal/auth/auth_test.go`
  verifying `auth.New(auth.Config{})` returns nil (graceful-degrade contract).
- `Cloud bool` field on `envconfig.Config` with four parse tests in
  `internal/envconfig/envconfig_test.go` (true, false, unset, truthy variants).
- `go.mod` / `go.sum` updated with `latere.ai/x/pkg/oidc`.

### Design evolution
No deviations. The re-export file matches the latere-ai reference layout
line-for-line; envconfig plumbed `Cloud` through the existing boolean-field
pattern, consumed by the CLI boot path in the follow-up routes task.
