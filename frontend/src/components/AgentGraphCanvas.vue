<script setup lang="ts">
import { computed, ref } from 'vue';
import type { Flow, FlowStep, FlowTopology } from '../api/types';

// AgentGraphCanvas renders a flow as a read-only agent-graph: one node per
// step (labelled with the step's agent), order edges between consecutive
// stages, and parallel-group steps drawn as siblings in the same stage. It is
// a focused, self-contained SVG renderer rather than a reuse of the Map's
// GraphCanvas, which is bound to the spec/task Graph model (computeLayout,
// edgePaths, nodeColors, actions). The SVG + curved-edge pattern follows
// GraphCanvas; the layout is the flow's stage order.
// `editable` turns on per-node edit affordances (a remove control). The page
// owns the draft; the canvas only emits intent, keyed by the step's agent_slug
// (the flow's unique wiring key), so the renderer stays state-free.
const props = withDefaults(defineProps<{ flow: Flow | null; editable?: boolean }>(), {
  editable: false,
});
const emit = defineEmits<{
  (e: 'remove', agentSlug: string): void;
  (e: 'parallel', payload: { from: string; to: string }): void;
  (e: 'ungroup', agentSlug: string): void;
  (e: 'reorder', payload: { slug: string; toStage: number }): void;
  (e: 'editAgent', agentSlug: string): void;
}>();

// Node dragging uses pointer events rather than HTML5 drag: SVG elements do not
// fire dragstart reliably across browsers, and a pointer model also generalises
// to reordering. Dropping a node ON another emits `parallel` (group them into
// one stage); dropping it in a gap between stages emits `reorder` (move its
// stage). Hit-testing is by elementFromPoint on the data attributes, so the
// rendered geometry stays the single source of truth. Nodes are drawn above the
// gaps, so a node wins when the pointer is over one.
const draggingSlug = ref<string | null>(null);
const dropTargetSlug = ref<string | null>(null);
const dropGapIndex = ref<number | null>(null);

function onNodePointerDown(slug: string) {
  if (!props.editable || !slug) return;
  draggingSlug.value = slug;
  dropTargetSlug.value = null;
  dropGapIndex.value = null;
}
type Hit = { kind: 'node'; slug: string } | { kind: 'gap'; index: number } | null;
function hitAtPoint(x: number, y: number): Hit {
  const el = document.elementFromPoint(x, y);
  const node = el?.closest('[data-node-slug]');
  const slug = node?.getAttribute('data-node-slug');
  if (slug) return { kind: 'node', slug };
  const gap = el?.closest('[data-gap-index]');
  if (gap) return { kind: 'gap', index: Number(gap.getAttribute('data-gap-index')) };
  return null;
}
function onCanvasPointerMove(e: PointerEvent) {
  if (!draggingSlug.value) return;
  const hit = hitAtPoint(e.clientX, e.clientY);
  if (hit?.kind === 'node' && hit.slug !== draggingSlug.value) {
    dropTargetSlug.value = hit.slug;
    dropGapIndex.value = null;
  } else if (hit?.kind === 'gap') {
    dropGapIndex.value = hit.index;
    dropTargetSlug.value = null;
  } else {
    dropTargetSlug.value = null;
    dropGapIndex.value = null;
  }
}
function onCanvasPointerUp() {
  if (draggingSlug.value && dropTargetSlug.value) {
    emit('parallel', { from: draggingSlug.value, to: dropTargetSlug.value });
  } else if (draggingSlug.value && dropGapIndex.value !== null) {
    emit('reorder', { slug: draggingSlug.value, toStage: dropGapIndex.value });
  }
  draggingSlug.value = null;
  dropTargetSlug.value = null;
  dropGapIndex.value = null;
}

// Geometry. A stage is a column; parallel steps stack as rows within it.
const NODE_W = 156;
const NODE_H = 48;
const COL_PITCH = 208;
const ROW_PITCH = 70;
const PAD = 24;

