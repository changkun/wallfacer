---
name: wf-spec-wrapup
description: Finalize a completed spec — works for both a dispatched spec (verify all child tasks are done, synthesize from task notes) and a directly-implemented spec (no task files; reconstruct from git history of the affects files). Updates the spec with an Outcome section, flips status to complete, updates specs/README.md, and commits. Use when a spec's work is done and it needs its completion write-up.
argument-hint: <spec-file>
user-invocable: true
---

# Wrap Up a Completed Spec

Finalize the spec at `$ARGUMENTS` after all implementation tasks are done.

## Step 1: Verify completion

1. Read the spec file. **Parse YAML frontmatter** to extract `title`, `status`,
   `depends_on`, `affects`, `effort`, `created`, `updated`,
   `dispatched_task_id`.
2. **Detect the lifecycle path** — a spec reaches "done" two ways, and the
   Outcome source differs:
   - **Dispatched path** — a child spec directory exists (sibling directory with
     the same name as the spec file without `.md`), and/or leaves carry a
     `dispatched_task_id`. Execution happened elsewhere; the record lives in task
     files.
   - **Direct-implement path** — no child directory and `dispatched_task_id` is
     `null`. The spec was implemented in-session (often by `/wf-spec-implement`,
     or by hand). There are no task files; the record is the git history of the
     `affects` files plus any in-session knowledge handed to you.

   Pick the path from what actually exists on disk, not from how you were
   invoked. Both paths run the rest of this skill; only the Outcome *source*
   (Step 2b) differs.
3. **Dispatched path only:** read all child spec files and parse their
   frontmatter. Verify every leaf spec in the subtree has `status: complete`
   (non-leaf children are complete when all their own leaves are). If any leaf is
   not `complete`, report them and stop — do not proceed with wrap-up.
   **Direct-implement path:** confirm the spec body's items are actually present
   in the code (spot-check the `affects` files / recent commits). If the
   implementation is clearly incomplete, report what is missing and stop.
4. Run `make test` to confirm all tests pass. If tests fail, stop. (If you just
   ran the equivalent full suites in-session and they were green, say so and you
   may skip the re-run.)

## Step 2: Update the parent spec

Read the parent spec file and update it to match the completed-spec style:

### 2a. Run the divergence analysis (the shared engine)

Before writing the Outcome, run the divergence analysis defined by
`/wf-spec-diff` (its Steps 2–5) as wrap-up's analysis engine — do not eyeball the
diff. This is the single place that classification logic lives; wrap-up composes
the Outcome from it, so the two skills never write the section twice.

1. Determine the commit range:
   - **Dispatched path** — the commits associated with the task UUID (per diff
     Step 2).
   - **Direct-implement path** — the commits on the `affects` files since
     `created:`: `git log --oneline -- <affects files>`.
2. Run diff Steps 3–5: classify each spec item (Satisfied / Diverged / Not
   implemented / Superseded), identify unspecified work (scaffolding /
   improvement / scope creep), and compute the **drift level** (Minimal /
   Moderate / Significant) and satisfaction rate.

If you are running `/wf-spec-diff` standalone (not via wrap-up), it writes the
Outcome itself; here, wrap-up owns the write (2b).

### 2b. Write the single Outcome section

Insert one `## Outcome` section before any "Future Work" or "Phase N (Future)"
sections. This is the canonical completion record — both `/wf-spec-implement`
(which delegates here) and `/wf-spec-diff` (whose analysis 2a just consumed) feed
this one section, replacing the older split between "Outcome", per-task
"Implementation notes", and diff's separate drift report. It contains:

1. **Summary** — 2-3 sentences: what shipped, dispatched vs implemented directly,
   commit SHAs (or PR link), and the **drift / satisfaction line** from 2a
   (e.g. "Drift: Minimal — N/M items satisfied (X%)").
