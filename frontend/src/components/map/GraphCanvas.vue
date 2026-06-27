<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue';
import type { Graph } from '../../api/types';
import { computeLayout, type Point } from './layout';
import { edgePaths } from './edges';
import { DragBatcher } from './dragController';

const props = defineProps<{
  graph: Graph;
  selectedId?: string | null;
}>();
const emit = defineEmits<{
  (e: 'select', id: string): void;
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
watch(() => props.graph, relayout, { immediate: true });

const posMap = computed(() => new Map(Object.entries(pos)));
const edges = computed(() => edgePaths(props.graph, posMap.value));

const criticalSet = computed(() => new Set(props.graph.critical_path));
const blockedSet = computed(() => new Set(props.graph.blocked));

function nodeClasses(id: string, kind: string, status: string) {
  return [
    'gc-node',
    `gc-node--${kind}`,
    `gc-status--${status}`,
    { 'gc-node--critical': criticalSet.value.has(id) },
    { 'gc-node--blocked': blockedSet.value.has(id) },
    { 'gc-node--selected': props.selectedId === id },
  ];
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
        >
          <rect class="gc-node__box" x="-80" y="-22" width="160" height="44" rx="8" />
          <text class="gc-node__label" x="0" y="-2" text-anchor="middle">{{ n.label }}</text>
          <text class="gc-node__status" x="0" y="13" text-anchor="middle">{{ n.status }}</text>
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
  background:
    radial-gradient(circle, var(--border, #2a2a2a) 1px, transparent 1px) 0 0 / 24px 24px;
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
.gc-node__box {
  fill: var(--surface-2, #1c1c1c);
  stroke: var(--col-backlog, #8e8a80);
  stroke-width: 1.5;
}
.gc-node__label {
  fill: var(--text, #e8e8e8);
  font-size: 12px;
  font-weight: 600;
}
.gc-node__status {
  fill: var(--text-muted, #9a9a9a);
  font-size: 10px;
}
/* Task status accents (reuse board tokens). */
.gc-status--in_progress .gc-node__box { stroke: var(--col-progress, #3a6db3); }
.gc-status--waiting .gc-node__box { stroke: var(--col-waiting, #a56a12); }
.gc-status--done .gc-node__box { stroke: var(--col-done, #3f7a4a); }
.gc-status--failed .gc-node__box { stroke: var(--col-failed, #b3433a); }
/* Spec lifecycle accents. */
.gc-status--validated .gc-node__box { stroke: var(--col-progress, #3a6db3); }
.gc-status--complete .gc-node__box { stroke: var(--col-done, #3f7a4a); }
.gc-status--stale .gc-node__box { stroke: var(--col-waiting, #a56a12); }

.gc-node--selected .gc-node__box {
  stroke-width: 2.5;
  filter: drop-shadow(0 0 4px var(--accent, #6f9bd8));
}
.gc-node--critical .gc-node__box {
  stroke-dasharray: none;
}
.gc-node--blocked {
  opacity: 0.5;
}

.gc-edge {
  stroke: var(--border-strong, #555);
  stroke-width: 1.5;
}
.gc-edge--containment { stroke-dasharray: 2 3; opacity: 0.6; }
.gc-edge--dispatch { stroke: var(--col-progress, #3a6db3); }
.gc-edge--spec_dep { stroke: var(--col-backlog, #8e8a80); }
.gc-edge--task_dep { stroke: var(--text-muted, #9a9a9a); }
.gc-edge--critical {
  stroke-width: 2.5;
  stroke: var(--accent, #6f9bd8);
}
.gc-arrowhead { fill: var(--border-strong, #555); }
.gc-arrowhead--dispatch { fill: var(--col-progress, #3a6db3); }
</style>
