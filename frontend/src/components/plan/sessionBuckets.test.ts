import { describe, it, expect } from 'vitest';
import { bucketForUpdated } from './sessionBuckets';

const DAY = 86_400_000;
const NOW = 1_700_000_000_000;

describe('bucketForUpdated', () => {
  it('buckets recent activity as today', () => {
    expect(bucketForUpdated(NOW, NOW)).toBe('today');
    expect(bucketForUpdated(NOW, NOW - 1)).toBe('today');
    expect(bucketForUpdated(NOW, NOW - (DAY - 1))).toBe('today');
  });

  it('crosses into last7 exactly at the 1-day boundary', () => {
    expect(bucketForUpdated(NOW, NOW - DAY)).toBe('last7');
    expect(bucketForUpdated(NOW, NOW - 3 * DAY)).toBe('last7');
    expect(bucketForUpdated(NOW, NOW - (7 * DAY - 1))).toBe('last7');
  });

  it('crosses into last30 exactly at the 7-day boundary', () => {
    expect(bucketForUpdated(NOW, NOW - 7 * DAY)).toBe('last30');
    expect(bucketForUpdated(NOW, NOW - 20 * DAY)).toBe('last30');
    expect(bucketForUpdated(NOW, NOW - (30 * DAY - 1))).toBe('last30');
  });

  it('crosses into older exactly at the 30-day boundary', () => {
    expect(bucketForUpdated(NOW, NOW - 30 * DAY)).toBe('older');
    expect(bucketForUpdated(NOW, NOW - 365 * DAY)).toBe('older');
  });

  it('treats a missing (zero) timestamp as older', () => {
    expect(bucketForUpdated(NOW, 0)).toBe('older');
  });

  it('treats a future timestamp (clock skew) as today', () => {
    expect(bucketForUpdated(NOW, NOW + DAY)).toBe('today');
  });
});
