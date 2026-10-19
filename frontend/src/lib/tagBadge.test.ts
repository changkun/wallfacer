import { describe, it, expect } from 'vitest';
import { classifyTag, orderTags } from './tagBadge';

describe('classifyTag', () => {
  it('strips the priority prefix and colors only high and critical', () => {
    expect(classifyTag('priority:high')).toMatchObject({ kind: 'priority', label: 'high', tone: 'warn' });
    expect(classifyTag('priority:critical')).toMatchObject({ kind: 'priority', label: 'critical', tone: 'err' });
    expect(classifyTag('priority:low')).toMatchObject({ kind: 'priority', label: 'low', tone: '' });
  });
  it('prefixes impact', () => {
    expect(classifyTag('impact:3')).toMatchObject({ kind: 'impact', label: 'impact 3', tone: '' });
  });
  it('treats everything else as a label with no color', () => {
    expect(classifyTag('frontend')).toEqual({ rawTag: 'frontend', kind: 'label', label: 'frontend', tone: '' });
  });
});

describe('orderTags', () => {
  it('puts priority first, then impact, then labels', () => {
    const out = orderTags(['frontend', 'impact:4', 'priority:high']).map((t) => t.kind);
    expect(out).toEqual(['priority', 'impact', 'label']);
  });
});