interface LayoutNode {
  key: string;
  // agent_slug of the step this node renders; empty for the synthetic entry
  // ("Task") node, which is not a step and so is not removable.
  slug: string;
  // parallel is true when this step shares its stage with at least one sibling,
  // which is when the ungroup affordance applies.
  parallel: boolean;
  label: string;
  optional: boolean;
  x: number;
  y: number;
  w: number;
  h: number;
}
interface LayoutEdge {
  key: string;
  d: string;
}
// A label drawn above a stage that runs more than one agent concurrently, so
// the sibling rows read as a parallel group rather than disconnected nodes.
interface StageBadge {
  key: string;
  x: number;
  y: number;
}
// A reorder drop zone occupying the gap before a stage. `index` is the stage
// insertion position (0..stageCount). Only rendered while a node is dragging.
interface GapZone {
  index: number;
  x: number;
  y: number;
  w: number;
  h: number;
}
interface Layout {
  nodes: LayoutNode[];
  entry: LayoutNode | null;
  edges: LayoutEdge[];
  badges: StageBadge[];
  gaps: GapZone[];
  width: number;
  height: number;
}

const steps = computed<FlowStep[]>(() => props.flow?.steps ?? []);

// groupParallel clusters steps into parallel stages by transitive closure on
// run_in_parallel_with, matching the flow engine's grouping. Re-implemented
// here (not shared from FlowsPage) so this renderer stays self-contained.
function groupParallel(list: FlowStep[]): FlowStep[][] {
  const bySlug: Record<string, number> = {};
  list.forEach((s, i) => {
    bySlug[s.agent_slug] = i;
  });
  const adj: number[][] = list.map((s) => {
    const peers: number[] = [];
    (s.run_in_parallel_with || []).forEach((p) => {
      const j = bySlug[p];
      if (typeof j === 'number' && j !== bySlug[s.agent_slug]) peers.push(j);
    });
    return peers;
  });
  const assigned = list.map(() => -1);
  const groups: FlowStep[][] = [];
  list.forEach((_, i) => {
    if (assigned[i] !== -1) return;
    const gid = groups.length;
    const queue = [i];
    assigned[i] = gid;
    const members = [i];
    while (queue.length) {
      const cur = queue.shift()!;
      adj[cur].forEach((n) => {
        if (assigned[n] === -1) {
          assigned[n] = gid;
          members.push(n);
          queue.push(n);
        }
      });
    }
    members.sort((a, b) => a - b);
    groups.push(members.map((idx) => list[idx]));
  });
  return groups;
}

const groups = computed<FlowStep[][]>(() => groupParallel(steps.value));

function stepLabel(s: FlowStep): string {
  return s.agent_name || s.agent_slug || '(unset)';
}

// edgePath draws a curved cubic bezier from the right edge of a source node to
// the left edge of a target node, the same hand-rolled shape GraphCanvas uses.
function edgePath(a: LayoutNode, b: LayoutNode): string {
  const sx = a.x + a.w;
  const sy = a.y + a.h / 2;
  const tx = b.x;
  const ty = b.y + b.h / 2;
  const mx = (sx + tx) / 2;
  return `M ${sx} ${sy} C ${mx} ${sy}, ${mx} ${ty}, ${tx} ${ty}`;
}

