import { describe, it, expect } from 'vitest';
import { hitTestZone } from './drag';

const rect = { left: 0, top: 0, width: 1000, height: 1000 };

describe('hitTestZone', () => {
  it('returns the nearest edge within the band', () => {
    expect(hitTestZone(rect, 50, 500)).toBe('left');
    expect(hitTestZone(rect, 950, 500)).toBe('right');
    expect(hitTestZone(rect, 500, 50)).toBe('top');
    expect(hitTestZone(rect, 500, 950)).toBe('bottom');
  });

  it('returns null in the center dead zone', () => {
    expect(hitTestZone(rect, 500, 500)).toBeNull();
  });

  it('returns null outside the workspace', () => {
    expect(hitTestZone(rect, -10, 500)).toBeNull();
    expect(hitTestZone(rect, 500, 1200)).toBeNull();
  });

  it('resolves a corner to the nearer edge', () => {
    // Closer to the top than the left.
    expect(hitTestZone(rect, 200, 50)).toBe('top');
    // Closer to the left than the top.
    expect(hitTestZone(rect, 50, 200)).toBe('left');
  });

  it('honors an offset rect', () => {
    expect(hitTestZone({ left: 100, top: 100, width: 200, height: 200 }, 110, 200)).toBe('left');
  });

  it('guards against a zero-size rect', () => {
    expect(hitTestZone({ left: 0, top: 0, width: 0, height: 0 }, 0, 0)).toBeNull();
  });
});
