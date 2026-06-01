import { describe, it, expect } from 'vitest';
import { shouldRefetchOnVisible } from './visibility';

describe('shouldRefetchOnVisible', () => {
  it('refetches when visible and a workspace is active', () => {
    expect(shouldRefetchOnVisible('visible', true)).toBe(true);
  });
  it('skips when hidden', () => {
    expect(shouldRefetchOnVisible('hidden', true)).toBe(false);
  });
  it('skips when no workspace is active', () => {
    expect(shouldRefetchOnVisible('visible', false)).toBe(false);
  });
});