const layout = computed<Layout>(() => {
  const gs = groups.value;
  if (gs.length === 0) {
    return { nodes: [], entry: null, edges: [], badges: [], gaps: [], width: 0, height: 0 };
  }
  const maxRows = Math.max(1, ...gs.map((g) => g.length));
  const height = PAD * 2 + maxRows * ROW_PITCH - (ROW_PITCH - NODE_H);
  const columns = gs.length + 1; // +1 for the Task entry column
  const baseWidth = PAD * 2 + columns * NODE_W + (columns - 1) * (COL_PITCH - NODE_W);
  // In edit mode reserve a trailing slot so a stage can be dropped after the
  // last one; read-only width is unchanged.
  const width = props.editable ? baseWidth + (COL_PITCH - NODE_W) : baseWidth;

  function columnX(col: number): number {
    return PAD + col * COL_PITCH;
  }
  // Vertically centre a column of `count` rows within the canvas height.
  function rowY(count: number, row: number): number {
    const blockH = count * ROW_PITCH - (ROW_PITCH - NODE_H);
    const startY = (height - blockH) / 2;
    return startY + row * ROW_PITCH;
  }

  const entry: LayoutNode = {
    key: 'entry',
    slug: '',
    parallel: false,
    label: 'Task',
    optional: false,
    x: columnX(0),
    y: rowY(1, 0),
    w: NODE_W,
    h: NODE_H,
  };

  const stageNodes: LayoutNode[][] = gs.map((group, ci) =>
    group.map((step, ri) => ({
      key: `${ci}:${ri}:${step.agent_slug}`,
      slug: step.agent_slug,
      parallel: group.length > 1,
      label: stepLabel(step),
      optional: !!step.optional,
      x: columnX(ci + 1),
      y: rowY(group.length, ri),
      w: NODE_W,
      h: NODE_H,
    })),
  );

  const edges: LayoutEdge[] = [];
  // Entry -> first stage.
  for (const n of stageNodes[0] ?? []) {
    edges.push({ key: `e:entry:${n.key}`, d: edgePath(entry, n) });
  }
  // Consecutive stages, fully connected to convey order between stages.
  for (let i = 0; i < stageNodes.length - 1; i++) {
    for (const a of stageNodes[i]) {
      for (const b of stageNodes[i + 1]) {
        edges.push({ key: `e:${a.key}:${b.key}`, d: edgePath(a, b) });
      }
    }
  }

  // A "parallel" label above each stage that runs concurrent siblings.
  const badges: StageBadge[] = [];
  stageNodes.forEach((col, ci) => {
    if (col.length <= 1) return;
    const topY = Math.min(...col.map((n) => n.y));
    badges.push({ key: `b:${ci}`, x: columnX(ci + 1) + NODE_W / 2, y: topY - 10 });
  });

  // Reorder gaps (edit mode only): one before each stage plus one after the
  // last. Gap k sits between canvas column k and k+1 (column 0 is the entry).
  const gaps: GapZone[] = [];
  if (props.editable) {
    for (let k = 0; k <= gs.length; k++) {
      gaps.push({
        index: k,
        x: columnX(k) + NODE_W,
        y: PAD / 2,
        w: COL_PITCH - NODE_W,
        h: height - PAD,
      });
    }
  }

  return { nodes: stageNodes.flat(), entry, edges, badges, gaps, width, height };
});

// Topology indicator. Only an agentic + dynamic flow carries a topology;
// pinned flows render as a deterministic chain. The fields are optional on the
// wire (see api/types.ts), so this reads defensively.
const topologyLabel = computed<string>(() => {
  const f = props.flow;
  if (!f?.agentic) return 'chain';
  if (!f.dynamic) return 'chain';
  const t: FlowTopology | undefined = f.topology;
  return t === 'mesh' ? 'mesh' : 'chain';
});
const showTopology = computed(() => !!props.flow?.agentic && !!props.flow?.dynamic);
</script>

