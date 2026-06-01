import { describe, it, expect } from 'vitest';
import { classifyTag } from './tagBadge';

describe('classifyTag', () => {
  const cats = new Set(['Performance', 'Security']);

  it('renders brainstorm categories as badge-category', () => {
    expect(classifyTag('Performance', cats)).toEqual({
      rawTag: 'Performance', label: 'Performance', cls: 'badge badge-category', styled: false,
    });
  });
  it('is exact-match (case-sensitive) for categories', () => {
    expect(classifyTag('performance', cats).cls).toBe('tag-chip');
  });
  it('renders idea-agent badge', () => {
    expect(classifyTag('idea-agent').cls).toBe('badge badge-idea-agent');
  });
  it('renders priority badge with stripped label', () => {
    expect(classifyTag('priority:high')).toMatchObject({ label: 'high', cls: 'badge badge-priority' });
  });
  it('renders impact badge with prefixed label', () => {
    expect(classifyTag('impact:3')).toMatchObject({ label: 'impact 3', cls: 'badge badge-impact' });
  });
  it('renders spawned-by chip', () => {
    expect(classifyTag('spawned-by:abc').cls).toBe('tag-chip badge-routine-spawn');
  });
  it('falls back to hue-styled chip', () => {
    expect(classifyTag('frontend')).toEqual({ rawTag: 'frontend', label: 'frontend', cls: 'tag-chip', styled: true });
  });
});
