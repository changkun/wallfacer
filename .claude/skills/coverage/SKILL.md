---
name: coverage
description: Improve code coverage up to a target percentage. Measures current Go and JS coverage, identifies low-coverage packages/files, writes tests, and iterates until the target is reached.
argument-hint: "<target%> [go|js] [package-or-file-filter...]"
user-invocable: true
---

# Coverage

Improve code coverage to a target percentage by identifying untested code and
writing tests iteratively.

## Argument parsing

Parse `$ARGUMENTS`:
- **Target** (required): A number like `80` or `80%`. This is the coverage target.
- **Language filter** (optional): `go`, `js`, or omitted for both.
- **Scope filter** (optional): Package paths or file globs to restrict which
  packages/files to improve. If omitted, improve all packages.

Examples:
- `/coverage 80` — both Go and JS to 80%
- `/coverage 90 go` — Go only to 90%
- `/coverage 75 js` — JS only to 75%
- `/coverage 85 go internal/runner/` — Go, only the runner package

## Step 0: Measure baseline coverage

### Go (if not filtered to JS-only)

Run coverage and capture the per-package breakdown:

```bash
go test ./... -coverprofile=coverage.out -count=1 2>&1 || true
```

Then parse the profile to get per-package percentages:

```bash
go tool cover -func=coverage.out
```

Record:
- **Overall coverage** (the `total:` line)
- **Per-package coverage** as a sorted list (lowest first)

If a scope filter was given, restrict attention to matching packages.

### JS (if not filtered to Go-only)

```bash
cd ui && npx --yes vitest@2 run --coverage --coverage.reporter=text 2>&1 || true
```

Record:
- **Overall coverage** from the summary line
- **Per-file coverage** sorted lowest first

If a scope filter was given, restrict attention to matching files.

Report the baseline to the user:
- Overall Go coverage: X%
- Overall JS coverage: Y%
- Bottom 5 packages/files by coverage

## Step 1: Plan coverage improvements

For each language that is below the target:

1. Identify the **bottom 5 packages/files** by coverage percentage.
2. For each, run coverage with line-level detail to find **uncovered functions
   and branches**:
   - Go: read `coverage.out` profile data, or use `go tool cover -func` output
     to find functions at 0% or low coverage.
   - JS: use the per-file line coverage from vitest output.
3. Prioritize by **impact**: large uncovered functions in important packages
   first. Skip generated code, test helpers, and vendor files.
4. Create a ranked list of test-writing tasks, each describing:
   - The file and function/export to test
   - What the test should cover (happy path, edge cases, error paths)
   - Expected coverage gain (rough estimate)

Present the plan to the user. Do NOT ask for confirmation — proceed directly.

## Step 2: Write tests iteratively

For each item in the plan (highest impact first):

### 2a. Read the source code

- Read the target file and understand the untested code paths.
- Read existing tests for the package/file to follow conventions.

### 2b. Write tests

- Add tests that exercise the uncovered code paths.
- Follow existing test patterns, naming, and file organization.
- Place Go tests in `_test.go` files in the same package.
- Place JS tests adjacent to source files following existing conventions
  (check for `*.test.js`, `*.spec.js`, or `__tests__/` patterns).
- Test real behavior — do not write trivial tests that just assert `true`.
- Cover: happy paths, error returns, edge cases, branch conditions.

### 2c. Run tests to verify

- Go: `go test ./path/to/package/... -count=1`
- JS: `cd ui && npx --yes vitest@2 run path/to/test`

Fix any failures before proceeding.

### 2d. Re-measure coverage

After each batch of tests (per package/file), re-measure coverage:

- Go: `go test ./... -coverprofile=coverage.out -count=1 && go tool cover -func=coverage.out`
- JS: `cd ui && npx --yes vitest@2 run --coverage --coverage.reporter=text`

If overall coverage has reached the target, stop early — do not write
unnecessary tests.

### 2e. Iterate

If coverage is still below target, move to the next item in the plan.
If the plan is exhausted but coverage is still below target:
1. Re-analyze coverage to find the next set of uncovered code.
2. Extend the plan with new items.
3. Continue writing tests.

Stop when:
- The target is reached, OR
- Remaining uncovered code is unreachable, generated, or would require
  extensive mocking of external systems (report these as "hard to cover"
  in the summary).

## Step 3: Final verification

1. Run `make fmt` and `make lint` — fix any issues.
2. Run full test suites:
   - `go test ./... -count=1`
   - `cd ui && npx --yes vitest@2 run`
3. Measure final coverage for both languages and record the numbers.

## Step 4: Summary

Report:
- **Before**: Go X%, JS Y%
- **After**: Go X%, JS Y%
- **Delta**: +N% Go, +M% JS
- **Tests added**: count of new test functions/files, grouped by package
- **Hard-to-cover code**: any remaining uncovered code that was skipped and why
- If target was not reached, explain what remains and what would be needed

## Guidelines

- **Read before writing** — always read source and existing tests before adding new ones.
- **No trivial tests** — every test must exercise real logic. No `assert(true)`.
- **Follow conventions** — match existing test style, naming, and patterns.
- **Minimal scope** — only add test code, do not modify source code (unless
  fixing a minor testability issue like unexported-but-needed helper).
- **Iterate and measure** — re-measure after each batch to avoid overshooting.
- **Stop early** — if target is reached, stop writing tests immediately.
- **Do NOT push** unless the user explicitly asks.
