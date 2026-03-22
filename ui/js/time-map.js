// --- Shared time-mapping utility for gap compression ---
// Used by both the flamegraph (modal-flamegraph.js) and Gantt timeline
// (modal-results.js) to compress idle gaps between activity spans.

// Merge overlapping/adjacent intervals. Input: [{start, end}].
function _mergeIntervals(intervals) {
  if (intervals.length === 0) return [];
  var sorted = intervals.slice().sort(function (a, b) {
    return a.start - b.start;
  });
  var merged = [{ start: sorted[0].start, end: sorted[0].end }];
  for (var i = 1; i < sorted.length; i++) {
    var last = merged[merged.length - 1];
    if (sorted[i].start <= last.end) {
      if (sorted[i].end > last.end) last.end = sorted[i].end;
    } else {
      merged.push({ start: sorted[i].start, end: sorted[i].end });
    }
  }
  return merged;
}

// Build a time mapping that compresses idle gaps between activity spans.
// Returns { toPercent(ms), fromPercent(pct), segments, compressed }.
// When no gap exceeds the threshold, returns a linear mapping (compressed=false).
//
// spans: array of objects with startMs and endMs properties
// globalStartMs / globalEndMs: overall time bounds
function buildTimeMap(spans, globalStartMs, globalEndMs) {
  var totalReal = globalEndMs - globalStartMs;
  var linearTo = function (ms) {
    return totalReal > 0
      ? Math.max(0, Math.min(100, ((ms - globalStartMs) / totalReal) * 100))
      : 0;
  };
  var linearFrom = function (pct) {
    return globalStartMs + (pct / 100) * totalReal;
  };
  var linearMap = {
    toPercent: linearTo,
    fromPercent: linearFrom,
    segments: [],
    compressed: false,
  };

  if (totalReal <= 0 || !spans || spans.length === 0) return linearMap;

  // Build merged activity intervals
  var intervals = [];
  spans.forEach(function (s) {
    if (s.endMs > s.startMs) intervals.push({ start: s.startMs, end: s.endMs });
  });
  if (intervals.length === 0) return linearMap;
  var merged = _mergeIntervals(intervals);

  // Build segments: alternating active and gap
  var segments = [];
  if (merged[0].start > globalStartMs) {
    segments.push({ start: globalStartMs, end: merged[0].start, isGap: true });
  }
  for (var i = 0; i < merged.length; i++) {
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
  var totalActive = 0;
  segments.forEach(function (seg) {
    if (!seg.isGap) totalActive += seg.end - seg.start;
  });
  if (totalActive <= 0) return linearMap;

  // Compress gaps longer than 10% of total active time
  var gapThreshold = totalActive * 0.1;
  var compressedWeight = totalActive * 0.03;
  var anyCompressed = false;

  segments.forEach(function (seg) {
    var dur = seg.end - seg.start;
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
  var totalVisual = 0;
  segments.forEach(function (seg) {
    totalVisual += seg.visualWeight;
  });
  if (totalVisual <= 0) return linearMap;

  var cumul = 0;
  segments.forEach(function (seg) {
    seg.visualStart = cumul;
    cumul += seg.visualWeight;
    seg.visualEnd = cumul;
  });

  function toPercent(ms) {
    if (ms <= globalStartMs) return 0;
    if (ms >= globalEndMs) return 100;
    for (var i = 0; i < segments.length; i++) {
      var seg = segments[i];
      if (
        ms >= seg.start &&
        (ms < seg.end || (i === segments.length - 1 && ms <= seg.end))
      ) {
        var realDur = seg.end - seg.start;
        var frac = realDur > 0 ? (ms - seg.start) / realDur : 0;
        return Math.min(
          100,
          ((seg.visualStart + frac * seg.visualWeight) / totalVisual) * 100,
        );
      }
    }
    return 100;
  }

  function fromPercent(pct) {
    if (pct <= 0) return globalStartMs;
    if (pct >= 100) return globalEndMs;
    var target = (pct / 100) * totalVisual;
    for (var i = 0; i < segments.length; i++) {
      var seg = segments[i];
      if (
        target >= seg.visualStart &&
        (target < seg.visualEnd ||
          (i === segments.length - 1 && target <= seg.visualEnd))
      ) {
        var segVisual = seg.visualEnd - seg.visualStart;
        var frac = segVisual > 0 ? (target - seg.visualStart) / segVisual : 0;
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
