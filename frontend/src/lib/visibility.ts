// Decides whether a tab-refocus should trigger a task refetch. When the tab
// returns to the foreground we pull the latest task list so SSE events missed
// while hidden (or a stale connection) are picked up immediately. Mirrors the
// legacy ui/js/api.js visibilitychange fallback.

export function shouldRefetchOnVisible(
  visibilityState: DocumentVisibilityState,
  hasWorkspaces: boolean,
): boolean {
  return visibilityState === 'visible' && hasWorkspaces;
}
