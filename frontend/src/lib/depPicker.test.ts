import { describe, it, expect } from 'vitest';
import { dependencyCandidates, filterCandidates } from './depPicker';
import type { Task } from '../api/types';

function mk(id: string, status: string, title = '', prompt = ''): Task {
  return { id, status, title, prompt } as unknown as Task;
}

describe('dependencyCandidates', () => {
  const tasks = [
    mk('a', 'done', 'Alpha'),
    mk('b', 'in_progress', 'Bravo'),
    mk('c', 'backlog', 'Charlie'),
    mk('self', 'waiting', 'Self'),
  ];
  it('excludes the given id', () => {
    expect(dependencyCandidates(tasks, 'self').map((c) => c.id)).not.toContain('self');
  });
  it('sorts by status priority (in_progress > waiting > backlog > done)', () => {
    expect(dependencyCandidates(tasks).map((c) => c.id)).toEqual(['b', 'self', 'c', 'a']);
  });
  it('falls back to a truncated prompt for the label', () => {
    const t = [mk('x', 'backlog', '', 'p'.repeat(80))];
    expect(dependencyCandidates(t)[0].label).toBe('p'.repeat(60) + '…');
  });
});

describe('filterCandidates', () => {
  const cands = [
    { id: 'a', label: 'Fix login bug', status: 'backlog' },
    { id: 'b', label: 'Add logout', status: 'backlog' },
  ];
  it('returns all on empty query', () => {
    expect(filterCandidates(cands, '')).toHaveLength(2);
  });
  it('filters by case-insensitive label substring', () => {
    expect(filterCandidates(cands, 'LOGIN').map((c) => c.id)).toEqual(['a']);
  });
});
