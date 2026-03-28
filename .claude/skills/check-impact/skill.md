---
name: check-impact
description: Analyze what existing code and specs a proposed change will affect. Use before implementing a spec to understand blast radius, identify risks, and find specs that need updating.
argument-hint: <spec-file.md>
allowed-tools: Read, Grep, Glob, Agent, Bash(git log *), Bash(ls *)
---

# Check Impact

Analyze the blast radius of a spec before implementation. Identify what existing
code, tests, specs, and documentation will be affected.

## Step 0: Parse arguments

Extract the spec file path from the first token.

## Step 1: Read the spec

1. Read the spec file in full.
2. Extract all mentioned file paths, package names, type names, function names,
   and interface names.
3. Identify the spec's deliverables: what new things it creates, what existing
   things it modifies.

## Step 2: Map the code impact

For each file, type, or function the spec plans to modify:

1. Find all callers and dependents using Grep/Glob:
   - Functions: grep for call sites across the codebase.
   - Types: grep for usage (field access, type assertions, interface
     implementations).
   - Interfaces: grep for implementations and consumers.
   - Packages: grep for imports.
2. Build a dependency fan-out: "changing X affects Y, Z, W".
3. Classify each affected file as:
   - **Direct** — mentioned in the spec, will be intentionally modified.
   - **Ripple** — not mentioned but uses something being changed; may need
     updates.
   - **Test** — test file that exercises affected code; may need updates.

Use Agent subagents (Explore type) for parallel searches across independent
packages. Launch up to 3 concurrently.

## Step 3: Check interface stability

For each interface or exported type the spec modifies:

1. Find all implementations (for interfaces) or embeddings (for structs).
2. Check if any are in other packages or other specs' deliverables.
3. Flag breaking changes: method signature changes, removed fields, renamed
   types.
4. For each breaking change, list every file that would need updating.

## Step 4: Check spec cross-references

1. Grep all other spec files for references to:
   - The spec being analyzed (by filename or milestone number).
   - Types, interfaces, or packages the spec modifies.
2. For each referencing spec, assess whether it:
   - Depends on the current shape of what's being changed (needs update).
   - Merely mentions it in passing (no action needed).
   - Has assumptions that conflict with the proposed changes.

## Step 5: Check documentation impact

Scan documentation files for references to things being changed:

1. `CLAUDE.md` — API routes, env vars, CLI flags, key files list.
2. `docs/guide/*.md` — user-facing documentation.
3. `docs/internals/*.md` — technical documentation.
4. `README.md` at project root if it exists.

Flag any doc sections that reference modified APIs, types, or behaviors.

## Step 6: Check test impact

1. Find test files in affected packages.
2. Grep for test functions that reference modified types or functions.
3. Estimate how many tests will need updating.
4. Flag packages with low test coverage in the affected area (no existing
   tests for the code being changed).

## Step 7: Generate report

```
## Impact Analysis: <spec-name>

### Direct Changes
Files the spec explicitly modifies:
- <file> — <what changes>

### Ripple Effects
Files not in the spec but affected by the changes:
- <file> — uses <thing being changed>, may need: <what>
- <file> — imports <package being changed>, may need: <what>

### Interface Changes
- <interface/type> — <N> implementations, <N> callers
  Breaking: <yes/no>, affected files: <list>

### Cross-Spec Impact
- <other-spec> — references <thing>, needs: <update/no action>

### Documentation Updates Needed
- <doc-file> — mentions <thing being changed>

### Test Impact
- <N> test files in affected packages
- <N> test functions reference modified code
- Packages with no tests in affected area: <list>

### Risk Assessment
- **Blast radius:** <Small (1-3 files) | Medium (4-10) | Large (10+)>
- **Breaking changes:** <None | <list>>
- **Highest risk area:** <description of what's most likely to break>

### Recommendations
- <actions to take before or during implementation>
```

## Notes

- This skill is read-only. It does not modify any files.
- Focus on actionable findings. Don't flag every transitive import — focus on
  code that will actually need changes.
- If the spec has a task breakdown, check impact per-task to identify which
  tasks carry the most risk.
