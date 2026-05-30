import { describe, it, expect } from 'vitest';
import { hasUnseen } from './unread';

describe('hasUnseen', () => {
  it('true when an id is not in the seen set', () => {
    expect(hasUnseen(['a', 'b'], new Set(['a']))).toBe(true);
  });
  it('false when all ids are seen', () => {
    expect(hasUnseen(['a', 'b'], new Set(['a', 'b']))).toBe(false);
  });
  it('false for an empty id list', () => {
    expect(hasUnseen([], new Set())).toBe(false);
  });
});
