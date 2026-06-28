<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { Flow } from '../api/types';
import { coordinationOf, type Coordination } from '../lib/flowDraft';

// AgentGraphCanvas renders a flow as an agent FLEET (unified-agent-graph-ui.md,
// the 2026-06-28 reframe). Nodes are agents; the first is the lead (the task's
// entry). Edges mean "can delegate to", derived from the coordination mode:
//   - lead:     the lead delegates to each member (orchestrator-worker).
//   - mesh:     the lead delegates and members also hand off among themselves.
//   - sequence: the simple case -- a fixed left-to-right chain (no delegation).
// A task enters at the lead and the fleet works it to an outcome. Editing emits
// intent keyed by agent_slug (remove, set-lead, edit-agent); the page owns the
// draft. Free-form node positioning is a follow-up; this lays the fleet out.
// runStatus overlays a run's lineage: agent_slug -> 'running' | 'done' |
// 'failed'. When present (a run is selected) the matching agent nodes are
// coloured by status, so a finished or in-flight run is visible on the same
// graph that authored it.
const props = withDefaults(
  defineProps<{ flow: Flow | null; editable?: boolean; runStatus?: Record<string, string> }>(),
  { editable: false, runStatus: undefined },
);
const emit = defineEmits<{
  (e: 'remove', agentSlug: string): void;
  (e: 'setLead', agentSlug: string): void;
  (e: 'editAgent', agentSlug: string): void;
}>();

const NODE_W = 160;
const NODE_H = 48;
const PAD = 28;
const COL = 214;
const ROW = 74;

// Free-form positions. Agents auto-lay-out by default, but the author can drag
// any agent anywhere; the override is keyed by agent_slug and persisted per
// fleet in localStorage (it is a visual arrangement, not part of the flow, so it
// does not touch the saved YAML). Task and Outcome stay anchored to the lead and
// the rightmost agent.
const svgRef = ref<SVGSVGElement | null>(null);
const freePos = ref<Record<string, { x: number; y: number }>>({});
const dragSlug = ref<string | null>(null);
const dragOffset = ref({ x: 0, y: 0 });

function posKey(slug: string): string {
  return `agc-pos:${slug}`;
}
function loadPositions(slug: string): Record<string, { x: number; y: number }> {
  try {
    return JSON.parse(localStorage.getItem(posKey(slug)) || '{}');
  } catch {
    return {};
  }
}
function savePositions() {
  const slug = props.flow?.slug;
  if (!slug) return;
  try {
    localStorage.setItem(posKey(slug), JSON.stringify(freePos.value));
  } catch {
    /* storage unavailable; positions stay in-memory only */
  }
}
watch(
  () => props.flow?.slug,
  (slug) => {
    freePos.value = slug ? loadPositions(slug) : {};
  },
  { immediate: true },
);

const mode = computed<Coordination>(() => coordinationOf(props.flow ?? {}));
const modeLabel = computed(
  () =>
    ({ lead: 'Lead delegates', mesh: 'Open mesh', sequence: 'Fixed sequence' })[mode.value],
);

interface Agent {
  slug: string;
  name: string;
  lead: boolean;
}
const agents = computed<Agent[]>(() =>
  (props.flow?.steps ?? []).map((s, i) => ({
    slug: s.agent_slug,
    name: s.agent_name || s.agent_slug || '(unset)',
    lead: i === 0,
  })),
);

// deterministicStages groups steps into ordered stages by the transitive closure
// of run_in_parallel_with: agents that run together (no ordering between them)
// land in one stage. This is the grouping the flow engine executes, so the
// drawing matches the run -- the parallel trio renders as one stage, not a line.
function deterministicStages(): Agent[][] {
  const steps = props.flow?.steps ?? [];
  const idx = new Map(steps.map((s, i) => [s.agent_slug, i]));
  const assigned = steps.map(() => -1);
  const stages: Agent[][] = [];
  steps.forEach((_, i) => {
    if (assigned[i] !== -1) return;
    const gid = stages.length;
    const queue = [i];
    assigned[i] = gid;
    const members = [i];
    while (queue.length) {
      const cur = queue.shift()!;
      for (const p of steps[cur].run_in_parallel_with ?? []) {
        const j = idx.get(p);
        if (j !== undefined && assigned[j] === -1) {
          assigned[j] = gid;
          members.push(j);
          queue.push(j);
        }
      }
    }
    members.sort((a, b) => a - b);
    stages.push(members.map((m) => ({ slug: steps[m].agent_slug, name: steps[m].agent_name || steps[m].agent_slug, lead: false })));
  });
  return stages;
}

