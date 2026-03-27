---
name: check-ci
description: Check GitHub Actions CI results for the current branch. Shows job statuses, failed test output, and lint errors. If failures are found, automatically diagnoses and fixes the issues, then commits and pushes.
argument-hint: [run-id]
allowed-tools: Bash(gh *), Read, Grep, Glob, Edit, Write
---

# Check GitHub Actions CI Results

Inspect the latest (or specified) GitHub Actions run for the current repository,
report job statuses, and **automatically fix any failures found**.

## Step 1: Identify the run

If $ARGUMENTS contains a run ID, use that. Otherwise, find the latest run for
the current branch:

```
gh run list --branch $(git branch --show-current) --limit 1
```

## Step 2: Show job-level summary

```
gh run view <run-id> --json jobs --jq '.jobs[] | {name, status, conclusion}'
```

Report each job as PASS / FAIL / IN PROGRESS.

## Step 3: Get failed logs

If any job failed:

```
gh run view <run-id> --log-failed
```

Save the output to a temp file and search it for:
- Lines matching `--- FAIL` (test failures)
- Lines matching `##[error]` (lint / build errors)
- Lines matching `FAIL\t` (package-level failures)

## Step 4: Report

For each failure found, report:
1. **Job name** (e.g., test-windows, test-linux)
2. **Failure type** (test failure, lint error, build error)
3. **Details** — the test name, error message, or lint diagnostic
4. If the run is still in progress, say so and suggest checking back later.

Keep the report concise. Group failures by job. If all jobs passed, just say
"CI passed" and stop here.

## Step 5: Fix failures

If there are failures, fix them:

1. **Diagnose** — Read the failing source files and tests identified in Step 4.
   Understand the root cause of each failure (broken test logic, lint violation,
   build error, type mismatch, etc.).

2. **Fix** — Edit the source files to resolve each issue. Prefer minimal,
   targeted fixes. Do not refactor unrelated code.

3. **Verify locally** — Run the same checks that failed in CI locally to confirm
   the fixes work:
   - Test failures → run the specific failing tests (e.g., `go test ./path/to/...` or the relevant test command)
   - Lint errors → run the linter (e.g., `make lint` or the project's lint command)
   - Build errors → run the build (e.g., `make build` or `go build ./...`)

4. **Commit and push** — If local verification passes, create a focused commit
   with a message like `fix: resolve CI failures in <job-name>` and push to the
   current branch. Follow the project's commit conventions.

5. **Report** — Summarize what was fixed and that the push was made. Suggest
   the user run `/check-ci` again after the new run completes to verify.
