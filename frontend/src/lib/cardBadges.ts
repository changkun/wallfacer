// Pure card-badge logic ported from ui/js/render.js. Kept testable and out
// of the SFC. Covers the dependency-state badge (blocked / ready /
// dependency-cancelled) and the friendly failure-category label.

export interface DepTask { id: string; title?: string; prompt?: string; status?: string }

export type DepBadgeKind = 'cancelled' | 'blocked' | 'ready';

export interface DepBadge {
  kind: DepBadgeKind;
  count: number;       // number of declared dependencies
  blocking: string;    // comma-joined blocking task names (for the tooltip)
}

function depName(dep: DepTask | undefined, id: string): string {
  if (!dep) return id.slice(0, 8) + '…';
  if (dep.title) return dep.title;
  const p = dep.prompt ?? '';
  return p.length > 30 ? p.slice(0, 30) + '…' : p;
}

/** Dependency badge for a backlog task. Returns null when the task is not
 *  backlog or declares no dependencies. Mirrors render.js renderDependencyBadge:
 *  - any dependency missing or cancelled → "cancelled"
 *  - else any dependency not yet done    → "blocked" (with blocking names)
 *  - else                                → "ready" */
export function dependencyBadge(
  task: { status?: string; depends_on?: string[] },
  byId: Map<string, DepTask>,
): DepBadge | null {
  if (task.status !== 'backlog') return null;
  const depIds = task.depends_on ?? [];
  if (depIds.length === 0) return null;

  const cancelledOrMissing = depIds.some((id) => {
    const dep = byId.get(id);
    return !dep || dep.status === 'cancelled';
  });
  if (cancelledOrMissing) return { kind: 'cancelled', count: depIds.length, blocking: '' };

  const unmet = depIds.filter((id) => {
    const dep = byId.get(id);
    return !dep || (dep.status !== 'done' && dep.status !== 'cancelled');
  });
  if (unmet.length > 0) {
    const blocking = unmet.map((id) => depName(byId.get(id), id)).join(', ');
    return { kind: 'blocked', count: depIds.length, blocking };
  }
  return { kind: 'ready', count: depIds.length, blocking: '' };
}

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