type NodeKind = 'task' | 'agent' | 'outcome';
interface LNode {
  key: string;
  kind: NodeKind;
  slug: string;
  name: string;
  lead: boolean;
  member: boolean;
  x: number;
  y: number;
}
interface LEdge {
  key: string;
  d: string;
  delegate: boolean;
  mesh: boolean;
}
interface Layout {
  nodes: LNode[];
  edges: LEdge[];
  vx: number;
  vy: number;
  width: number;
  height: number;
}

// bezier draws the same hand-rolled curved edge GraphCanvas uses, from one point
// to another.
function bezier(ax: number, ay: number, bx: number, by: number): string {
  const mx = (ax + bx) / 2;
  return `M ${ax} ${ay} C ${mx} ${ay}, ${mx} ${by}, ${bx} ${by}`;
}
function colX(c: number): number {
  return PAD + c * COL;
}

const layout = computed<Layout>(() => {
  const ags = agents.value;
  const nodes: LNode[] = [];
  const edges: LEdge[] = [];
  if (ags.length === 0) return { nodes, edges, vx: 0, vy: 0, width: 0, height: 0 };

  // Deterministic ("fixed sequence"): Task -> stages -> Outcome, where a stage is
  // a set of agents that run together. Agents with no ordering between them (the
  // implement commit-msg / title / oversight trio) share a stage and render as a
  // parallel fan -- NOT a false linear chain. Edges mean "then / feeds".
  if (mode.value === 'sequence') {
    const stages = deterministicStages();
    const maxRows = Math.max(1, ...stages.map((s) => s.length));
    const height = PAD * 2 + maxRows * ROW - (ROW - NODE_H);
    const cols = stages.length + 2; // task + stages + outcome
    const width = PAD * 2 + cols * NODE_W + (cols - 1) * (COL - NODE_W);
    const rowY = (count: number, row: number): number => {
      const block = count * ROW - (ROW - NODE_H);
      return (height - block) / 2 + row * ROW;
    };
    const midY = (height - NODE_H) / 2;

    const taskN: LNode = { key: 'task', kind: 'task', slug: '', name: 'Task', lead: false, member: false, x: colX(0), y: midY };
    const stageNodes: LNode[][] = stages.map((stage, ci) =>
      stage.map((a, ri) => ({
        key: `a:${a.slug}`, kind: 'agent' as NodeKind, slug: a.slug, name: a.name,
        lead: false, member: false, x: colX(ci + 1), y: rowY(stage.length, ri),
      })),
    );
    const outN: LNode = { key: 'outcome', kind: 'outcome', slug: '', name: 'Outcome', lead: false, member: false, x: colX(cols - 1), y: midY };
    nodes.push(taskN, ...stageNodes.flat(), outN);

    // Task -> first stage; consecutive stages fully connected; last stage -> Outcome.
    for (const n of stageNodes[0] ?? []) {
      edges.push({ key: `e:task:${n.key}`, d: bezier(taskN.x + NODE_W, taskN.y + NODE_H / 2, n.x, n.y + NODE_H / 2), delegate: false, mesh: false });
    }
    for (let i = 0; i < stageNodes.length - 1; i++) {
      for (const a of stageNodes[i]) {
        for (const b of stageNodes[i + 1]) {
          edges.push({ key: `e:${a.key}:${b.key}`, d: bezier(a.x + NODE_W, a.y + NODE_H / 2, b.x, b.y + NODE_H / 2), delegate: false, mesh: false });
        }
      }
    }
    for (const n of stageNodes[stageNodes.length - 1] ?? []) {
      edges.push({ key: `e:out:${n.key}`, d: bezier(n.x + NODE_W, n.y + NODE_H / 2, outN.x, outN.y + NODE_H / 2), delegate: false, mesh: false });
    }
    return { nodes, edges, vx: 0, vy: 0, width, height };
  }

  // lead / mesh: free-form. Auto-layout (Lead | members) provides defaults; any
  // agent's stored position overrides it. Task and Outcome anchor to the lead
  // and the rightmost agent so the "enters / returns" reading survives a drag.
  const lead = ags[0];
  const members = ags.slice(1);
  const autoHeight = PAD * 2 + Math.max(1, members.length) * ROW - (ROW - NODE_H);
  const midY = (autoHeight - NODE_H) / 2;
  const rowY = (row: number): number => {
    const block = members.length * ROW - (ROW - NODE_H);
    return (autoHeight - block) / 2 + row * ROW;
  };
  const at = (slug: string, ax: number, ay: number) => freePos.value[slug] ?? { x: ax, y: ay };

  const leadP = at(lead.slug, colX(1), midY);
  const leadN: LNode = { key: `a:${lead.slug}`, kind: 'agent', slug: lead.slug, name: lead.name, lead: true, member: false, x: leadP.x, y: leadP.y };
  const memberNs: LNode[] = members.map((a, i) => {
    const p = at(a.slug, colX(2), rowY(i));
    return { key: `a:${a.slug}`, kind: 'agent', slug: a.slug, name: a.name, lead: false, member: true, x: p.x, y: p.y };
  });
  const agentNs = [leadN, ...memberNs];
  const maxX = Math.max(...agentNs.map((n) => n.x));

  const taskN: LNode = { key: 'task', kind: 'task', slug: '', name: 'Task', lead: false, member: false, x: leadN.x - COL, y: leadN.y };
  const outN: LNode = { key: 'outcome', kind: 'outcome', slug: '', name: 'Outcome', lead: false, member: false, x: maxX + COL, y: leadN.y };
  nodes.push(taskN, leadN, ...memberNs, outN);

  edges.push({ key: 'e:enter', d: bezier(taskN.x + NODE_W, taskN.y + NODE_H / 2, leadN.x, leadN.y + NODE_H / 2), delegate: false, mesh: false });
  edges.push({ key: 'e:outcome', d: bezier(leadN.x + NODE_W, leadN.y + NODE_H / 2, outN.x, outN.y + NODE_H / 2), delegate: false, mesh: false });
  memberNs.forEach((mn, i) =>
    edges.push({ key: `e:deleg:${i}`, d: bezier(leadN.x + NODE_W, leadN.y + NODE_H / 2, mn.x, mn.y + NODE_H / 2), delegate: true, mesh: false }),
  );
  if (mode.value === 'mesh') {
    for (let i = 0; i < memberNs.length - 1; i++) {
      const a = memberNs[i];
      const b = memberNs[i + 1];
      edges.push({ key: `e:mesh:${i}`, d: bezier(a.x + NODE_W / 2, a.y + NODE_H, b.x + NODE_W / 2, b.y), delegate: false, mesh: true });
    }
  }

  // Frame all nodes (free positions can run anywhere) with a padded viewBox.
  const x0 = Math.min(...nodes.map((n) => n.x)) - PAD;
  const y0 = Math.min(...nodes.map((n) => n.y)) - PAD;
  const x1 = Math.max(...nodes.map((n) => n.x + NODE_W)) + PAD;
  const y1 = Math.max(...nodes.map((n) => n.y + NODE_H)) + PAD;
  return { nodes, edges, vx: x0, vy: y0, width: x1 - x0, height: y1 - y0 };
});

