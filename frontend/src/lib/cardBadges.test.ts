import { describe, it, expect } from 'vitest';
import { dependencyBadge, failureLabel, type DepTask } from './cardBadges';

function idx(...tasks: DepTask[]): Map<string, DepTask> {
  return new Map(tasks.map((t) => [t.id, t]));
}

describe('dependencyBadge', () => {
  it('returns null for non-backlog or no deps', () => {
    expect(dependencyBadge({ status: 'in_progress', depends_on: ['a'] }, idx())).toBeNull();
    expect(dependencyBadge({ status: 'backlog', depends_on: [] }, idx())).toBeNull();
    expect(dependencyBadge({ status: 'backlog' }, idx())).toBeNull();
  });

  it('ready when all deps done', () => {
    const b = dependencyBadge(
      { status: 'backlog', depends_on: ['a', 'b'] },
      idx({ id: 'a', status: 'done' }, { id: 'b', status: 'done' }),
    );
    expect(b).toEqual({ kind: 'ready', count: 2, blocking: '' });
  });

  it('blocked with names when a dep is unfinished', () => {
    const b = dependencyBadge(
      { status: 'backlog', depends_on: ['a', 'b'] },
      idx({ id: 'a', status: 'done' }, { id: 'b', status: 'in_progress', title: 'Build API' }),
    );
    expect(b?.kind).toBe('blocked');
    expect(b?.count).toBe(2);
    expect(b?.blocking).toBe('Build API');
  });

  it('cancelled when a dep is missing or cancelled (wins over blocked)', () => {
    expect(dependencyBadge(
      { status: 'backlog', depends_on: ['a'] },
      idx(/* a absent */),
    )?.kind).toBe('cancelled');
    expect(dependencyBadge(
      { status: 'backlog', depends_on: ['a', 'b'] },
      idx({ id: 'a', status: 'cancelled' }, { id: 'b', status: 'in_progress' }),
    )?.kind).toBe('cancelled');
  });

  it('blocking name falls back to prompt snippet then id', () => {
    const b = dependencyBadge(
      { status: 'backlog', depends_on: ['longid12345'] },
      idx({ id: 'longid12345', status: 'backlog', prompt: 'a'.repeat(40) }),
    );
    expect(b?.blocking).toBe('a'.repeat(30) + '…');
  });
});

describe('failureLabel', () => {
  it('maps known categories', () => {
    expect(failureLabel('timeout')).toBe('Timeout');
    expect(failureLabel('budget_exceeded')).toBe('Budget');
    expect(failureLabel('container_crash')).toBe('Crash');
  });
  it('empty for unknown/missing', () => {
    expect(failureLabel(undefined)).toBe('');
    expect(failureLabel('unknown')).toBe('');
  });
  it('passes through an unmapped category verbatim', () => {
    expect(failureLabel('weird_new_cat')).toBe('weird_new_cat');
  });
});
