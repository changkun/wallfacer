import { describe, it, expect } from 'vitest';
import { rankDocs } from './docSearch';

const docs = [
  { slug: 'configuration', title: 'Configuration' },
  { slug: 'board-and-tasks', title: 'Board and Tasks' },
  { slug: 'agents-and-flows', title: 'Agents and Flows' },
  { slug: 'getting-started', title: 'Getting Started' },
];

describe('rankDocs', () => {
  it('returns original order (capped) for an empty query', () => {
    expect(rankDocs(docs, '').map((d) => d.slug)).toEqual([
      'configuration', 'board-and-tasks', 'agents-and-flows', 'getting-started',
    ]);
  });
  it('ranks title-prefix above title-substring', () => {
    const r = rankDocs(docs, 'a');
    // "Agents and Flows" starts with "a" (3); "Board and Tasks" / "Configuration"
    // contain "a" (2). Prefix wins.
    expect(r[0].slug).toBe('agents-and-flows');
  });
  it('matches slug when title does not', () => {
    const r = rankDocs(docs, 'started');
    expect(r.map((d) => d.slug)).toContain('getting-started');
  });
  it('excludes non-matches', () => {
    expect(rankDocs(docs, 'zzz')).toHaveLength(0);
  });
  it('breaks ties alphabetically by title', () => {
    const r = rankDocs(docs, 'and'); // both "Board and Tasks" and "Agents and Flows" score 2
    expect(r[0].title).toBe('Agents and Flows');
  });
  it('respects the limit', () => {
    expect(rankDocs(docs, '', 2)).toHaveLength(2);
  });
});