<template>
  <div class="agc">
    <div v-if="!flow" class="agc__empty">Select a flow to view its agent-graph.</div>
    <div v-else-if="steps.length === 0" class="agc__empty">
      This flow has no steps.
    </div>
    <template v-else>
      <div class="agc__meta">
        <span v-if="showTopology" class="agc__topology" :class="`agc__topology--${topologyLabel}`">
          {{ topologyLabel === 'mesh' ? 'Dynamic mesh' : 'Dynamic chain' }}
        </span>
        <span v-else class="agc__topology agc__topology--pinned">Pinned chain</span>
      </div>
      <svg
        class="agc__svg"
        :class="{ 'agc__svg--dragging': !!draggingSlug }"
        :viewBox="`0 0 ${layout.width} ${layout.height}`"
        :width="layout.width"
        :height="layout.height"
        role="img"
        aria-label="Flow agent-graph"
        @pointermove="onCanvasPointerMove"
        @pointerup="onCanvasPointerUp"
        @pointerleave="onCanvasPointerUp"
      >
        <g class="agc__edges">
          <path
            v-for="edge in layout.edges"
            :key="edge.key"
            class="agc-edge"
            :d="edge.d"
            fill="none"
          />
        </g>

        <!-- Reorder drop zones, visible only during a node drag. Rendered
             before the nodes so a node wins the elementFromPoint hit-test. -->
        <g v-if="draggingSlug" class="agc__gaps">
          <rect
            v-for="gap in layout.gaps"
            :key="`gap:${gap.index}`"
            class="agc-gap"
            :class="{ 'agc-gap--active': dropGapIndex === gap.index }"
            :data-gap-index="gap.index"
            :x="gap.x"
            :y="gap.y"
            :width="gap.w"
            :height="gap.h"
            rx="6"
          />
        </g>

        <g class="agc__badges">
          <text
            v-for="badge in layout.badges"
            :key="badge.key"
            class="agc-badge"
            :x="badge.x"
            :y="badge.y"
            text-anchor="middle"
            dominant-baseline="central"
          >parallel</text>
        </g>

        <g v-if="layout.entry" class="agc__entry">
          <rect
            class="agc-entry-box"
            :x="layout.entry.x"
            :y="layout.entry.y"
            :width="layout.entry.w"
            :height="layout.entry.h"
            rx="10"
          />
          <text
            class="agc-entry-text"
            :x="layout.entry.x + layout.entry.w / 2"
            :y="layout.entry.y + layout.entry.h / 2"
            text-anchor="middle"
            dominant-baseline="central"
          >Task</text>
        </g>

        <g class="agc__nodes">
          <g
            v-for="node in layout.nodes"
            :key="node.key"
            class="agc-node"
            :class="{
              'agc-node--optional': node.optional,
              'agc-node--editable': editable,
              'agc-node--dragging': draggingSlug === node.slug,
              'agc-node--drop-target': dropTargetSlug === node.slug,
            }"
            :data-node-slug="node.slug || null"
            @pointerdown="editable && onNodePointerDown(node.slug)"
            @dblclick="node.slug && emit('editAgent', node.slug)"
          >
            <rect
              class="agc-node-box"
              :x="node.x"
              :y="node.y"
              :width="node.w"
              :height="node.h"
              rx="10"
            />
            <text
              class="agc-node-text"
              :x="node.x + node.w / 2"
              :y="node.y + (node.optional ? node.h / 2 - 6 : node.h / 2)"
              text-anchor="middle"
              dominant-baseline="central"
            >{{ node.label }}</text>
            <text
              v-if="node.optional"
              class="agc-node-tag"
              :x="node.x + node.w / 2"
              :y="node.y + node.h / 2 + 11"
              text-anchor="middle"
              dominant-baseline="central"
            >optional</text>

            <!-- Remove control, edit mode only. Sits on the node's top-right
                 corner; emits the step's agent_slug for the page to splice.
                 pointerdown is stopped so grabbing it does not start a drag. -->
            <g
              v-if="editable && node.slug"
              class="agc-node-remove"
              role="button"
              :aria-label="`Remove ${node.label}`"
              tabindex="0"
              @pointerdown.stop
              @click="emit('remove', node.slug)"
              @keydown.enter="emit('remove', node.slug)"
            >
              <title>Remove step</title>
              <circle
                class="agc-node-remove-bg"
                :cx="node.x + node.w - 9"
                :cy="node.y + 9"
                r="9"
              />
              <text
                class="agc-node-remove-x"
                :x="node.x + node.w - 9"
                :y="node.y + 9"
                text-anchor="middle"
                dominant-baseline="central"
              >&#215;</text>
            </g>

            <!-- Ungroup control, edit mode + parallel nodes only. Top-left
                 corner; pulls the step out of its parallel stage. -->
            <g
              v-if="editable && node.slug && node.parallel"
              class="agc-node-ungroup"
              role="button"
              :aria-label="`Make ${node.label} sequential`"
              tabindex="0"
              @pointerdown.stop
              @click="emit('ungroup', node.slug)"
              @keydown.enter="emit('ungroup', node.slug)"
            >
              <title>Make sequential (remove from parallel group)</title>
              <circle
                class="agc-node-ungroup-bg"
                :cx="node.x + 9"
                :cy="node.y + 9"
                r="9"
              />
              <text
                class="agc-node-ungroup-icon"
                :x="node.x + 9"
                :y="node.y + 9"
                text-anchor="middle"
                dominant-baseline="central"
              >&#8676;</text>
            </g>
          </g>
        </g>
      </svg>
    </template>
  </div>
