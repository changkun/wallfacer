---
title: Handler helper deduplication (bearer / slices / PathUUID / orgs decode)
status: complete
depends_on:
  - specs/local/backend-redundancy-cleanup.md
affects:
  - internal/auth/middleware.go
  - internal/handler/sandbox_proxy.go
  - internal/handler/routines.go
  - internal/handler/orgs.go
  - internal/pkg/httpjson/
effort: small
created: 2026-06-01
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Handler helper deduplication

Four micro-cleanups that share no real dependency but each touch the
handler layer and are too small to be their own spec. Group them so
one task can knock them all out.

## 1. Unify the two `bearerToken` helpers

- `internal/auth/middleware.go:140` extracts from `*http.Request`,
  used by `Auth` and `OptionalAuth`.
- `internal/handler/sandbox_proxy.go:278` takes a raw header string,
  used by `requireClaims`.

Both check the same `Bearer ` prefix and trim. Keep the canonical
helper in `internal/auth` (string-arg signature is more flexible) and
have both consumers use it.

## 2. Replace `hasScope` / `hasAud` with `slices.Contains`

`internal/handler/sandbox_proxy.go:287-302` open-codes two six-line
loops that are exactly `slices.Contains`. Drop the helpers, call
`slices.Contains` directly. The handler imports `slices` already.

## 3. Extract `httpjson.PathUUID`

Three UUID path-parsing variants exist:

- `withID` shim in `internal/cli/server.go:917` (hardcoded `id` param).
- `parsePathID` in `internal/handler/routines.go:282` (configurable
  param name).
- A few inline `uuid.Parse(r.PathValue("id"))` leftovers.

Promote `parsePathID` to `internal/pkg/httpjson.PathUUID(w, r, name)`,
have `withID` call it with `"id"`, and migrate the inline holdouts.
Delete `parsePathID` from `routines.go`.

## 4. `orgs.go` SwitchOrg should use `httpjson.DecodeBody`

`internal/handler/orgs.go:160` still hand-rolls
`json.NewDecoder(r.Body).Decode(&req)`. The other four `json.Unmarshal`
sites in the handler package are not HTTP request bodies and are
fine to leave alone. One-line fix to bring SwitchOrg in line.

## Tests

- bearer dedup: existing `auth` middleware tests + a unit test for the
  promoted helper string-arg signature.
- `httpjson.PathUUID`: small table test (valid uuid, missing param,
  malformed uuid).
- orgs decode: existing `SwitchOrg` tests should pass unchanged; if
  the body shape changes (it shouldn't), they cover it.

## Out of scope

The `Auth` vs `OptionalAuth` middleware near-duplication
(`internal/auth/middleware.go:66-109`) — the two helpers differ only
in their fail branch (skip vs `writeUnauthorized`); parameterising
adds indirection that probably hurts readability more than the
deduplication helps. Skip unless a third caller appears.
