<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue';
import type { Graph } from '../../api/types';
import { computeLayout, type Point } from './layout';
import { edgePaths } from './edges';
import { DragBatcher } from './dragController';
import { stateColor } from './nodeColors';

const props = defineProps<{
  graph: Graph;
  selectedId?: string | null;
}>();
const emit = defineEmits<{
  (e: 'select', id: string): void;
  (e: 'open', id: string): void;
}>();

// Live node positions, seeded from the layered layout and then mutated in place
// during drags. A plain reactive record (not a Map) so Vue tracks per-node
// updates and the edge `computed` recomputes on each drag flush.
const pos = reactive<Record<string, Point>>({});

function relayout() {
  const next = computeLayout(props.graph);
  for (const k of Object.keys(pos)) delete pos[k];
  for (const [id, p] of next) pos[id] = { x: p.x, y: p.y };
}
// Relayout only when the node/edge *structure* changes — not when a task's
// status flips. A live run streams status updates constantly; relaying out on
// each would discard drag positions and prevent any animation. Status-only
// changes flow through reactively (node fill/classes) with positions intact.
function structuralKey(): string {
  const g = props.graph;
  return g.nodes.map((n) => n.id).join(',') + '|' + g.edges.map((e) => `${e.from}>${e.to}`).join(',');
}
watch(structuralKey, relayout, { immediate: true });

const posMap = computed(() => new Map(Object.entries(pos)));
const edges = computed(() => edgePaths(props.graph, posMap.value));

const criticalSet = computed(() => new Set(props.graph.critical_path));
const blockedSet = computed(() => new Set(props.graph.blocked));

const readySet = computed(
  () => new Set(props.graph.nodes.filter((n) => (n.available_actions?.length ?? 0) > 0).map((n) => n.id)),
);

// Live execution states drive the animation layer.
function isRunning(status: string) {
  return status === 'in_progress' || status === 'committing';
}
function isWaiting(status: string) {
  return status === 'waiting';
}

function nodeClasses(id: string, kind: string, status: string) {
  return [
    'gc-node',
    `gc-node--${kind}`,
    `gc-status--${status}`,
    { 'gc-node--critical': criticalSet.value.has(id) },
    { 'gc-node--blocked': blockedSet.value.has(id) },
    { 'gc-node--ready': readySet.value.has(id) },
    { 'gc-node--selected': props.selectedId === id },
    { 'gc-node--running': isRunning(status) },
    { 'gc-node--waiting-live': isWaiting(status) },
    // "You are here": a running node that sits on the critical path is where the
    // pipeline currently is.
    { 'gc-node--here': isRunning(status) && criticalSet.value.has(id) },
  ];
}
// SVG <text> neither wraps nor ellipsises, so a title is greedily wrapped into
// up to maxLines lines (so most titles show in full) and only the final line is
// truncated when even that overflows. Memoized per label since the template
// reads it a few times per node across hundreds of nodes.
const LABEL_MAX_CHARS = 20;
const LABEL_MAX_LINES = 2;
const lineCache = new Map<string, string[]>();
function labelLines(label: string): string[] {
  const hit = lineCache.get(label);
  if (hit) return hit;
  const words = label.split(/\s+/).filter(Boolean);
  const lines: string[] = [];
  let cur = '';
  for (let w of words) {
    while (w.length > LABEL_MAX_CHARS) {
      // hard-break an over-long single token
      if (cur) { lines.push(cur); cur = ''; }
      if (lines.length >= LABEL_MAX_LINES) break;
      lines.push(w.slice(0, LABEL_MAX_CHARS));
      w = w.slice(LABEL_MAX_CHARS);
    }
    if (lines.length >= LABEL_MAX_LINES && !cur) break;
    const cand = cur ? cur + ' ' + w : w;
    if (cand.length <= LABEL_MAX_CHARS) {
      cur = cand;
    } else {
      lines.push(cur);
      cur = w;
      if (lines.length >= LABEL_MAX_LINES) break;
    }
  }
  if (cur && lines.length < LABEL_MAX_LINES) lines.push(cur);
  const out = lines.slice(0, LABEL_MAX_LINES);
  // Ellipsise the last line if any content was dropped.
  const shown = out.join(' ');
  if (shown.length < label.replace(/\s+/g, ' ').length && out.length) {
    const i = out.length - 1;
    out[i] = out[i].slice(0, LABEL_MAX_CHARS - 1).trimEnd() + '…';
  }
  lineCache.set(label, out);
  return out;
}

