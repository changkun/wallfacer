import { describe, it, expect } from 'vitest';
import { stateColor, STATE_COLORS } from './nodeColors';

describe('stateColor', () => {
  it('gives every state its own colour expression on the ramp', () => {
    const values = Object.values(STATE_COLORS);
    expect(new Set(values).size).toBe(values.length);
    for (const v of values) expect(v).toMatch(/^(var\(--|color-mix\()/);
    expect(stateColor('in_progress')).toBe('var(--run)');
    expect(stateColor('failed')).toBe('var(--err)');
  });
  it('falls back to the backlog tone for an unknown state', () => {
    expect(stateColor('something-else')).toBe(STATE_COLORS.backlog);
  });
});
