import { describe, it, expect } from 'vitest';
import { filterTemplates } from './templateFilter';
import type { PromptTemplate } from '../api/types';

const tmpls: PromptTemplate[] = [
  { id: '1', name: 'Bug fix', body: 'Reproduce then patch' },
  { id: '2', name: 'Refactor', body: 'Extract the helper' },
];

describe('filterTemplates', () => {
  it('returns all on empty query', () => {
    expect(filterTemplates(tmpls, '')).toHaveLength(2);
  });
  it('matches the name (case-insensitive)', () => {
    expect(filterTemplates(tmpls, 'BUG').map((t) => t.id)).toEqual(['1']);
  });
  it('matches the body', () => {
    expect(filterTemplates(tmpls, 'helper').map((t) => t.id)).toEqual(['2']);
  });
  it('returns [] when nothing matches', () => {
    expect(filterTemplates(tmpls, 'zzz')).toHaveLength(0);
  });
});
