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
const trackHeight = computed(() => PAD_TOP + laneCount.value * (LANE_H + LANE_GAP));

function blockY(lane: number): number {
  return PAD_TOP + lane * (LANE_H + LANE_GAP);
}

// Tick labels are HTML, anchored at their percentage. Edge labels hug the
// track edges (first left-aligned, last right-aligned) so they stay inside.
function tickLabelStyle(pct: number): Record<string, string> {
  const transform = pct <= 0 ? 'none' : pct >= 100 ? 'translateX(-100%)' : 'translateX(-50%)';
  return { left: pct + '%', transform };
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
  const rect = (ev.currentTarget as HTMLElement).getBoundingClientRect();
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
    <!-- Bars, labels, and axis are percentage-positioned HTML overlays, not
         SVG <text>. An SVG with preserveAspectRatio="none" stretches text
         horizontally by the (width / 100) scale factor, smearing labels into
         an unreadable mess at real widths. HTML positioning sidesteps that. -->
    <div v-else class="flamegraph__track" :style="{ height: trackHeight + 'px' }">
      <!-- Compressed idle-gap markers (hatched) behind everything else. -->
      <div
        v-for="(g, i) in gapMarkers"
        :key="'gap' + i"
        class="flamegraph__gap"
        :style="{ left: g.left + '%', width: g.width + '%', top: PAD_TOP + 'px', height: (trackHeight - PAD_TOP) + 'px' }"
        :title="g.title"
      />

      <!-- Axis ticks: a gridline down the lanes plus a time label on top. -->
      <template v-for="t in ticks" :key="t.pct">
        <div
          class="flamegraph__tick-line"
          :style="{ left: t.pct + '%', top: (PAD_TOP - 4) + 'px', height: (trackHeight - PAD_TOP + 4) + 'px' }"
        />
        <span class="flamegraph__tick-label" :style="tickLabelStyle(t.pct)">{{ t.label }}</span>
      </template>

      <!-- Span blocks, positioned in compressed visual space (leftPct/widthPct). -->
      <div
        v-for="(b, i) in layout.blocks"
        :key="i"
        class="flamegraph__block"
        :style="{ left: b.leftPct + '%', width: b.widthPct + '%', top: blockY(b.lane) + 'px', height: LANE_H + 'px', background: b.color }"
        @mouseenter="(e) => showTip(e, b)"
        @mouseleave="hideTip"
      >
        <span class="flamegraph__label">{{ b.label }}</span>
      </div>
    </div>
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
.flamegraph__track { position: relative; width: 100%; }
.flamegraph__tick-line {
  position: absolute;
  width: 1px;
  background: var(--rule);
  pointer-events: none;
}
.flamegraph__tick-label {
  position: absolute;
  top: 2px;
  font-size: 9px;
  color: var(--text-muted);
  font-family: var(--font-mono);
  white-space: nowrap;
  pointer-events: none;
}
.flamegraph__gap {
  position: absolute;
  background: repeating-linear-gradient(
    120deg,
    transparent,
    transparent 3px,
    var(--border) 3px,
    var(--border) 4px
  );
  opacity: 0.4;
  pointer-events: none;
}
.flamegraph__block {
  position: absolute;
  border-radius: 2px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  padding: 0 4px;
  overflow: hidden;
  cursor: default;
}
.flamegraph__label {
  font-size: 10px;
  color: #fff;
  font-family: var(--font-sans);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  pointer-events: none;
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
