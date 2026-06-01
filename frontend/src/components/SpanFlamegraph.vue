<script setup lang="ts">
// SVG-based span timeline (a Gantt-style flamegraph) for the task detail
// view. Pure layout helpers live in lib/flamegraph; this component is just
// presentation + hover tooltip. Lanes are laid out top-down; horizontal
// axis is the task's wall-clock time normalised into 100% of the SVG width.
import { computed, ref } from 'vue';
import { formatMs, layoutSpans, labelHue, cumulativeCostPoints, type SpanResult, type TurnUsageRecord } from '../lib/flamegraph';

const props = defineProps<{ spans: SpanResult[]; turnUsages?: TurnUsageRecord[] }>();

// Cumulative-cost overlay positioned along the (idle-compressed) timeline.
const costChart = computed(() => {
  const recs = props.turnUsages ?? [];
  if (!recs.length) return null;
  const { points, maxCost } = cumulativeCostPoints(recs, layout.value.timeMap.toPercent);
  if (points.length < 2 || maxCost <= 0) return null;
  const chartH = 48, padding = 4, innerH = chartH - padding * 2;
  const yFor = (cost: number) => padding + innerH * (1 - cost / maxCost);
  return {
    chartH,
    total: `$${maxCost.toFixed(4)}`,
    polyPoints: points.map((p) => `${p.xPct.toFixed(3)},${yFor(p.cost).toFixed(1)}`).join(' '),
    dots: points.slice(1).map((p) => ({
      left: p.xPct,
      top: yFor(p.cost),
      color: `hsl(${labelHue(p.activity)}, 55%, 55%)`,
      title: `${p.activity || 'cost'}: $${p.cost.toFixed(4)}`,
    })),
  };
});
const LANE_H = 22;
const LANE_GAP = 3;
const PAD_TOP = 24; // axis row

const layout = computed(() => layoutSpans(props.spans));
const laneCount = computed(() => Math.max(1, layout.value.laneCount));
const svgHeight = computed(() => PAD_TOP + laneCount.value * (LANE_H + LANE_GAP));

function blockY(lane: number): number {
  return PAD_TOP + lane * (LANE_H + LANE_GAP);
}

// Axis ticks at 5 evenly-spaced VISUAL positions. Because idle gaps are
// compressed, each tick's label is the real elapsed time at that visual
// fraction (timeMap.fromPercent), so the axis stays truthful even when the
// horizontal scale is non-linear. Compressed gap segments get a hatched
// marker so the break is legible.
const ticks = computed(() => {
  const lay = layout.value;
  const out: { pct: number; label: string }[] = [];
  for (let i = 0; i <= 5; i++) {
    const pct = (i / 5) * 100;
    const realMs = lay.timeMap.fromPercent(pct) - lay.t0;
    out.push({ pct, label: formatMs(realMs) });
  }
  return out;
});

// Hatched markers for each compressed idle gap, positioned in visual space.
const gapMarkers = computed(() => {
  const lay = layout.value;
  if (!lay.timeMap.compressed) return [];
  const out: { left: number; width: number; title: string }[] = [];
  for (const seg of lay.timeMap.segments) {
    if (!seg.compressed) continue;
    const left = lay.timeMap.toPercent(seg.start);
    const right = lay.timeMap.toPercent(seg.end);
    const width = right - left;
    if (width < 0.1) continue;
    out.push({
      left,
      width,
      title: `Idle ${formatMs(seg.end - seg.start)}`,
    });
  }
  return out;
});

const hovered = ref<{ label: string; range: string; lane: number; x: number; y: number } | null>(null);
function showTip(ev: MouseEvent, b: typeof layout.value.blocks[number]) {
  const rect = (ev.currentTarget as SVGElement).getBoundingClientRect();
  hovered.value = {
    label: b.label,
    range: `${formatMs(b.startMs - layout.value.t0)} → ${formatMs(b.endMs - layout.value.t0)} (${formatMs(b.durationMs)})`,
    lane: b.lane,
    x: rect.left,
    y: rect.top,
  };
}
function hideTip() { hovered.value = null; }

// Detail table: every span sorted by duration descending, with its start
// offset, duration, and share of total span time (mirrors modal-flamegraph.js).
const detailRows = computed(() => {
  const lay = layout.value;
  const total = lay.blocks.reduce((sum, b) => sum + b.durationMs, 0);
  return [...lay.blocks]
    .sort((a, b) => b.durationMs - a.durationMs)
    .map((b) => ({
      label: b.label,
      color: b.color,
      start: formatMs(b.startMs - lay.t0),
      duration: formatMs(b.durationMs),
      pct: total > 0 ? ((b.durationMs / total) * 100).toFixed(1) : '0.0',
    }));
});
</script>

