# Known bugs / follow-ups

## Workspace isolation: data-layer gating

The visibility fix (`visibleWorkspaces`/`isAllowedWorkspace`/`requireVisibleWorkspace`)
covers the browser-facing surfaces: spec tree, file list, file explorer, git
status, terminal cwd, planning reads (thread list, messages, SSE stream),
planning + spec mutations (send/clear/interrupt/start, thread create/patch,
undo, archive/unarchive, dispatch/undispatch), and the stats planning
aggregation. A session that cannot see the active org-stamped workspace gets
empty reads and 403 mutations, matching `/api/config`.

Remaining, intentionally not gated:

- **`env.go` TestSandbox** seeds its probe runner with `currentWorkspaces()`.
  It is a Settings connectivity probe (reached from the always-available
  Settings page), not a workspace-data surface, and gating it would block
  sandbox testing whenever an org workspace is active. Left ungated by design;
  revisit if a probe must never run against a workspace the caller can't see.
- Internal/background callers (`persistPlanningRoundUsage`, group-toggle/limit
  helpers, the spec-completion callback) keep `currentWorkspaces()` — they run
  without a request principal and act on the active group regardless of viewer.
