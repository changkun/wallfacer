# Known bugs / follow-ups

## Workspace isolation: data-layer gating incomplete

The visibility fix (`visibleWorkspaces`/`isAllowedWorkspace`) covers the
browser-facing read surfaces a user can see: spec tree, file list, file
explorer, git status, terminal cwd. Two gaps remain for full defense-in-depth
when an org-stamped workspace is active in a session that cannot see it:

- **Planning data is still served.** The Plan view is hidden at the frontend
  (`meta.needsWorkspace` gate), but `planning.go` handlers still read
  `currentWorkspaces()` directly. The chat history / planning API would return
  the hidden workspace's data if called directly. Frontend-hidden, not
  backend-isolated.
- **Mutation endpoints not gated.** Spec transitions (`specs.go` archive /
  create), dispatch (`specs_dispatch.go`), planning actions, and
  `instructions.go` reinit still use `currentWorkspaces()`. They are only
  reachable from now-hidden UI, but are not isolated at the API layer.

Low priority for local single-user host mode (the caller owns all data); matters
for any genuinely multi-tenant deployment. Route these through
`visibleWorkspaces(ctx)` / a request-scoped principal if backend isolation is
required.