<template>
  <div class="flamegraph">
    <div v-if="!layout.blocks.length" class="flamegraph__empty">
      No timing spans recorded yet.
    </div>
    <svg
      v-else
      :viewBox="`0 0 100 ${svgHeight}`"
      preserveAspectRatio="none"
      width="100%"
      :height="svgHeight"
      role="img"
      aria-label="Span timeline"
    >
      <!-- Compressed idle-gap markers (hatched) behind everything else. -->
      <rect
        v-for="(g, i) in gapMarkers"
        :key="'gap' + i"
        :x="g.left"
        :y="PAD_TOP"
        :width="g.width"
        :height="svgHeight - PAD_TOP"
        class="flamegraph__gap"
      >
        <title>{{ g.title }}</title>
      </rect>

      <!-- Axis ticks. The vertical lines extend down across all lanes
           so the user can eyeball alignment between blocks. -->
      <g v-for="t in ticks" :key="t.pct" class="flamegraph__tick">
        <line :x1="t.pct" :y1="PAD_TOP - 4" :x2="t.pct" :y2="svgHeight" />
        <text :x="t.pct" :y="PAD_TOP - 8" text-anchor="middle">{{ t.label }}</text>
      </g>

      <!-- Span blocks, positioned in compressed visual space (leftPct/widthPct). -->
      <g v-for="(b, i) in layout.blocks" :key="i">
        <rect
          :x="b.leftPct"
          :y="blockY(b.lane)"
          :width="b.widthPct"
          :height="LANE_H"
          :fill="b.color"
          rx="2"
          @mouseenter="(e) => showTip(e, b)"
          @mouseleave="hideTip"
        />
        <text
          :x="b.leftPct + 0.5"
          :y="blockY(b.lane) + LANE_H / 2"
          dominant-baseline="middle"
          class="flamegraph__label"
        >{{ b.label }}</text>
      </g>
    </svg>
    <div
      v-if="hovered"
      class="flamegraph__tip"
      :style="{ left: hovered.x + 'px', top: (hovered.y + 24) + 'px' }"
    >
      <div class="flamegraph__tip-label">{{ hovered.label }}</div>
      <div class="flamegraph__tip-range">{{ hovered.range }}</div>
    </div>

    <div
      v-if="costChart"
      class="flamegraph__cost"
      :style="{ height: costChart.chartH + 'px' }"
      :title="`Cumulative cost across activities. Total: ${costChart.total}`"
    >
      <svg :viewBox="`0 0 100 ${costChart.chartH}`" preserveAspectRatio="none" width="100%" :height="costChart.chartH">
        <polyline :points="costChart.polyPoints" class="flamegraph__cost-line" />
      </svg>
      <span
        v-for="(d, i) in costChart.dots"
        :key="'dot' + i"
        class="flamegraph__cost-dot"
        :style="{ left: d.left + '%', top: d.top + 'px', background: d.color }"
        :title="d.title"
      />
      <span class="flamegraph__cost-total">{{ costChart.total }}</span>
    </div>

    <table v-if="detailRows.length" class="flamegraph__table">
      <thead>
        <tr>
          <th class="flamegraph__th-left">Span</th>
          <th class="flamegraph__th-right">Start</th>
          <th class="flamegraph__th-right">Duration</th>
          <th class="flamegraph__th-right">%</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, i) in detailRows" :key="i">
          <td>
            <span class="flamegraph__swatch" :style="{ background: r.color }" />
            {{ r.label }}
          </td>
          <td class="flamegraph__td-right">{{ r.start }}</td>
          <td class="flamegraph__td-right">{{ r.duration }}</td>
          <td class="flamegraph__td-right flamegraph__td-muted">{{ r.pct }}%</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.flamegraph { width: 100%; position: relative; }
.flamegraph__empty {
  padding: 16px;
  font-size: 12px;
  color: var(--text-muted);
  text-align: center;
}
.flamegraph__tick line {
  stroke: var(--rule);
  stroke-width: 0.05;
  vector-effect: non-scaling-stroke;
}
.flamegraph__tick text {
  font-size: 9px;
  fill: var(--text-muted);
  font-family: var(--font-mono);
}
.flamegraph__gap {
  fill: var(--bg-sunk, rgba(127, 127, 127, 0.08));
  opacity: 0.5;
}
.flamegraph__label {
  font-size: 10px;
  fill: #fff;
  font-family: var(--font-sans);
  pointer-events: none;
  /* SVG text in a non-uniformly-scaled viewBox squashes horizontally.
     Negative letter-spacing pulls it back so labels remain readable. */
  letter-spacing: -0.5px;
}
.flamegraph__tip {
  position: fixed;
  z-index: 200;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 10px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
  font-size: 11px;
  pointer-events: none;
}
.flamegraph__tip-label { font-weight: 600; color: var(--text); }
.flamegraph__tip-range { color: var(--text-muted); font-family: var(--font-mono); }

.flamegraph__cost {
  position: relative;
  width: 100%;
  margin-top: 8px;
}
.flamegraph__cost-line {
  fill: none;
  stroke: var(--accent);
  stroke-width: 1.5;
  vector-effect: non-scaling-stroke;
}
.flamegraph__cost-dot {
  position: absolute;
  width: 6px;
  height: 6px;
  margin: -3px 0 0 -3px;
  border-radius: 50%;
  pointer-events: none;
}
.flamegraph__cost-total {
  position: absolute;
  top: 0;
  right: 0;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--text-muted);
}

.flamegraph__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 11px;
  margin-top: 12px;
}
.flamegraph__table th {
  padding: 3px 6px;
  font-weight: 500;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
}
.flamegraph__th-left { text-align: left; }
.flamegraph__th-right { text-align: right; }
.flamegraph__table td {
  padding: 3px 6px;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}
.flamegraph__td-right { text-align: right; font-family: var(--font-mono); }
.flamegraph__td-muted { color: var(--text-muted); }
.flamegraph__swatch {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 2px;
  margin-right: 4px;
}
</style>
