import type { Task } from '../api/types';

export interface DepCandidate {
  id: string;
  label: string;
  status: string;
}

// Status display order in the dependency picker (mirrors ui/js/tasks.js
// populateDependsOnPicker): active work first, finished last, unknown after.
const STATUS_PRIORITY: Record<string, number> = {
  in_progress: 0,
  waiting: 1,
  backlog: 2,
  done: 3,
};

function candidateLabel(t: Pick<Task, 'title' | 'prompt'>): string {
  if (t.title) return t.title;
  const p = t.prompt || '';
  return p.length > 60 ? p.slice(0, 60) + '…' : p;
}

// Build the picker candidate list: every task except excludeId, sorted by
// status priority (stable within a priority bucket).
export function dependencyCandidates(
  tasks: readonly Task[],
  excludeId?: string,
): DepCandidate[] {
  return tasks
    .filter((t) => t.id !== excludeId)
    .map((t, i) => ({ t, i }))
    .sort((a, b) => {
      const pa = STATUS_PRIORITY[a.t.status] ?? 4;
      const pb = STATUS_PRIORITY[b.t.status] ?? 4;
      return pa - pb || a.i - b.i;
    })
    .map(({ t }) => ({ id: t.id, label: candidateLabel(t), status: t.status }));
}

// Case-insensitive label substring filter.
export function filterCandidates(candidates: readonly DepCandidate[], query: string): DepCandidate[] {
  const q = (query || '').trim().toLowerCase();
  if (!q) return candidates.slice();
  return candidates.filter((c) => c.label.toLowerCase().includes(q));
}
