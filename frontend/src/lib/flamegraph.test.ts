import { describe, it, expect } from 'vitest';
import { assignLanes, formatMs, humanSpanLabel, layoutSpans, labelHue, type SpanResult } from './flamegraph';

describe('assignLanes', () => {
  it('packs non-overlapping spans onto lane 0', () => {
    const out = assignLanes([
      { startMs: 0, endMs: 10 },
      { startMs: 10, endMs: 20 },
      { startMs: 20, endMs: 30 },
    ]);
    expect(out.map((x) => x.lane)).toEqual([0, 0, 0]);
  });
  it('spreads overlapping spans across lanes', () => {
    const out = assignLanes([
      { startMs: 0, endMs: 20 },
      { startMs: 5, endMs: 25 },
      { startMs: 10, endMs: 15 },
    ]);
    expect(out.map((x) => x.lane)).toEqual([0, 1, 2]);
  });
});

describe('formatMs', () => {
  it.each([
    [500, '500ms'],
    [1500, '1.5s'],
    [120_000, '2.0min'],
    [7_200_000, '2.0h'],
  ])('formats %d → %s', (ms, expected) => {
    expect(formatMs(ms)).toBe(expected);
  });
});

describe('humanSpanLabel', () => {
  it('renames agent_turn variants', () => {
    expect(humanSpanLabel('agent_turn', 'implementation_3')).toBe('Impl. Turn 3');
    expect(humanSpanLabel('agent_turn', 'test_1')).toBe('Test Turn 1');
    expect(humanSpanLabel('agent_turn', 'agent_turn_2')).toBe('Turn 2');
  });
  it('falls back to phase:label form for unknown phases', () => {
    expect(humanSpanLabel('migration', 'apply')).toBe('migration: apply');
  });
});

describe('labelHue', () => {
  it('returns a stable value in 0..360 for any string', () => {
    const h = labelHue('agent_turn:implementation_1');
    expect(h).toBeGreaterThanOrEqual(0);
    expect(h).toBeLessThan(360);
    expect(labelHue('agent_turn:implementation_1')).toBe(h);
  });
});

describe('layoutSpans', () => {
  it('returns empty range for no spans', () => {
    const r = layoutSpans([]);
    expect(r.blocks).toEqual([]);
    expect(r.t0).toBe(0);
    expect(r.t1).toBe(0);
  });
  it('computes lanes + start/end ms for a small overlap', () => {
    const spans: SpanResult[] = [
      { phase: 'agent_turn', label: 'implementation_1', started_at: '2026-06-01T00:00:00.000Z', ended_at: '2026-06-01T00:00:10.000Z', duration_ms: 10_000 },
      { phase: 'commit', label: '', started_at: '2026-06-01T00:00:08.000Z', ended_at: '2026-06-01T00:00:14.000Z', duration_ms: 6000 },
    ];
    const { blocks, t0, t1, laneCount } = layoutSpans(spans);
    expect(blocks).toHaveLength(2);
    expect(t1 - t0).toBe(14_000);
    expect(blocks[0].lane).toBe(0);
    expect(blocks[1].lane).toBe(1); // overlaps → new lane
    expect(laneCount).toBe(2);
    expect(blocks[0].label).toBe('Impl. Turn 1');
    expect(blocks[1].label).toBe('Commit');
  });

  it('every block carries a visual extent (leftPct/widthPct)', () => {
    const spans: SpanResult[] = [
      { phase: 'commit', label: '', started_at: '2026-06-01T00:00:00.000Z', ended_at: '2026-06-01T00:00:05.000Z', duration_ms: 5000 },
    ];
    const { blocks } = layoutSpans(spans);
    expect(blocks[0].leftPct).toBeGreaterThanOrEqual(0);
    expect(blocks[0].widthPct).toBeGreaterThanOrEqual(0.4);
  });

  it('compresses a long idle gap so flanking activity is not squished', () => {
    // 5s of work, a 1h idle gap, then 5s more. Without compression the two
    // active blocks would each occupy <0.3% of the width. The time map should
    // mark the gap compressed and pull the active blocks well apart.
    const spans: SpanResult[] = [
      { phase: 'agent_turn', label: 'implementation_1', started_at: '2026-06-01T00:00:00.000Z', ended_at: '2026-06-01T00:00:05.000Z', duration_ms: 5000 },
      { phase: 'agent_turn', label: 'implementation_2', started_at: '2026-06-01T01:00:05.000Z', ended_at: '2026-06-01T01:00:10.000Z', duration_ms: 5000 },
    ];
    const lay = layoutSpans(spans);
    expect(lay.timeMap.compressed).toBe(true);
    // First block starts at 0%, second block's start is pushed far right but
    // not pinned at 100% (the gap is compressed, not removed).
    expect(lay.blocks[0].leftPct).toBeCloseTo(0, 1);
    expect(lay.blocks[1].leftPct).toBeGreaterThan(40);
    expect(lay.blocks[1].leftPct).toBeLessThan(100);
    // The first 5s block keeps a meaningful width instead of collapsing.
    expect(lay.blocks[0].widthPct).toBeGreaterThan(5);
  });
});
