import { describe, it, expect } from 'vitest';
import { isSystemRoutineCard } from './boardVisibility';

describe('isSystemRoutineCard', () => {
  it('hides routine cards tagged system:*', () => {
    expect(isSystemRoutineCard({ kind: 'routine', tags: ['system:ideation'] })).toBe(true);
  });
  it('keeps routine cards without a system:* tag', () => {
    expect(isSystemRoutineCard({ kind: 'routine', tags: ['nightly'] })).toBe(false);
  });
  it('keeps non-routine tasks even with a system:* tag', () => {
    expect(isSystemRoutineCard({ kind: 'task', tags: ['system:ideation'] })).toBe(false);
  });
  it('handles missing tags', () => {
    expect(isSystemRoutineCard({ kind: 'routine', tags: undefined })).toBe(false);
  });
});