function edgeClasses(kind: string, from: string, to: string) {
  const onPath = criticalSet.value.has(from) && criticalSet.value.has(to);
  return ['gc-edge', `gc-edge--${kind}`, { 'gc-edge--critical': onPath }];
}

// --- pan / zoom ---
const tx = ref(0);
const ty = ref(0);
const scale = ref(1);
const transform = computed(() => `translate(${tx.value} ${ty.value}) scale(${scale.value})`);

const spaceHeld = ref(false);
function onKeyDown(e: KeyboardEvent) {
  if (e.code === 'Space') spaceHeld.value = true;
}
function onKeyUp(e: KeyboardEvent) {
  if (e.code === 'Space') spaceHeld.value = false;
}

function onWheel(e: WheelEvent) {
  if (!(e.ctrlKey || e.metaKey)) return; // only Ctrl/⌘+scroll zooms
  e.preventDefault();
  const factor = e.deltaY < 0 ? 1.1 : 1 / 1.1;
  scale.value = Math.min(2.5, Math.max(0.3, scale.value * factor));
}

// --- drag: node move (RAF-batched) and canvas pan ---
type DragKind = 'node' | 'pan';
interface DragState {
  kind: DragKind;
  id?: string;
  startX: number;
  startY: number;
  origX: number;
  origY: number;
}
let drag: DragState | null = null;
const batcher = new DragBatcher((id, p) => {
  pos[id] = p; // single write per frame; edges computed re-aims from this
});

function onNodePointerDown(e: PointerEvent, id: string) {
  if (spaceHeld.value) return; // space means pan, not node drag
  e.stopPropagation();
  (e.target as Element).setPointerCapture?.(e.pointerId);
  drag = { kind: 'node', id, startX: e.clientX, startY: e.clientY, origX: pos[id].x, origY: pos[id].y };
}
function onCanvasPointerDown(e: PointerEvent) {
  if (!spaceHeld.value) return;
  drag = { kind: 'pan', startX: e.clientX, startY: e.clientY, origX: tx.value, origY: ty.value };
}
function onPointerMove(e: PointerEvent) {
  if (!drag) return;
  const dx = (e.clientX - drag.startX) / scale.value;
  const dy = (e.clientY - drag.startY) / scale.value;
  if (drag.kind === 'node' && drag.id) {
    batcher.schedule(drag.id, { x: drag.origX + dx, y: drag.origY + dy });
  } else {
    tx.value = drag.origX + (e.clientX - drag.startX);
    ty.value = drag.origY + (e.clientY - drag.startY);
  }
}
function onPointerUp() {
  drag = null;
}

function resetView() {
  tx.value = 0;
  ty.value = 0;
  scale.value = 1;
  relayout();
}
defineExpose({ resetView });
</script>

<template>
  <div
    class="gc-canvas"
    tabindex="0"
    :class="{ 'gc-canvas--panning': spaceHeld }"
    @keydown="onKeyDown"
    @keyup="onKeyUp"
    @wheel="onWheel"
    @pointerdown="onCanvasPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @pointerleave="onPointerUp"
  >
    <svg class="gc-svg" width="100%" height="100%">
      <defs>
        <marker
          v-for="kind in ['containment', 'dispatch', 'spec_dep', 'task_dep']"
          :id="`gc-arrow-${kind}`"
          :key="kind"
          viewBox="0 0 8 8"
          refX="7"
          refY="4"
          markerWidth="7"
          markerHeight="7"
          orient="auto-start-reverse"
        >
          <path d="M0,0 L8,4 L0,8 z" :class="`gc-arrowhead gc-arrowhead--${kind}`" />
        </marker>
      </defs>
      <g :transform="transform">
        <path
          v-for="(e, i) in edges"
          :key="`e${i}`"
          :d="e.d"
          :class="edgeClasses(e.kind, e.from, e.to)"
          :marker-end="`url(#gc-arrow-${e.kind})`"
          fill="none"
        />
        <g
          v-for="n in graph.nodes"
          :key="n.id"
          :transform="`translate(${pos[n.id]?.x ?? 0} ${pos[n.id]?.y ?? 0})`"
          :class="nodeClasses(n.id, n.kind, n.status)"
          @pointerdown="onNodePointerDown($event, n.id)"
          @click.stop="emit('select', n.id)"
          @dblclick.stop="emit('open', n.id)"
        >
          <title>{{ n.label }} · {{ n.status }}</title>
          <circle
            v-if="isRunning(n.status) || isWaiting(n.status)"
            class="gc-pulse"
            :class="isWaiting(n.status) ? 'gc-pulse--waiting' : 'gc-pulse--running'"
            :stroke="stateColor(n.status)"
            :r="n.kind === 'task' ? 9 : 11"
            fill="none"
          />
          <circle class="gc-dot" :fill="stateColor(n.status)" :r="n.kind === 'task' ? 9 : 11" />
          <text class="gc-node__label" text-anchor="middle">
            <tspan v-for="(ln, i) in labelLines(n.label)" :key="i" x="0" :y="24 + i * 13">{{ ln }}</tspan>
          </text>
        </g>
      </g>
    </svg>
  </div>
