import { describe, it, expect } from 'vitest';
import { hashToRoute } from './hashRoute';

describe('hashToRoute', () => {
  it('maps a uuid hash to the board with ?task', () => {
    expect(hashToRoute('#b4e13ac4-8bd6-46f5-a470-c7a31d53f0f6')).toEqual({
      path: '/', query: { task: 'b4e13ac4-8bd6-46f5-a470-c7a31d53f0f6' },
    });
  });
  it('maps #plan/<path> to /plan?spec', () => {
    expect(hashToRoute('#plan/specs/local/foo.md')).toEqual({
      path: '/plan', query: { spec: 'specs/local/foo.md' },
    });
  });
  it('maps #plan to /plan', () => {
    expect(hashToRoute('#plan')).toEqual({ path: '/plan' });
  });
  it('returns null for empty or unrecognised hashes', () => {
    expect(hashToRoute('')).toBeNull();
    expect(hashToRoute('#')).toBeNull();
    expect(hashToRoute('#whatever')).toBeNull();
  });
});
