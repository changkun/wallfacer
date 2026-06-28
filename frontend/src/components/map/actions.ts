import type { GraphAction } from '../../api/types';

// User-facing labels for the transition verbs the backend marks available.
export const ACTION_LABELS: Record<GraphAction, string> = {
  dispatch: 'Dispatch',
  undispatch: 'Undispatch',
  validate: 'Validate',
  'force-complete': 'Force complete',
  unstale: 'Un-stale',
  unarchive: 'Unarchive',
  start: 'Start',
};

// Actions that move a node *forward* through the pipeline. Only these flag a
// node as "ready to act" (accent ring + the inspector's Ready-to-act list), so
// that highlight keeps its signal. Recovery/reversal verbs (undispatch,
// unstale, unarchive, force-complete) stay available in the node menu but don't
// add highlight noise — on a real repo full of stale specs they otherwise swamp
// the list.
const PRIMARY_ACTIONS = new Set<GraphAction>(['validate', 'dispatch', 'start']);

export function hasPrimaryAction(actions?: GraphAction[]): boolean {
  return (actions ?? []).some((a) => PRIMARY_ACTIONS.has(a));
}

// primaryActions keeps only the forward verbs, for the Ready-to-act chips.
export function primaryActions(actions?: GraphAction[]): GraphAction[] {
  return (actions ?? []).filter((a) => PRIMARY_ACTIONS.has(a));
}