// clientToSvg maps a pointer's screen coordinates into the SVG user space
// (accounting for the viewBox), so a drag tracks the cursor exactly.
function clientToSvg(cx: number, cy: number): { x: number; y: number } {
  const svg = svgRef.value;
  const ctm = svg?.getScreenCTM?.();
  if (!svg || !ctm) return { x: cx, y: cy };
  const p = new DOMPoint(cx, cy).matrixTransform(ctm.inverse());
  return { x: p.x, y: p.y };
}
function onNodePointerDown(e: PointerEvent, node: LNode) {
  // Free-form repositioning applies to agents in the delegating fleet modes; a
  // fixed sequence keeps its left-to-right order.
  if (!props.editable || node.kind !== 'agent' || mode.value === 'sequence') return;
  const p = clientToSvg(e.clientX, e.clientY);
  dragSlug.value = node.slug;
  dragOffset.value = { x: p.x - node.x, y: p.y - node.y };
}
function onSvgPointerMove(e: PointerEvent) {
  if (!dragSlug.value) return;
  const p = clientToSvg(e.clientX, e.clientY);
  freePos.value = { ...freePos.value, [dragSlug.value]: { x: p.x - dragOffset.value.x, y: p.y - dragOffset.value.y } };
}
function onSvgPointerUp() {
  if (dragSlug.value) savePositions();
  dragSlug.value = null;
}
const draggable = computed(() => props.editable && mode.value !== 'sequence');
</script>

