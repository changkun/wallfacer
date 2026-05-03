import { describe, it, expect } from 'vitest';
import { statusCount } from './stats';

describe('statusCount', () => {
  it('returns the value when the API delivers a plain number', () => {
    expect(statusCount(7)).toBe(7);
  });

  // Regression: /api/stats now returns rich per-status objects
  // ({ count, cost_usd, input_tokens, output_tokens }); rendering the
  // bucket as a number dumped JSON into the bar chart.
  it('extracts count from a bucket object', () => {
    expect(statusCount({ count: 3, cost_usd: 0.5, input_tokens: 1, output_tokens: 2 })).toBe(3);
  });

  it('falls back to 0 for null or undefined', () => {
    expect(statusCount(null)).toBe(0);
    expect(statusCount(undefined)).toBe(0);
  });

  it('falls back to 0 when the bucket has no count', () => {
    expect(statusCount({} as { count: number })).toBe(0);
  });
});
