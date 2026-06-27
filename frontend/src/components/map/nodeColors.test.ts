import { describe, it, expect } from 'vitest';
import { stateColor, STATE_COLORS } from './nodeColors';

describe('stateColor', () => {
  it('returns a distinct color for each known state', () => {
    const states = Object.keys(STATE_COLORS);
    for (const s of states) {
      expect(stateColor(s)).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });

  it('distinguishes the common pipeline states', () => {
    const distinct = new Set(
      ['backlog', 'in_progress', 'done', 'failed', 'drafted', 'validated', 'complete', 'stale'].map(stateColor),
    );
    expect(distinct.size).toBeGreaterThanOrEqual(6);
  });

  it('falls back to a neutral color for an unknown state', () => {
    expect(stateColor('something-else')).toMatch(/^#[0-9a-f]{6}$/i);
  });
});
