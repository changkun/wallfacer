// Which quick-action buttons a task card shows, by status. Single source of
// truth ported from ui/js/render.js's per-status footer button logic, so the
// matrix is unit-testable instead of buried in template v-ifs.

import type { Task } from '../api/types';

export type CardAction = 'plan' | 'start' | 'resume' | 'test' | 'done' | 'retry';

export interface CardActionDef {
  id: CardAction;
  label: string;
  icon: string;
  title: string;
  cls: string;
}

export const CARD_ACTION_DEFS: Record<CardAction, CardActionDef> = {
  plan: { id: 'plan', label: 'Plan', icon: '✎', title: 'Send to Plan', cls: 'card-action-plan' },
  start: { id: 'start', label: 'Start', icon: '▶', title: 'Move to In Progress', cls: 'card-action-start' },
  resume: { id: 'resume', label: 'Resume', icon: '↻', title: 'Resume in existing session', cls: 'card-action-resume' },
  test: { id: 'test', label: 'Test', icon: '▶', title: 'Run test agent', cls: 'card-action-test' },
  done: { id: 'done', label: 'Done', icon: '✓', title: 'Mark done and commit', cls: 'card-action-done' },
  retry: { id: 'retry', label: 'Retry', icon: '↩', title: 'Move back to Backlog', cls: 'card-action-retry' },
};

// Returns the ordered list of quick actions for a task's current status.
// Routine cards and archived cards have no quick actions (routines get their
// own footer; archived cards are read-only).
export function cardActionsFor(task: Pick<Task, 'status' | 'archived' | 'kind' | 'session_id'>): CardAction[] {
  if (task.archived || task.kind === 'routine') return [];
  switch (task.status) {
    case 'backlog':
      return ['plan', 'start'];
    case 'waiting':
      return task.session_id ? ['resume', 'test', 'done'] : ['test', 'done'];
    case 'failed':
      return task.session_id ? ['resume', 'retry'] : ['retry'];
    case 'cancelled':
    case 'done':
      return ['retry'];
    default:
      return [];
  }
}
