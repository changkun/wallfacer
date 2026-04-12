// --- Shared time-mapping utility for gap compression ---
// Used by both the flamegraph (modal-flamegraph.js) and Gantt timeline
// (modal-results.js) to compress idle gaps between activity spans.

interface TimeMapInterval {
  start: number;
  end: number;
}

interface TimeMapSegment {
  start: number;
  end: number;
  isGap: boolean;
  visualWeight?: number;
  visualStart?: number;
  visualEnd?: number;
  compressed?: boolean;
}

interface TimeMapSpan {
  startMs: number;
  endMs: number;
}

interface TimeMap {
  toPercent: (ms: number) => number;
  fromPercent: (pct: number) => number;
  segments: TimeMapSegment[];
  compressed: boolean;
  totalVisual?: number;
}

/** Merge overlapping/adjacent intervals. */
function _mergeIntervals(intervals: TimeMapInterval[]): TimeMapInterval[] {
  if (intervals.length === 0) return [];
  const sorted = intervals.slice().sort((a, b) => a.start - b.start);
  const merged: TimeMapInterval[] = [
    { start: sorted[0].start, end: sorted[0].end },
  ];
  for (let i = 1; i < sorted.length; i++) {
    const last = merged[merged.length - 1];
    if (sorted[i].start <= last.end) {
      if (sorted[i].end > last.end) last.end = sorted[i].end;
    } else {
      merged.push({ start: sorted[i].start, end: sorted[i].end });
    }
  }
  return merged;
}

/**
 * Build a time mapping that compresses idle gaps between activity spans.
 * When no gap exceeds the threshold, returns a linear mapping
 * (compressed=false).
 */
function buildTimeMap(
  spans: TimeMapSpan[] | null | undefined,
  globalStartMs: number,
  globalEndMs: number,
): TimeMap {
  const totalReal = globalEndMs - globalStartMs;
  const linearTo = (ms: number): number =>
    totalReal > 0
      ? Math.max(0, Math.min(100, ((ms - globalStartMs) / totalReal) * 100))
      : 0;
  const linearFrom = (pct: number): number =>
    globalStartMs + (pct / 100) * totalReal;
  const linearMap: TimeMap = {
    toPercent: linearTo,
    fromPercent: linearFrom,
    segments: [],
    compressed: false,
  };

  if (totalReal <= 0 || !spans || spans.length === 0) return linearMap;

  // Build merged activity intervals
  const intervals: TimeMapInterval[] = [];
  spans.forEach((s) => {
    if (s.endMs > s.startMs) intervals.push({ start: s.startMs, end: s.endMs });
  });
  if (intervals.length === 0) return linearMap;
  const merged = _mergeIntervals(intervals);

  // Build segments: alternating active and gap
  const segments: TimeMapSegment[] = [];
  if (merged[0].start > globalStartMs) {
    segments.push({ start: globalStartMs, end: merged[0].start, isGap: true });
  }
  for (let i = 0; i < merged.length; i++) {
    segments.push({ start: merged[i].start, end: merged[i].end, isGap: false });
    if (i + 1 < merged.length && merged[i].end < merged[i + 1].start) {
      segments.push({
        start: merged[i].end,
        end: merged[i + 1].start,
        isGap: true,
      });
    }
  }
  if (merged[merged.length - 1].end < globalEndMs) {
    segments.push({
      start: merged[merged.length - 1].end,
      end: globalEndMs,
      isGap: true,
    });
  }

  // Compute total active time
  let totalActive = 0;
  segments.forEach((seg) => {
    if (!seg.isGap) totalActive += seg.end - seg.start;
  });
  if (totalActive <= 0) return linearMap;

  // Compress gaps longer than 10% of total active time
  const gapThreshold = totalActive * 0.1;
  const compressedWeight = totalActive * 0.03;
  let anyCompressed = false;

  segments.forEach((seg) => {
    const dur = seg.end - seg.start;
    if (seg.isGap && dur > gapThreshold) {
      seg.visualWeight = compressedWeight;
      seg.compressed = true;
      anyCompressed = true;
    } else {
      seg.visualWeight = dur;
      seg.compressed = false;
    }
  });

  if (!anyCompressed) return linearMap;

  // Compute cumulative visual positions
  let totalVisual = 0;
  segments.forEach((seg) => {
    totalVisual += seg.visualWeight || 0;
  });
  if (totalVisual <= 0) return linearMap;

  let cumul = 0;
  segments.forEach((seg) => {
    seg.visualStart = cumul;
    cumul += seg.visualWeight || 0;
    seg.visualEnd = cumul;
  });

  function toPercent(ms: number): number {
    if (ms <= globalStartMs) return 0;
    if (ms >= globalEndMs) return 100;
    for (let i = 0; i < segments.length; i++) {
      const seg = segments[i];
      if (
        ms >= seg.start &&
        (ms < seg.end || (i === segments.length - 1 && ms <= seg.end))
      ) {
        const realDur = seg.end - seg.start;
        const frac = realDur > 0 ? (ms - seg.start) / realDur : 0;
        const vStart = seg.visualStart || 0;
        const vWeight = seg.visualWeight || 0;
        return Math.min(100, ((vStart + frac * vWeight) / totalVisual) * 100);
      }
    }
    return 100;
  }

  function fromPercent(pct: number): number {
    if (pct <= 0) return globalStartMs;
    if (pct >= 100) return globalEndMs;
    const target = (pct / 100) * totalVisual;
    for (let i = 0; i < segments.length; i++) {
      const seg = segments[i];
      const vStart = seg.visualStart || 0;
      const vEnd = seg.visualEnd || 0;
      if (
        target >= vStart &&
        (target < vEnd || (i === segments.length - 1 && target <= vEnd))
      ) {
        const segVisual = vEnd - vStart;
        const frac = segVisual > 0 ? (target - vStart) / segVisual : 0;
        return seg.start + frac * (seg.end - seg.start);
      }
    }
    return globalEndMs;
  }

  return {
    toPercent: toPercent,
    fromPercent: fromPercent,
    segments: segments,
    compressed: true,
    totalVisual: totalVisual,
  };
}