<template>
  <div class="agc">
    <div v-if="!flow" class="agc__empty">Select a fleet to view its agent-graph.</div>
    <div v-else-if="agents.length === 0" class="agc__empty">This fleet has no agents yet.</div>
    <template v-else>
      <div class="agc__meta">
        <span class="agc__mode" :class="`agc__mode--${mode}`">{{ modeLabel }}</span>
        <span v-if="mode !== 'sequence'" class="agc__experimental-tag">experimental</span>
        <span class="agc__hint">
          {{ mode === 'sequence'
            ? 'Agents run in order, left to right.'
            : 'A task enters at the lead; the lead delegates to members. Does not make durable commits yet.' }}
        </span>
      </div>

      <svg
        ref="svgRef"
        class="agc__svg"
        :class="{ 'agc__svg--dragging': !!dragSlug }"
        :viewBox="`${layout.vx} ${layout.vy} ${layout.width} ${layout.height}`"
        :width="layout.width"
        :height="layout.height"
        role="img"
        aria-label="Agent fleet graph"
        @pointermove="onSvgPointerMove"
        @pointerup="onSvgPointerUp"
        @pointerleave="onSvgPointerUp"
      >
        <defs>
          <marker id="agc-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M 0 1 L 7 4 L 0 7 z" class="agc-arrow-head" />
          </marker>
        </defs>

        <g class="agc__edges">
          <path
            v-for="edge in layout.edges"
            :key="edge.key"
            class="agc-edge"
            :class="{ 'agc-edge--delegate': edge.delegate, 'agc-edge--mesh': edge.mesh }"
            :marker-end="edge.delegate ? 'url(#agc-arrow)' : undefined"
            :d="edge.d"
            fill="none"
          />
        </g>

        <g class="agc__nodes">
          <g
            v-for="node in layout.nodes"
            :key="node.key"
            class="agc-node"
            :class="{
              'agc-node--task': node.kind === 'task',
              'agc-node--outcome': node.kind === 'outcome',
              'agc-node--agent': node.kind === 'agent',
              'agc-node--lead': node.lead && mode !== 'sequence',
              'agc-node--editable': editable && node.kind === 'agent',
              'agc-node--draggable': draggable && node.kind === 'agent',
              'agc-node--dragging': dragSlug === node.slug,
              [`agc-node--run-${runStatus?.[node.slug]}`]: !!(node.slug && runStatus?.[node.slug]),
            }"
            :data-node-slug="node.slug || null"
            @pointerdown="onNodePointerDown($event, node)"
            @dblclick="node.slug && emit('editAgent', node.slug)"
          >
            <text
              v-if="node.lead && mode !== 'sequence'"
              class="agc-lead-tag"
              :x="node.x + NODE_W / 2"
              :y="node.y - 9"
              text-anchor="middle"
            >LEAD</text>

            <rect
              class="agc-node-box"
              :x="node.x"
              :y="node.y"
              :width="NODE_W"
              :height="NODE_H"
              rx="10"
            />
            <text
              class="agc-node-text"
              :x="node.x + NODE_W / 2"
              :y="node.y + NODE_H / 2"
              text-anchor="middle"
              dominant-baseline="central"
            >{{ node.name }}</text>

            <!-- Promote to lead, edit mode + members only (top-left). -->
            <g
              v-if="editable && node.member"
              class="agc-node-lead-btn"
              role="button"
              :aria-label="`Make ${node.name} the lead`"
              tabindex="0"
              @pointerdown.stop
              @click="emit('setLead', node.slug)"
              @keydown.enter="emit('setLead', node.slug)"
            >
              <title>Make this the lead agent</title>
              <circle class="agc-corner-bg agc-corner-bg--lead" :cx="node.x + 9" :cy="node.y + 9" r="9" />
              <text class="agc-corner-icon agc-corner-icon--lead" :x="node.x + 9" :y="node.y + 9" text-anchor="middle" dominant-baseline="central">&#9733;</text>
            </g>

            <!-- Remove agent, edit mode (top-right). -->
            <g
              v-if="editable && node.kind === 'agent'"
              class="agc-node-remove"
              role="button"
              :aria-label="`Remove ${node.name}`"
              tabindex="0"
              @pointerdown.stop
              @click="emit('remove', node.slug)"
              @keydown.enter="emit('remove', node.slug)"
            >
              <title>Remove agent</title>
              <circle class="agc-corner-bg agc-corner-bg--remove" :cx="node.x + NODE_W - 9" :cy="node.y + 9" r="9" />
              <text class="agc-corner-icon agc-corner-icon--remove" :x="node.x + NODE_W - 9" :y="node.y + 9" text-anchor="middle" dominant-baseline="central">&#215;</text>
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
  /* Node labels are diagram content, not selectable text; selection also fights
     pointer interactions. */
  user-select: none;
  -webkit-user-select: none;
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
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin: 0 0 0.6rem;
  flex-wrap: wrap;
}
.agc__mode {
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
.agc__mode--lead,
.agc__mode--mesh {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.agc__experimental-tag {
  font-size: 0.62rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  color: var(--warning, #c98a00);
  background: color-mix(in srgb, var(--warning, #c98a00) 14%, transparent);
}
.agc__hint {
  font-size: 0.74rem;
  color: var(--text-secondary);
}
.agc__svg {
  display: block;
  max-width: none;
}
.agc-edge {
  stroke: var(--border-strong, var(--border));
  stroke-width: 1.5;
}
.agc-edge--delegate {
  stroke: var(--accent);
}
.agc-edge--mesh {
  stroke: var(--accent);
  stroke-dasharray: 4 3;
  opacity: 0.7;
}
.agc-arrow-head {
  fill: var(--accent);
}
.agc-node-box {
  fill: var(--bg-elevated);
  stroke: var(--border);
  stroke-width: 1.2;
}
.agc-node--task .agc-node-box,
.agc-node--outcome .agc-node-box {
  fill: var(--bg-sunk);
  stroke-dasharray: 4 3;
}
.agc-node--lead .agc-node-box {
  stroke: var(--accent);
  stroke-width: 1.8;
}
/* Run overlay: status colours for the agent that ran. */
.agc-node--run-running .agc-node-box {
  stroke: var(--warning, #c98a00);
  stroke-width: 2;
  fill: color-mix(in srgb, var(--warning, #c98a00) 12%, var(--bg-elevated));
}
.agc-node--run-done .agc-node-box {
  stroke: var(--success, #2e9e5b);
  stroke-width: 2;
  fill: color-mix(in srgb, var(--success, #2e9e5b) 12%, var(--bg-elevated));
}
.agc-node--run-failed .agc-node-box {
  stroke: var(--danger, #d2453f);
  stroke-width: 2;
  fill: color-mix(in srgb, var(--danger, #d2453f) 12%, var(--bg-elevated));
}
.agc-node-text {
  fill: var(--text);
  font-size: 0.8rem;
  font-weight: 600;
}
.agc-node--task .agc-node-text,
.agc-node--outcome .agc-node-text {
  fill: var(--text-secondary);
}
.agc-lead-tag {
  fill: var(--accent);
  font-size: 0.6rem;
  font-weight: 700;
  letter-spacing: 0.08em;
}
.agc-node--editable {
  cursor: pointer;
}
.agc-node--draggable {
  cursor: grab;
}
.agc__svg--dragging .agc-node--draggable {
  cursor: grabbing;
}
.agc-node--dragging .agc-node-box {
  opacity: 0.7;
}
.agc-node-remove,
.agc-node-lead-btn {
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.1s ease;
}
.agc-node:hover .agc-node-remove,
.agc-node:hover .agc-node-lead-btn,
.agc-node-remove:focus-visible,
.agc-node-lead-btn:focus-visible {
  opacity: 1;
}
.agc-corner-bg {
  fill: var(--bg-elevated);
  stroke-width: 1.2;
}
.agc-corner-bg--remove {
  stroke: var(--danger, #d2453f);
}
.agc-corner-bg--lead {
  stroke: var(--accent);
}
.agc-node-remove:hover .agc-corner-bg--remove {
  fill: var(--danger, #d2453f);
}
.agc-node-lead-btn:hover .agc-corner-bg--lead {
  fill: var(--accent);
}
.agc-corner-icon {
  font-size: 0.78rem;
  font-weight: 700;
  pointer-events: none;
}
.agc-corner-icon--remove {
  fill: var(--danger, #d2453f);
}
.agc-corner-icon--lead {
  fill: var(--accent);
}
.agc-node-remove:hover .agc-corner-icon--remove,
.agc-node-lead-btn:hover .agc-corner-icon--lead {
  fill: #fff;
}
</style>
