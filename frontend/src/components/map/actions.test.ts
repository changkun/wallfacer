import { describe, it, expect } from 'vitest';
import { hasPrimaryAction } from './actions';

describe('hasPrimaryAction', () => {
  it('flags forward-momentum actions', () => {
    expect(hasPrimaryAction(['validate'])).toBe(true);
    expect(hasPrimaryAction(['dispatch'])).toBe(true);
    expect(hasPrimaryAction(['start'])).toBe(true);
  });

  it('does not flag recovery/reversal actions alone', () => {
    expect(hasPrimaryAction(['unstale'])).toBe(false);
    expect(hasPrimaryAction(['unarchive'])).toBe(false);
    expect(hasPrimaryAction(['undispatch'])).toBe(false);
    expect(hasPrimaryAction(['force-complete'])).toBe(false);
    expect(hasPrimaryAction([])).toBe(false);
    expect(hasPrimaryAction(undefined)).toBe(false);
  });
});
