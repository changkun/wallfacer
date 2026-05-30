import { describe, it, expect } from 'vitest';
import { sortBacklog } from './backlogSort';

const t = (id: string, impact_score?: number) => ({ id, impact_score });

describe('sortBacklog', () => {
  it('manual mode returns the list unchanged', () => {
    const list = [t('a', 1), t('b', 9)];
    expect(sortBacklog(list, 'manual')).toBe(list);
  });

  it('impact mode sorts by descending impact_score', () => {
    const r = sortBacklog([t('a', 1), t('b', 9), t('c', 5)], 'impact');
    expect(r.map(x => x.id)).toEqual(['b', 'c', 'a']);
  });

  it('tasks without a score sink to the bottom, original order preserved among them', () => {
    const r = sortBacklog([t('a'), t('b', 3), t('c'), t('d', 7)], 'impact');
    expect(r.map(x => x.id)).toEqual(['d', 'b', 'a', 'c']);
  });

  it('is stable for equal scores', () => {
    const r = sortBacklog([t('a', 5), t('b', 5), t('c', 5)], 'impact');
    expect(r.map(x => x.id)).toEqual(['a', 'b', 'c']);
  });
});
