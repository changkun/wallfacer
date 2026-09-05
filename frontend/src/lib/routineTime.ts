// Routine schedule wording shared by the board card and the Routines page:
// the interval in minutes, the countdown to the next fire, and how long ago
// the routine last fired. `now` is the shared clock from useNow so every
// caller re-renders on the same tick.
import type { Task } from '../api/types';

const STOPPED = new Set(['cancelled', 'done', 'failed']);

export function routineMinutes(task: Task): number {
  const sec = task.routine_interval_seconds || 0;
  return sec > 0 ? Math.max(1, Math.round(sec / 60)) : 0;
}

export function routineCountdown(task: Task, now: number): string {
  if (task.archived) return 'stopped (archived)';
  if (task.status && STOPPED.has(task.status)) return 'stopped';
  if (!task.routine_enabled) return 'paused';
  const nextRun = task.routine_next_run;
  if (!nextRun) return 're-arming...';
  const next = new Date(nextRun).getTime();
  if (Number.isNaN(next)) return '-';
  const diffMs = next - now;
  if (diffMs <= 0) return 'fired just now';
  const total = Math.floor(diffMs / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `in ${h}h ${m}m`;
  if (m > 0) return `in ${m}m ${s}s`;
  return `in ${s}s`;
}

export function routineLastFired(task: Task, now: number): string {
  const iso = task.routine_last_fired_at;
  if (!iso) return '';
  const fired = new Date(iso).getTime();
  if (Number.isNaN(fired)) return '';
  const diffMs = now - fired;
  if (diffMs < 0) return '';
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return `fired ${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `fired ${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `fired ${hr}h ago`;
  return `fired ${Math.floor(hr / 24)}d ago`;
}
