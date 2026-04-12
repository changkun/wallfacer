---
title: "Chat-edit fan-out"
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-state-control-plane/propagation-algorithm.md
affects:
  - internal/handler/planning_git.go
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
effort: small
---

# Chat-edit Fan-out

When a planning chat round commits edits under `specs/`, every live
dependent of a modified spec transitions to `stale`. The author clears
the staleness with `/wf-spec-refine`.

---

## Trigger

`commitPlanningRound` in `internal/handler/planning_git.go`. The commit
itself already runs; fan-out is inserted between "stage specs/" and
"create final commit."

## Action

1. Compute the set of modified spec paths from the staged diff:
   `git diff --cached --name-only -- specs/`.
2. For each modified spec, run the propagation algorithm's two channels:
   - `DependsOnImpact(tree, sourcePath)` — reverse dependency traversal
   - `AffectsImpactFromSpec(tree, sourcePath)` — affects overlap using
     source's declared affects (no code diff available — the commit
     touches specs/ only)
3. Union the impact sets across all modified specs; exclude any spec
   already in the modified set (authors edited it themselves).
4. `FanOutStale(tree, impacted)` transitions each to `stale`.
5. The stale writes are staged and included in the **same** planning
   commit, so `git revert` of the planning round reverses the cascade
   in one step.

See [propagation-algorithm.md](propagation-algorithm.md) for channel
mechanics and archive exclusions.

---

## Skip heuristic: frontmatter-only bumps

A chat round that only writes `updated: <today>` (no body or semantic
frontmatter changes) should not fan out. Heuristic:

1. For each modified spec, compute the pre/post diff restricted to the
   body and to non-`updated` frontmatter fields.
2. If the restricted diff is empty, skip fan-out for that spec.

Implementation: parse each side, compare the resulting `Spec` struct
with `Updated` zeroed on both. If equal, skip.

---

## Rationale for the blunt rule

"Mark every live dependent stale" is loud. The alternative — run
semantic drift assessment on each dependent, same tester agent as the
drift pipeline — is more precise but expensive and coupled. Ship the
blunt rule first; revisit after the drift pipeline lands and the
tester is known-good. At that point a dependent-level pass becomes
cheap to graft on.

---

## Commit integration

Today's `commitPlanningRound`:

```
1. git add specs/
2. compute commit message
3. git commit
```

After this change:

```
1. git add specs/  (staged edits)
2. detect modified specs from the staged diff
3. run fan-out algorithm; stage the resulting frontmatter writes
4. git add specs/  (re-stage to include fan-out writes)
5. compute commit message (now covers edits + fan-out)
6. git commit
```

Keeps everything inside one commit, which is the whole point. The
commit message template can list impacted specs as a bullet section.

---

## Acceptance

- A planning round that modifies `A.md`, where `B.md` and `C.md`
  depend on `A.md`, commits with both `A` edits and `B`, `C` frontmatter
  `status: stale` writes in the same commit.
- Archived specs are never impacted.
- A round that only bumps `updated` on a spec produces no fan-out.
- Unit test: plan a round modifying `A.md`; assert impacted set equals
  reverse-deps ∪ affects-overlap; assert archived are excluded;
  assert `updated`-only bump skips.
- Integration test: commit a planning round on a spec with live
  dependents; assert the commit contains all expected frontmatter
  mutations.

---

## Open Questions

1. **Multi-spec rounds.** A planning round often modifies several specs
   in one commit. Should fan-out compute impact as a union (once, over
   the whole modified set) or per-spec (then union the results)? They
   produce the same set modulo self-exclusion; prefer the "per-spec
   then union" order so we can skip `updated`-only bumps per file.
2. **Semantic filter on the "blunt rule."** Some chat edits are clearly
   additive (e.g., fixing a typo). Automated additive-detection is
   unreliable at the diff level. Defer until the drift pipeline's
   tester is known to give good signal.
