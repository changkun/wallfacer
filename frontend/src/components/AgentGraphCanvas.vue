<script setup lang="ts">
import { computed } from 'vue';
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
const props = withDefaults(defineProps<{ flow: Flow | null; editable?: boolean }>(), {
  editable: false,
});
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
  if (ags.length === 0) return { nodes, edges, width: 0, height: 0 };

  // Fixed sequence: Task -> a0 -> a1 -> ... -> Outcome, one row.
  if (mode.value === 'sequence') {
    const cols = ags.length + 2; // task + agents + outcome
    const width = PAD * 2 + cols * NODE_W + (cols - 1) * (COL - NODE_W);
    const height = PAD * 2 + NODE_H;
    const y = PAD;
    nodes.push({ key: 'task', kind: 'task', slug: '', name: 'Task', lead: false, member: false, x: colX(0), y });
    ags.forEach((a, i) =>
      nodes.push({ key: `a:${a.slug}`, kind: 'agent', slug: a.slug, name: a.name, lead: a.lead, member: !a.lead, x: colX(i + 1), y }),
    );
    nodes.push({ key: 'outcome', kind: 'outcome', slug: '', name: 'Outcome', lead: false, member: false, x: colX(cols - 1), y });
    for (let i = 0; i < nodes.length - 1; i++) {
      const a = nodes[i];
      const b = nodes[i + 1];
      edges.push({ key: `e:${i}`, d: bezier(a.x + NODE_W, a.y + NODE_H / 2, b.x, b.y + NODE_H / 2), delegate: false, mesh: false });
    }
    return { nodes, edges, width, height };
  }

  // lead / mesh: Task | Lead | members (stacked) | Outcome.
  const lead = ags[0];
  const members = ags.slice(1);
  const rows = Math.max(1, members.length);
  const height = PAD * 2 + rows * ROW - (ROW - NODE_H);
  const width = PAD * 2 + 4 * NODE_W + 3 * (COL - NODE_W);
  const midY = (height - NODE_H) / 2;
  const rowY = (row: number): number => {
    const block = members.length * ROW - (ROW - NODE_H);
    return (height - block) / 2 + row * ROW;
  };

  const taskN: LNode = { key: 'task', kind: 'task', slug: '', name: 'Task', lead: false, member: false, x: colX(0), y: midY };
  const leadN: LNode = { key: `a:${lead.slug}`, kind: 'agent', slug: lead.slug, name: lead.name, lead: true, member: false, x: colX(1), y: midY };
  const memberNs: LNode[] = members.map((a, i) => ({
    key: `a:${a.slug}`, kind: 'agent', slug: a.slug, name: a.name, lead: false, member: true, x: colX(2), y: rowY(i),
  }));
  const outN: LNode = { key: 'outcome', kind: 'outcome', slug: '', name: 'Outcome', lead: false, member: false, x: colX(3), y: midY };
  nodes.push(taskN, leadN, ...memberNs, outN);

  // Task enters at the lead; the lead returns the outcome.
  edges.push({ key: 'e:enter', d: bezier(taskN.x + NODE_W, taskN.y + NODE_H / 2, leadN.x, leadN.y + NODE_H / 2), delegate: false, mesh: false });
  edges.push({ key: 'e:outcome', d: bezier(leadN.x + NODE_W, leadN.y + NODE_H / 2, outN.x, outN.y + NODE_H / 2), delegate: false, mesh: false });
  // The lead delegates to each member.
  memberNs.forEach((mn, i) =>
    edges.push({ key: `e:deleg:${i}`, d: bezier(leadN.x + NODE_W, leadN.y + NODE_H / 2, mn.x, mn.y + NODE_H / 2), delegate: true, mesh: false }),
  );
  // Open mesh: members also hand off among themselves (drawn vertically).
  if (mode.value === 'mesh') {
    for (let i = 0; i < memberNs.length - 1; i++) {
      const a = memberNs[i];
      const b = memberNs[i + 1];
      edges.push({ key: `e:mesh:${i}`, d: bezier(a.x + NODE_W / 2, a.y + NODE_H, b.x + NODE_W / 2, b.y), delegate: false, mesh: true });
    }
  }
  return { nodes, edges, width, height };
});
</script>

<template>
  <div class="agc">
    <div v-if="!flow" class="agc__empty">Select a fleet to view its agent-graph.</div>
    <div v-else-if="agents.length === 0" class="agc__empty">This fleet has no agents yet.</div>
    <template v-else>
      <div class="agc__meta">
        <span class="agc__mode" :class="`agc__mode--${mode}`">{{ modeLabel }}</span>
        <span class="agc__hint">
          {{ mode === 'sequence'
            ? 'Agents run in order, left to right.'
            : 'A task enters at the lead; the fleet delegates to reach an outcome.' }}
        </span>
      </div>

      <svg
        class="agc__svg"
        :viewBox="`0 0 ${layout.width} ${layout.height}`"
        :width="layout.width"
        :height="layout.height"
        role="img"
        aria-label="Agent fleet graph"
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
            }"
            :data-node-slug="node.slug || null"
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
