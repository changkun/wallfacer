---
name: bugfix
description: Fix bugs documented in BUGS.md. For each bug, write a regression test that fails without the fix, apply the minimal fix, verify the test passes, and mark the bug as fixed in BUGS.md.
argument-hint: "[bug-filter...]"
user-invocable: true
---

# Bugfix

Fix bugs listed in `BUGS.md`. If `$ARGUMENTS` is provided, only fix bugs
matching the given package or file names. Otherwise, fix all bugs.

## Step 0: Read BUGS.md

1. Read `BUGS.md` at the repo root.
2. Parse each bug entry: file path, line range, description.
3. If `$ARGUMENTS` filters to specific packages/files, select only matching bugs.
4. Skip any bugs already marked as fixed (checkbox `[x]`).

## Step 1: For each bug

### 1a. Understand the bug

- Read the affected file(s) and surrounding context.
- Read existing tests for the affected package.
- Confirm the bug is real by analyzing the code path described.

### 1b. Write a regression test

- Add a test that **reproduces the bug** — it must exercise the exact code path
  described in the bug entry.
- The test should **fail** (panic, wrong result, etc.) without the fix and
  **pass** after the fix.
- Follow existing test patterns in the package.
- Name the test descriptively: `TestFoo_BugDescription`.

### 1c. Verify the test fails

- Run the test to confirm it fails or panics without the fix.
- If the test passes without the fix, the bug may already be fixed or the test
  doesn't cover the right path — investigate and adjust.

### 1d. Apply the minimal fix

- Make the smallest change that fixes the bug.
- Do not refactor surrounding code.
- Do not change behavior for non-buggy code paths.

### 1e. Verify the fix

1. Run the specific test to confirm it passes.
2. Run `go test ./...` for the affected package to confirm no regressions.
3. Run `go vet ./...` as a sanity check.

### 1f. Document root cause and lessons learned

For each bug, add to the BUGS.md entry:
- **Root cause** — the underlying design or implementation issue that caused the
  bug (not just the symptom). e.g., "Two constructors produce the same type but
  with different field invariants; the method assumed one constructor's invariant."
- **Lesson learned** — a high-level takeaway applicable beyond this specific bug.
  e.g., "When a type has multiple constructors with different post-conditions,
  methods must handle all valid states."

### 1g. Mark the bug as fixed in BUGS.md

Update the bug entry to indicate it has been fixed:
- Change `- **file.go:123**` to `- [x] **file.go:123**`
- Append the root cause and lesson inline: `**Root cause:** ... **Lesson:** ...`

## Step 2: Final verification

1. Run `make fmt` and `make lint` — fix any issues.
2. Run `go test ./...` to confirm all tests pass.
3. Run `go build ./...` to confirm clean build.

## Step 3: Commit

Create one commit per bug (or one combined commit if fixing multiple related
bugs in the same package):

```
<package>: fix <brief description of bug>
```

Do NOT push unless the user explicitly asks.

## Step 4: Summary

Report:
- Number of bugs fixed
- For each: the test added, the fix applied, before/after behavior
- For each: root cause and lesson learned (high-level takeaway)
- Any bugs skipped and why

## Guidelines

- **Regression test first** — every fix must have a test that fails without it.
- **Minimal fix** — smallest change that corrects the behavior.
- **Read before writing** — never modify a file you haven't read.
- **One bug at a time** — fix, test, verify, then move to the next.
