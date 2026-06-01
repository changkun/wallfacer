// Helpers for the span-timeline flamegraph view. Pure + tested so the
// layout logic is not buried inside the SVG component. Mirrors the
// label / lane / humanise behaviour of the legacy ui/js/modal-flamegraph.js,
// including idle-gap compression via the shared time map (so a long
// "waiting" gap doesn't squish all the real activity into a sliver).

import { buildTimeMap, type TimeMap } from './timeMap';

export interface SpanResult {
  phase: string;
  label: string;
  started_at: string;
  ended_at: string;
  duration_ms: number;
}

export interface SpanBlock {
  raw: SpanResult;
  startMs: number;
  endMs: number;
  durationMs: number;
  lane: number;
  label: string;
  color: string;
  // Visual x-extent as percentages of the full track width, after idle-gap
  // compression. left/width are what the SVG actually draws.
  leftPct: number;
  widthPct: number;
}

/** Greedy lane-packing: place each span on the lowest lane whose last
 *  endpoint is ≤ the span's start. Spans must arrive sorted by startMs. */
export function assignLanes<T extends { startMs: number; endMs: number }>(spans: T[]): { item: T; lane: number }[] {
  const laneEnds: number[] = [];
  return spans.map((span) => {
    let lane = -1;
    for (let i = 0; i < laneEnds.length; i++) {
      if (laneEnds[i] <= span.startMs) { lane = i; break; }
    }
    if (lane === -1) { lane = laneEnds.length; laneEnds.push(0); }
    laneEnds[lane] = span.endMs;
    return { item: span, lane };
  });
}

/** Format a duration in ms as ms / s / min / h. */
export function formatMs(ms: number): string {
  if (ms < 1000) return ms.toFixed(0) + 'ms';
  if (ms > 60_000 && ms <= 3_600_000) return (ms / 60_000).toFixed(1) + 'min';
  if (ms > 3_600_000) return (ms / 3_600_000).toFixed(1) + 'h';
  return (ms / 1000).toFixed(1) + 's';
}

/** Convert a raw phase:label key into a human-readable display string. */
export function humanSpanLabel(phase: string, label: string): string {
  if (phase === 'agent_turn') {
    let m: RegExpMatchArray | null;
    if ((m = label.match(/^implementation_(\d+)$/))) return `Impl. Turn ${m[1]}`;
    if ((m = label.match(/^test_(\d+)$/))) return `Test Turn ${m[1]}`;
    if ((m = label.match(/^agent_turn_(\d+)$/))) return `Turn ${m[1]}`;
    return label || phase;
  }
  if (phase === 'container_run') return label || 'Container';
  if (phase === 'commit') return 'Commit';
  if (phase === 'oversight') return 'Oversight';
  if (phase === 'title_gen') return 'Title gen';
  return label ? `${phase}: ${label}` : phase;
}

/** Deterministic per-label colour (HSL hue from a djb2 hash). */
export function labelHue(s: string): number {
  let h = 5381;
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h) ^ s.charCodeAt(i);
  return Math.abs(h) % 360;
}

export interface FlamegraphLayout {
  blocks: SpanBlock[];
  t0: number;
  t1: number;
  timeMap: TimeMap;
  laneCount: number;
}

/** Lay out a list of SpanResults into renderable blocks. Positions are
 *  expressed both in milliseconds (for tooltips) and as compressed
 *  percentages (leftPct/widthPct) via the shared time map, matching the
 *  legacy flamegraph's idle-gap compression. */
export function layoutSpans(spans: SpanResult[]): FlamegraphLayout {
  const empty = (): FlamegraphLayout => ({
    blocks: [], t0: 0, t1: 0, laneCount: 0,
    timeMap: buildTimeMap(null, 0, 0),
  });
  if (!spans.length) return empty();
  const items = spans
    .map((raw) => {
      const startMs = new Date(raw.started_at).getTime();
      const endRaw = raw.ended_at ? new Date(raw.ended_at).getTime() : 0;
      const endMs = endRaw > startMs ? endRaw : startMs + (raw.duration_ms || 0);
      return { raw, startMs, endMs };
    })
    .filter((x) => !Number.isNaN(x.startMs))
    .sort((a, b) => a.startMs - b.startMs);
  if (!items.length) return empty();

  const t0 = items[0].startMs;
  let t1 = items[0].endMs;
  for (const it of items) if (it.endMs > t1) t1 = it.endMs;
  if (t1 <= t0) t1 = t0 + 1;

  // Compress idle gaps so positions reflect activity density, not wall clock.
  const timeMap = buildTimeMap(items, t0, t1);

  const lanes = assignLanes(items);
  let laneCount = 0;
  const blocks: SpanBlock[] = lanes.map(({ item, lane }) => {
    if (lane + 1 > laneCount) laneCount = lane + 1;
    const left = timeMap.toPercent(item.startMs);
    const right = timeMap.toPercent(item.endMs);
    return {
      raw: item.raw,
      startMs: item.startMs,
      endMs: item.endMs,
      durationMs: item.endMs - item.startMs,
      lane,
      label: humanSpanLabel(item.raw.phase, item.raw.label),
      color: `hsl(${labelHue(item.raw.phase + ':' + item.raw.label)}, 55%, 55%)`,
      leftPct: left,
      widthPct: Math.max(0.4, right - left),
    };
  });
  return { blocks, t0, t1, timeMap, laneCount };
}

// Per-turn usage record (subset of store.TurnUsageRecord) used by the
// cumulative cost chart overlay.
export interface TurnUsageRecord {
  timestamp?: string;
  cost_usd?: number;
  sub_agent?: string;
}

export interface CostPoint {
  xPct: number;
  cost: number;
  activity: string;
}

// Build cumulative-cost points for the flamegraph cost chart, positioned along
// the (idle-compressed) timeline via toPercent. Records with no positive cost
// or unparseable timestamp are skipped. Mirrors modal-flamegraph.js buildCostChart.
export function cumulativeCostPoints(
  records: readonly TurnUsageRecord[],
  toPercent: (ms: number) => number,
): { points: CostPoint[]; maxCost: number } {
  const sorted = [...records].sort((a, b) => {
    const ta = a.timestamp ? Date.parse(a.timestamp) : 0;
    const tb = b.timestamp ? Date.parse(b.timestamp) : 0;
    return ta - tb;
  });
  let cum = 0;
  const points: CostPoint[] = [{ xPct: 0, cost: 0, activity: '' }];
  for (const u of sorted) {
    const cost = u.cost_usd || 0;
    if (cost <= 0) continue;
    const ts = u.timestamp ? Date.parse(u.timestamp) : NaN;
    if (!Number.isFinite(ts)) continue;
    cum += cost;
    points.push({ xPct: toPercent(ts), cost: cum, activity: u.sub_agent || '' });
  }
  return { points, maxCost: points[points.length - 1].cost };
}
