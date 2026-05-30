// Backlog ordering. Ports ui/js/dnd.js + state.js's backlog sort modes:
// "manual" keeps the user's drag order; "impact" sorts by descending
// impact_score (tasks without a score sink to the bottom, ties keep order).

export type BacklogSortMode = 'manual' | 'impact';

export function sortBacklog<T extends { impact_score?: number }>(tasks: T[], mode: BacklogSortMode): T[] {
  if (mode !== 'impact') return tasks;
  // Stable sort by impact_score desc; missing/NaN scores treated as -Infinity.
  return tasks
    .map((t, i) => ({ t, i }))
    .sort((a, b) => {
      const sa = Number.isFinite(a.t.impact_score) ? (a.t.impact_score as number) : -Infinity;
      const sb = Number.isFinite(b.t.impact_score) ? (b.t.impact_score as number) : -Infinity;
      return sb - sa || a.i - b.i;
    })
    .map((x) => x.t);
}

const KEY = 'wallfacer-backlog-sort-mode';

export function loadBacklogSortMode(): BacklogSortMode {
  try {
    return localStorage.getItem(KEY) === 'impact' ? 'impact' : 'manual';
  } catch {
    return 'manual';
  }
}

export function saveBacklogSortMode(mode: BacklogSortMode): void {
  try { localStorage.setItem(KEY, mode); } catch { /* ignore */ }
}
