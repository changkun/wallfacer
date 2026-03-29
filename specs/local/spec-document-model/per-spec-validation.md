---
title: Per-Spec Validation
status: validated
track: local
depends_on:
  - specs/local/spec-document-model/spec-model-types.md
  - specs/local/spec-document-model/spec-lifecycle.md
affects:
  - internal/spec/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Per-Spec Validation

## Goal

Implement per-spec validation rules that check structural correctness of individual spec documents. These run independently of the spec tree (no cross-spec context needed).

## What to do

1. Create `internal/spec/validate.go` with:

   - `ValidationSeverity` type: `SeverityError`, `SeverityWarning`.

   - `ValidationResult` struct:
     ```go
     type ValidationResult struct {
         Path     string             // spec relative path
         Severity ValidationSeverity
         Rule     string             // rule identifier (e.g., "required-fields")
         Message  string             // human-readable description
     }
     ```

   - `ValidateSpec(spec *Spec, repoRoot string) []ValidationResult` — runs all per-spec rules and returns all violations (not just the first). Takes `repoRoot` to resolve `affects` paths.

2. Implement these validation rules (from the spec):

   | Rule | Severity | Check |
   |------|----------|-------|
   | `required-fields` | error | `title`, `status`, `track`, `effort`, `created`, `updated`, `author` must be non-empty |
   | `valid-status` | error | `status` is one of the 5 valid values |
   | `valid-track` | error | `track` is one of the 4 valid values |
   | `valid-effort` | error | `effort` is one of the 4 valid values |
   | `track-matches-path` | error | `track` matches the spec's filesystem location (`specs/<track>/...`) |
   | `date-format` | error | `created` and `updated` are valid dates |
   | `date-ordering` | error | `updated` >= `created` |
   | `no-self-dependency` | error | spec's own path does not appear in `depends_on` |
   | `dispatch-consistency` | error | non-leaf specs must have nil `dispatched_task_id`; leaf specs may have nil or valid UUID |
   | `depends-on-exist` | error | every path in `depends_on` resolves to an existing file (relative to repo root) |
   | `affects-exist` | warning | every path in `affects` resolves to an existing file or directory |
   | `body-not-empty` | warning | specs beyond `vague` status should have non-empty body |

3. The `dispatch-consistency` rule needs to know if a spec is a leaf. Pass this as a parameter: `ValidateSpec(spec *Spec, repoRoot string, isLeaf bool) []ValidationResult`.

## Tests

- `TestValidateSpec_Valid`: A fully valid spec returns no errors or warnings.
- `TestValidateSpec_MissingTitle`: Missing title triggers `required-fields` error.
- `TestValidateSpec_MissingMultipleFields`: Multiple missing fields produce multiple errors.
- `TestValidateSpec_InvalidStatus`: Invalid status string triggers `valid-status` error.
- `TestValidateSpec_InvalidTrack`: Invalid track triggers `valid-track` error.
- `TestValidateSpec_InvalidEffort`: Invalid effort triggers `valid-effort` error.
- `TestValidateSpec_TrackMismatch`: Track doesn't match path triggers `track-matches-path` error.
- `TestValidateSpec_DateOrdering`: `updated` before `created` triggers `date-ordering` error.
- `TestValidateSpec_SelfDependency`: Spec path in own `depends_on` triggers `no-self-dependency` error.
- `TestValidateSpec_NonLeafWithDispatch`: Non-leaf spec with non-nil dispatch ID triggers `dispatch-consistency` error.
- `TestValidateSpec_LeafWithDispatch`: Leaf spec with dispatch ID — no error.
- `TestValidateSpec_DependsOnMissing`: Non-existent `depends_on` path triggers error.
- `TestValidateSpec_AffectsMissing`: Non-existent `affects` path triggers warning (not error).
- `TestValidateSpec_EmptyBodyWarning`: Spec with `drafted` status and empty body triggers warning.
- `TestValidateSpec_VagueEmptyBody`: Spec with `vague` status and empty body — no warning.
- `TestValidateSpec_AllRulesRun`: Spec with multiple issues — verify all relevant rules fire, not just the first.

Use `t.TempDir()` to create test file structures for `depends_on`/`affects` existence checks.

## Boundaries

- Do NOT implement cross-spec validation (DAG cycles, orphan detection, status consistency across tree).
- Do NOT implement validation CLI or HTTP endpoint.
- Do NOT modify the `Spec` struct.
