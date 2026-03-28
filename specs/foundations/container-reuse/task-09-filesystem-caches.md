# Task 9: Named Volume Caches for Dependencies (Optional)

**Status:** Done
**Depends on:** Task 3
**Phase:** 4 (Filesystem Reuse)
**Effort:** Medium

## Goal

Add named volume mounts for dependency caches so warm caches persist
across worker lifetimes (e.g., server restart).

## What to do

1. Identify common dependency cache directories:
   - `~/.npm` (Node.js)
   - `~/.cache/pip` (Python)
   - `~/.cargo/registry` (Rust)
   - `~/.cache/go-build` (Go)

2. Add named volumes to the container spec for these directories.
   Use a per-workspace-key volume name so different workspace groups
   don't share caches (e.g., `wallfacer-cache-npm-<key>`).

3. Make this configurable — some users may not want persistent caches
   (e.g., reproducibility concerns).

4. Measure cold vs warm task execution times to quantify the benefit.

## Tests

- `TestCacheVolumeMountsPresent` — verify named volumes appear in
  container create args when enabled.
- `TestCacheVolumesNotPresentWhenDisabled` — verify no cache volumes
  when feature is off.

## Boundaries

- This is an optional follow-up, not required for the core worker feature.
- Do NOT change the sandbox image (caches use standard paths).
