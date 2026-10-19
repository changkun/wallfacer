import { describe, it, expect } from 'vitest';
import { failureLabel } from './cardBadges';

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
