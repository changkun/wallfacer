---
title: Spec Model Types and YAML Parsing
status: validated
track: local
depends_on: []
affects:
  - internal/spec/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec Model Types and YAML Parsing

## Goal

Define the core Go types for spec documents and implement YAML frontmatter parsing from markdown files. This is the foundational data model that all other spec operations build on.

## What to do

1. Create `internal/spec/` package directory.

2. Create `internal/spec/model.go` with the following types:

   - `SpecStatus` string type with constants: `StatusVague`, `StatusDrafted`, `StatusValidated`, `StatusComplete`, `StatusStale`.
   - `SpecTrack` string type with constants: `TrackFoundations`, `TrackLocal`, `TrackCloud`, `TrackShared`.
   - `SpecEffort` string type with constants: `EffortSmall`, `EffortMedium`, `EffortLarge`, `EffortXLarge`.
   - `Spec` struct with fields matching the frontmatter:
     ```go
     type Spec struct {
         Title            string     `yaml:"title"`
         Status           SpecStatus `yaml:"status"`
         Track            SpecTrack  `yaml:"track"`
         DependsOn        []string   `yaml:"depends_on"`
         Affects          []string   `yaml:"affects"`
         Effort           SpecEffort `yaml:"effort"`
         Created          Date       `yaml:"created"`
         Updated          Date       `yaml:"updated"`
         Author           string     `yaml:"author"`
         DispatchedTaskID *string    `yaml:"dispatched_task_id"`

         // Derived fields (not from YAML)
         Path    string // relative path from repo root (e.g., "specs/local/foo.md")
         Body    string // markdown content below frontmatter
     }
     ```
   - `Date` type wrapping `time.Time` with custom YAML unmarshal for `YYYY-MM-DD` format.

3. Create `internal/spec/parse.go` with:

   - `ParseFile(path string) (*Spec, error)` — reads a file, splits frontmatter from body, unmarshals YAML into `Spec`, sets `Path` and `Body` fields.
   - Frontmatter detection: split on `---` delimiters (first two occurrences).
   - Use `gopkg.in/yaml.v3` (already in go.mod).

4. Create `internal/spec/parse_test.go` and `internal/spec/model_test.go`.

## Tests

- `TestParseFile_ValidSpec`: Parse a well-formed spec file, verify all fields populated correctly.
- `TestParseFile_AllStatuses`: Parse specs with each valid status value.
- `TestParseFile_AllTracks`: Parse specs with each valid track value.
- `TestParseFile_AllEfforts`: Parse specs with each valid effort value.
- `TestParseFile_NullDispatchID`: Verify `dispatched_task_id: null` parses as nil pointer.
- `TestParseFile_UUIDDispatchID`: Verify a UUID string parses correctly.
- `TestParseFile_EmptyDependsOn`: Verify empty `depends_on` parses as empty slice.
- `TestParseFile_MultipleDependsOn`: Verify multiple entries parse correctly.
- `TestParseFile_BodyExtraction`: Verify markdown body below frontmatter is captured.
- `TestParseFile_MissingFrontmatter`: File without `---` delimiters returns error.
- `TestParseFile_EmptyFile`: Empty file returns error.
- `TestParseFile_NoEndDelimiter`: Frontmatter without closing `---` returns error.
- `TestDate_UnmarshalYAML`: Verify `YYYY-MM-DD` string parses to correct time.Time.
- `TestDate_InvalidFormat`: Non-date strings return error.

## Boundaries

- Do NOT implement validation logic (that's a separate task).
- Do NOT implement tree building or filesystem traversal.
- Do NOT implement lifecycle transition rules.
- Do NOT add HTTP handlers or CLI commands.
- Do NOT create a store or persistence layer for specs — specs are read from the filesystem.
