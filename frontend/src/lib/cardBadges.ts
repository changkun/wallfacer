// Pure card-badge logic ported from ui/js/render.js. Kept testable and out
// of the SFC. Covers the friendly failure-category label.

const FAILURE_LABELS: Record<string, string> = {
  timeout: 'Timeout',
  budget_exceeded: 'Budget',
  container_crash: 'Crash',
  agent_error: 'Agent Error',
  worktree_setup: 'Worktree',
  sync_error: 'Sync',
  unknown: '',
};

/** Friendly label for a failure_category (empty string → no badge). */
export function failureLabel(category: string | undefined): string {
  if (!category) return '';
  return FAILURE_LABELS[category] ?? category;
}
