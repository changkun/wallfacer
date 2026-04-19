---
title: Data Boundary Enforcement
status: drafted
depends_on: []
affects:
  - internal/cloud/
  - internal/store/
  - .github/workflows/
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Data Boundary Enforcement

## Problem

Wallfacer's cloud coordination layer (Phase 2) receives metadata from local instances. The data boundary defines what can leave the user's machine: task IDs, timestamps, token counts, model names, cost estimates, failure categories. It explicitly excludes source code, file contents, diffs, agent output, secrets, env vars, and repo paths.

This boundary is currently a manual contract. One accidental log line that includes code or a file path leaks user data to wallfacer.cloud. For a privacy-focused local-first product, this is the single highest-impact failure mode: silent, hard to detect after the fact, and catastrophic for trust.

## Scope

Automated CI-level enforcement of the data boundary. Every telemetry payload produced by `internal/cloud/` must conform to a field allowlist or CI fails.

## Design

### Allowlist definition

`internal/cloud/boundary.go` exports an `AllowedField` enum listing every field permitted in a telemetry event:
- `task_id`
- `task_status`
- `task_started_at`
- `task_completed_at`
- `task_duration_ms`
- `model_name`
- `sandbox_type`
- `input_tokens`
- `output_tokens`
- `cache_tokens`
- `cost_usd`
- `event_type` (one of: state_change, error, system)
- `event_timestamp`
- `failure_category` (one of: timeout, budget_exceeded, worktree_setup, container_crash, agent_error, sync_error, unknown)
- `spec_title` (title only, not body)
- `dependency_graph` (edge list, node IDs only)

### Enforcement

1. `TelemetryEvent` struct has explicit fields matching the allowlist. No `map[string]any` or free-form payload.
2. JSON marshaling uses struct tags. Extra fields are impossible to accidentally include.
3. A CI test (`internal/cloud/boundary_test.go`) runs on every PR:
   - Generates a sample telemetry payload with all fields populated.
   - Asserts the JSON output contains exactly the allowlist field names.
   - Fails CI with a loud error if any extra field is found.
4. A second test scans `internal/cloud/*.go` for forbidden patterns: `json.Marshal(task.Data)`, `json.Marshal(spec.Body)`, anything that might serialize arbitrary content. Fails CI on match.

### Red-team test

Add a test that attempts to smuggle forbidden data (e.g., source code in a spec title field). The marshaler should truncate `spec_title` to a max length (e.g., 200 chars) to prevent using it as a leak channel.

## Implementation

- `internal/cloud/boundary.go` — allowlist + struct definitions
- `internal/cloud/boundary_test.go` — enforcement tests
- CI workflow file — ensure these tests run on every PR
- Documentation in `docs/internals/data-and-storage.md` about the data boundary

## Test Plan

- Unit: verify `TelemetryEvent` marshals exactly to allowlist fields
- Unit: verify attempting to add a non-allowlisted field causes compile error
- Integration: run a full Phase 2 cloud send flow, capture the HTTP body, assert conforms to allowlist
- Red-team: attempt to smuggle code via spec title, verify truncation

## Success

- CI blocks any PR that introduces a new field in telemetry without updating the allowlist
- Every telemetry payload sent to cloud is a documented, limited field set
- Changkun can point to this spec when users ask "what leaves my machine?"
