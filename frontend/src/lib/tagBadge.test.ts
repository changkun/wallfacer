import { describe, it, expect } from 'vitest';
import { classifyTag } from './tagBadge';

describe('classifyTag', () => {
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
