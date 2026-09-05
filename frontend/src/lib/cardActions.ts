// Which quick-action buttons a task card shows, by status. Single source of
// truth ported from ui/js/render.js's per-status footer button logic, so the
// matrix is unit-testable instead of buried in template v-ifs.

import type { Task } from '../api/types';

export type CardAction = 'plan' | 'start' | 'resume' | 'test' | 'done' | 'retry' | 'sync';

export interface CardActionDef {
  id: CardAction;
  label: string;
  icon: string;
  title: string;
}

export const CARD_ACTION_DEFS: Record<CardAction, CardActionDef> = {
  plan: { id: 'plan', label: 'Plan', icon: '✎', title: 'Send to Plan' },
  start: { id: 'start', label: 'Start', icon: '▶', title: 'Move to In Progress' },
  resume: { id: 'resume', label: 'Resume', icon: '↻', title: 'Resume in existing session' },
  test: { id: 'test', label: 'Test', icon: '▶', title: 'Run test agent' },
  done: { id: 'done', label: 'Done', icon: '✓', title: 'Mark done and commit' },
  retry: { id: 'retry', label: 'Retry', icon: '↩', title: 'Move back to Backlog' },
  sync: { id: 'sync', label: 'Sync with default', icon: '⟳', title: 'Rebase worktrees onto the default branch' },
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

// The one action that moves the task forward from its column; the card renders
// it as the ink button and everything else as a ghost. Waiting moves forward
// by Done, failed by Resume when a session exists (Retry otherwise), backlog by
// Start, and a finished task can only be reopened.
export function primaryCardAction(task: Pick<Task, 'status' | 'archived' | 'kind' | 'session_id'>): CardAction | null {
  const actions = cardActionsFor(task);
  if (actions.length === 0) return null;
  switch (task.status) {
    case 'backlog': return 'start';
    case 'waiting': return 'done';
    case 'failed': return actions.includes('resume') ? 'resume' : 'retry';
    default: return actions[0];
  }
}

// Command-palette action list: the card actions plus a "Sync with default"
// action for waiting/failed tasks (the board surfaces sync via the behind
// chip, so it's palette-only here). Mirrors ui/js/command-palette.js.
export function commandPaletteActionsFor(
  task: Pick<Task, 'status' | 'archived' | 'kind' | 'session_id'>,
): CardAction[] {
  const base = cardActionsFor(task);
  if (task.status === 'waiting' || task.status === 'failed') {
    return [...base, 'sync'];
  }
  return base;
}