2. **What Shipped** — key deliverables: API endpoints and location (or "no new
   endpoints" if reusing routes), frontend components/stores/libs and approximate
   size, number of tests (backend + frontend) and coverage, key features.
3. **Design Evolution** — the **Diverged** and **Superseded** items from 2a:
   what the spec said vs. what was built, why, and the commit. If a deviation
   made the spec body wrong, fix the body inline too.
4. **Not Implemented** — the **Not implemented** items from 2a: each with why
   (deferred / descoped / superseded / blocked) and whether a follow-up exists.
5. **Unspecified Work** — the unspecified changes from 2a, each tagged
   scaffolding / improvement / scope creep.
6. **Decisions / surprises / follow-ups** — judgment calls not in the spec
   (naming, defaults, schema, UX micro-details, test strategy) with reasoning;
   gotchas a future maintainer or dependent spec should know (hidden coupling,
   fragile assumptions, test-infra quirks); concrete follow-ups not done here
   (link new specs/issues, or "None"). Highest-value detail — omit a bullet only
   if genuinely empty. On the dispatched path, mine the task files'
   `## Implementation notes` for this; on the direct path, use any in-session
   knowledge handed to you plus the commit diffs.

### 2c. Transition status through the `testing` gate (drift-aware)

The lifecycle (`internal/spec/lifecycle.go`) forbids `validated → complete`
directly: a spec reaches `complete` only via `testing`, where the drift verdict is
rendered. The drift analysis from 2a **is** that verdict. Honor the gate; never
hand-write `complete` onto a `validated` spec. Always set `updated: <today>` and
keep `dispatched_task_id` `null` for non-leaf specs.

Drive the status by the path detected in Step 1, preferring the server transition
API (it validates the edge and runs stale fan-out); fall back to legal-edge YAML
only when the server is unreachable:

- **Dispatched path** — the **server already owns** `validated → testing →
  complete/stale` (the task-done drift pipeline; or, with `WALLFACER_DRIFT_TESTER`
  off, an unconditional `complete`). Do **not** re-set the status. Read it:
  - Already `complete`/`stale` → leave it; you are only enriching the Outcome.
  - Stuck in `testing` (verdict pending / tester failed) → use the
    `force-complete` action (it's `testing → complete`) only if your analysis says
    minimal/moderate drift; on significant drift use the `stale` action. Both are
    gates — confirm with the user before overriding the server.
- **Direct-implement path** — no task ran, so no server hook fired; the spec is
  still `validated`. Walk the legal edges yourself, using the drift level from 2a:
  - **Minimal / Moderate drift** → `validated → testing → complete`. There is no
    API action to enter `testing` from `validated`, so write `status: testing`
    (legal) as a YAML edit, then complete the `testing → complete` leg via the
    `force-complete` action when the server is reachable, else by writing
    `status: complete` (legal from `testing`). For Moderate, the Outcome documents
    the divergences; suggest `/wf-spec-refine` to align the body.
  - **Significant drift** → `validated → stale` (legal directly). Do **not**
    complete it; the spec no longer describes what was built. Report this and
    recommend `/wf-spec-refine`, then re-dispatch the remaining work.

### 2d. Update File Inventory

If the spec has a File Inventory section, verify it matches the actual files that were created/modified. Update any discrepancies.

## Step 3: Update `specs/README.md`

1. Read `specs/README.md`.
2. Change the status in the ASCII art tree (e.g., `◐  M4: File Explorer (N/M)` → `✅ M4: File Explorer`).
3. Change the status in the Milestones table (e.g., `**In progress** (N/M tasks done)` → `**Complete**`).
4. Update the delivers column if the implementation differs from what was originally described.

## Step 4: Check downstream specs (reverse dependency analysis)

Scan all spec files for `depends_on` entries that reference this spec's path:
1. **Reverse `depends_on` scan** — grep all spec frontmatter for this spec's
   path in their `depends_on` lists. These are specs that were blocked by this
   one and are now potentially unblocked.
2. If this spec introduced or changed interfaces listed in its `affects`, check
   whether downstream specs reference those same files/packages. Verify their
   descriptions are still accurate.
3. If a `stale` spec depends on this one, flag it — the completion may resolve
   or worsen the staleness.
4. Only make factual corrections — do not redesign other specs.

## Step 5: Commit

Stage all modified spec files and commit:
```
specs: mark <spec-name> as complete, update README
```

Do NOT push unless the user explicitly asks.

## Step 6: Report

Tell the user:
- The spec is marked complete
- How many tasks were verified
- Any downstream specs that were updated
- Whether any issues were found during verification

## Guidelines

- **Read before writing** — read the spec and all task files before making changes.
- **Preserve UX design** — the spec may contain detailed UX descriptions, wireframes, and user interaction flows. These are valuable documentation even after implementation. Do not remove or summarize them.
- **Preserve future work** — Phase 3, Phase 4, and "Future Work" sections describe planned extensions. Keep them intact.
- **Match the style** of other completed specs in the repo (e.g., `01-sandbox-backends.md`).
- **Be factual** — the Outcome and Design Evolution sections should document what actually happened, not what was planned. Read task implementation notes for accuracy.
