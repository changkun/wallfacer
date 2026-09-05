import { describe, it, expect } from 'vitest';
import { statusPill } from './statusPill';

describe('statusPill', () => {
  it('maps each column to its ramp tone', () => {
    expect(statusPill('in_progress')).toBe('pill-run');
    expect(statusPill('committing')).toBe('pill-run');
    expect(statusPill('waiting')).toBe('pill-warn');
    expect(statusPill('done')).toBe('pill-ok');
    expect(statusPill('failed')).toBe('pill-err');
    expect(statusPill('cancelled')).toBe('pill-pub');
  });
  it('falls back to neutral for backlog and unknown states', () => {
    expect(statusPill('backlog')).toBe('pill-neutral');
    expect(statusPill(undefined)).toBe('pill-neutral');
  });
});