</template>

<style scoped>
.agc {
  width: 100%;
  height: 100%;
  overflow: auto;
  padding: 0.5rem;
}
.agc__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 8rem;
  color: var(--text-secondary);
  font-size: 0.85rem;
}
.agc__meta {
  margin: 0 0 0.6rem;
}
.agc__topology {
  display: inline-block;
  font-size: 0.68rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  color: var(--text-muted);
  background: var(--bg-hover);
}
.agc__topology--mesh {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.agc__svg {
  display: block;
  max-width: none;
}
.agc-edge {
  stroke: var(--border-strong, var(--border));
  stroke-width: 1.5;
}
.agc-entry-box {
  fill: var(--bg-sunk);
  stroke: var(--border);
  stroke-width: 1;
  stroke-dasharray: 4 3;
}
.agc-entry-text {
  fill: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 600;
}
.agc-node-box {
  fill: var(--bg-elevated);
  stroke: var(--border);
  stroke-width: 1.2;
}
.agc-node--optional .agc-node-box {
  stroke-dasharray: 5 3;
}
.agc-node-text {
  fill: var(--text);
  font-size: 0.8rem;
  font-weight: 600;
}
.agc-node-tag {
  fill: var(--text-muted);
  font-size: 0.62rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.agc-badge {
  fill: var(--text-muted);
  font-size: 0.62rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.agc-gap {
  fill: var(--accent);
  opacity: 0.06;
  stroke: var(--accent);
  stroke-width: 1;
  stroke-dasharray: 4 3;
}
.agc-gap--active {
  opacity: 0.22;
}
.agc-node--editable {
  cursor: grab;
}
.agc__svg--dragging .agc-node--editable {
  cursor: grabbing;
}
.agc-node--dragging .agc-node-box {
  opacity: 0.5;
}
.agc-node--drop-target .agc-node-box {
  stroke: var(--accent);
  stroke-width: 2;
  fill: color-mix(in srgb, var(--accent) 12%, var(--bg-elevated));
}
.agc-node-remove,
.agc-node-ungroup {
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.1s ease;
}
.agc-node:hover .agc-node-remove,
.agc-node:hover .agc-node-ungroup,
.agc-node-remove:focus-visible,
.agc-node-ungroup:focus-visible {
  opacity: 1;
}
.agc-node-ungroup-bg {
  fill: var(--bg-elevated);
  stroke: var(--accent);
  stroke-width: 1.2;
}
.agc-node-ungroup:hover .agc-node-ungroup-bg {
  fill: var(--accent);
}
.agc-node-ungroup-icon {
  fill: var(--accent);
  font-size: 0.74rem;
  font-weight: 700;
  pointer-events: none;
}
.agc-node-ungroup:hover .agc-node-ungroup-icon {
  fill: #fff;
}
.agc-node-remove-bg {
  fill: var(--bg-elevated);
  stroke: var(--danger, #d2453f);
  stroke-width: 1.2;
}
.agc-node-remove:hover .agc-node-remove-bg {
  fill: var(--danger, #d2453f);
}
.agc-node-remove-x {
  fill: var(--danger, #d2453f);
  font-size: 0.8rem;
  font-weight: 700;
  pointer-events: none;
}
.agc-node-remove:hover .agc-node-remove-x {
  fill: #fff;
}
</style>
