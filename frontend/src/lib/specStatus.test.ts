import { describe, expect, it } from 'vitest';
import { specStatusPill, specStatusTone } from './specStatus';

describe('specStatusTone', () => {
  it('maps every lifecycle status to one ramp tone', () => {
    expect(specStatusTone('vague')).toBe('neutral');
    expect(specStatusTone('drafted')).toBe('neutral');
    expect(specStatusTone('validated')).toBe('run');
    expect(specStatusTone('testing')).toBe('warn');
    expect(specStatusTone('complete')).toBe('ok');
    expect(specStatusTone('stale')).toBe('err');
    expect(specStatusTone('archived')).toBe('muted');
  });
  it('mutes a frontmatter-less doc whatever its status says', () => {
    expect(specStatusTone('complete', true)).toBe('muted');
  });
  it('gives the focused view a pill class', () => {
    expect(specStatusPill('complete')).toBe('pill-ok');
    expect(specStatusPill('archived')).toBe('pill-neutral');
  });
});