</template>

<style scoped>
.gc-canvas {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  outline: none;
  user-select: none;
  -webkit-user-select: none;
  background:
    radial-gradient(circle, var(--rule, #d9d3c5) 1px, transparent 1px) 0 0 / 24px 24px;
  background-color: var(--bg, #f4f1ea);
}
.gc-canvas--panning {
  cursor: grab;
}
.gc-svg {
  display: block;
}
.gc-node {
  cursor: pointer;
}
/* Network-style node: a state-colored disc with the title below it. */
.gc-dot {
  stroke: var(--bg, #f4f1ea);
  stroke-width: 2;
  transition: r 80ms ease;
}
.gc-node__label {
  font-size: 11px;
  font-weight: 600;
  fill: var(--ink, #1b1916);
  /* Halo so the label stays legible over edges and the dot grid. */
  paint-order: stroke;
  stroke: var(--bg, #f4f1ea);
  stroke-width: 3px;
  stroke-linejoin: round;
}

.gc-node--selected .gc-dot {
  stroke: var(--accent, #c45a33);
  stroke-width: 3;
  filter: drop-shadow(0 0 5px var(--accent-soft, #f3dccf));
}
.gc-node--blocked {
  opacity: 0.45;
}
/* "Actionable now": a node the backend marked with an available action gets an
   accent ring so the operator can spot what's ready to dispatch/start. */
.gc-node--ready .gc-dot {
  stroke: var(--accent, #c45a33);
  stroke-width: 3;
}

/* --- live execution animation --- */
/* An expanding ring radiates from a running/waiting node so a glance shows what
   is currently executing. */
.gc-pulse {
  stroke-width: 2;
  transform-box: fill-box;
  transform-origin: center;
  pointer-events: none;
}
.gc-pulse--running {
  animation: gc-pulse 1.6s ease-out infinite;
}
.gc-pulse--waiting {
  animation: gc-pulse 2.4s ease-out infinite;
  opacity: 0.7;
}
@keyframes gc-pulse {
  0% { transform: scale(1); opacity: 0.6; }
  100% { transform: scale(2.6); opacity: 0; }
}
/* The running disc itself breathes; a waiting one holds a steady amber ring. */
.gc-node--running .gc-dot {
  animation: gc-breathe 1.6s ease-in-out infinite;
}
@keyframes gc-breathe {
  0%, 100% { transform: scale(1); }
  50% { transform: scale(1.18); }
}
.gc-node--running .gc-dot,
.gc-node--waiting-live .gc-dot {
  transform-box: fill-box;
  transform-origin: center;
}
/* "You are here": the running node on the critical path gets a bold accent ring
   plus the breathing disc, so the eye lands on where the pipeline is now. */
.gc-node--here .gc-dot {
  stroke: var(--accent, #c45a33);
  stroke-width: 3;
}

@media (prefers-reduced-motion: reduce) {
  .gc-pulse,
  .gc-node--running .gc-dot {
    animation: none;
  }
  /* Keep a static ring so running nodes still read as active. */
  .gc-pulse--running,
  .gc-pulse--waiting {
    opacity: 0.5;
  }
}

.gc-edge {
  stroke: var(--border-strong, #c7c0af);
  stroke-width: 1.5;
}
.gc-edge--containment { stroke-dasharray: 2 3; opacity: 0.55; }
.gc-edge--dispatch { stroke: var(--col-progress, #3a6db3); }
.gc-edge--spec_dep { stroke: var(--col-backlog, #8e8a80); }
.gc-edge--task_dep { stroke: var(--ink-4, #97928a); }
.gc-edge--critical {
  stroke-width: 2.5;
  stroke: var(--accent, #c45a33);
}
.gc-arrowhead { fill: var(--border-strong, #c7c0af); }
.gc-arrowhead--dispatch { fill: var(--col-progress, #3a6db3); }
</style>
