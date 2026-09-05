import { describe, it, expect } from 'vitest';
import { classifyTag, orderTags } from './tagBadge';

describe('classifyTag', () => {
  it('strips the priority prefix and colours only high and critical', () => {
    expect(classifyTag('priority:high')).toMatchObject({ kind: 'priority', label: 'high', tone: 'warn' });
    expect(classifyTag('priority:critical')).toMatchObject({ kind: 'priority', label: 'critical', tone: 'err' });
    expect(classifyTag('priority:low')).toMatchObject({ kind: 'priority', label: 'low', tone: '' });
  });
  it('prefixes impact', () => {
    expect(classifyTag('impact:3')).toMatchObject({ kind: 'impact', label: 'impact 3', tone: '' });
  });
  it('keeps the spawned-by provenance whole', () => {
    expect(classifyTag('spawned-by:abc')).toMatchObject({ kind: 'spawned', label: 'spawned-by:abc' });
  });
  it('treats everything else as a label with no colour', () => {
    expect(classifyTag('frontend')).toEqual({ rawTag: 'frontend', kind: 'label', label: 'frontend', tone: '' });
  });
});

describe('orderTags', () => {
  it('puts priority first, then impact, labels, provenance', () => {
    const out = orderTags(['frontend', 'spawned-by:r1', 'impact:4', 'priority:high']).map((t) => t.kind);
    expect(out).toEqual(['priority', 'impact', 'label', 'spawned']);
  });
});
