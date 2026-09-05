import { describe, it, expect } from 'vitest';
import { routineCountdown, routineLastFired, routineMinutes } from './routineTime';
import type { Task } from '../api/types';

const base = { id: 'r', kind: 'routine', status: 'backlog', routine_enabled: true } as unknown as Task;
const now = Date.parse('2026-09-06T12:00:00Z');

describe('routineTime', () => {
  it('rounds the interval to minutes', () => {
    expect(routineMinutes({ ...base, routine_interval_seconds: 3600 })).toBe(60);
    expect(routineMinutes({ ...base, routine_interval_seconds: 0 })).toBe(0);
  });
  it('counts down to the next run and names the stopped states', () => {
    expect(routineCountdown({ ...base, routine_next_run: '2026-09-06T13:30:20Z' }, now)).toBe('in 1h 30m');
    expect(routineCountdown({ ...base, routine_next_run: '2026-09-06T12:00:45Z' }, now)).toBe('in 45s');
    expect(routineCountdown({ ...base, routine_next_run: '2026-09-06T11:00:00Z' }, now)).toBe('fired just now');
    expect(routineCountdown({ ...base, routine_enabled: false }, now)).toBe('paused');
    expect(routineCountdown({ ...base, status: 'done' }, now)).toBe('stopped');
    expect(routineCountdown({ ...base, archived: true } as Task, now)).toBe('stopped (archived)');
    expect(routineCountdown({ ...base }, now)).toBe('re-arming...');
  });
  it('describes the last fire relative to now', () => {
    expect(routineLastFired({ ...base, routine_last_fired_at: '2026-09-06T11:58:00Z' }, now)).toBe('fired 2m ago');
    expect(routineLastFired({ ...base, routine_last_fired_at: '2026-09-04T12:00:00Z' }, now)).toBe('fired 2d ago');
    expect(routineLastFired({ ...base }, now)).toBe('');
  });
});
