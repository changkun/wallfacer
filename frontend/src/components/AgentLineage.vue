<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api/client';
import { renderMarkdown } from '../lib/markdown';
import type { TaskLineage, LineageNode } from '../api/types';

// AgentLineage renders an agentic-flow run's agent graph plus a live, per-agent
// transcript. The graph nodes (with status colour) and handoff edges come from
// the persisted lineage; the transcript is built from the run's trace events
// (forwarded onto the task timeline as the run proceeds), so it appears live
// while the run is in flight, not just after it completes. refreshKey (the task's
// updated_at) re-pulls both whenever the task changes, riding the existing live
// task-update path — no dedicated stream needed.
const props = defineProps<{ taskId: string; refreshKey?: string }>();

interface TraceRow {
  id: number;
  agent: string;
  kind: string; // "assistant" | "delegate" | "tool"
  text: string;
  result: string;
}

const lineage = ref<TaskLineage | null>(null);
const trace = ref<TraceRow[]>([]);
const error = ref('');

interface RawEvent {
  id: number;
  event_type: string;
  data?: Record<string, unknown>;
}

async function fetchAll() {
  // Lineage: 404 (not persisted yet, mid-run) is the normal live case.
  try {
    lineage.value = await api<TaskLineage>('GET', `/api/tasks/${props.taskId}/lineage`);
  } catch {
    lineage.value = null;
  }
  try {
    const data = await api<RawEvent[] | { events?: RawEvent[] }>(
      'GET',
      `/api/tasks/${props.taskId}/events`,
    );
    const raw = Array.isArray(data) ? data : (data?.events ?? []);
    trace.value = raw
      .filter((e) => e.data?.source === 'agentgraph')
      .map((e) => ({
        id: e.id,
        agent: String(e.data?.agent ?? ''),
        kind: String(e.data?.kind ?? ''),
        text: String(e.data?.text ?? ''),
        result: String(e.data?.result ?? ''),
      }));
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load trace';
  }
}

watch(() => [props.taskId, props.refreshKey], fetchAll, { immediate: true });

// Persisted nodes when the run has finished; otherwise synthesize provisional
// "running" nodes from the agents seen in the trace so the graph shows live.
const nodes = computed<LineageNode[]>(() => {
  if (lineage.value?.nodes?.length) return lineage.value.nodes;
  const seen = new Map<string, LineageNode>();
  for (const r of trace.value) {
    if (r.agent && !seen.has(r.agent)) {
      seen.set(r.agent, { id: r.agent, name: r.agent, role: '', status: 'running', grants: [], sandbox: '' });
    }
  }
  return [...seen.values()];
});
const edges = computed(() => lineage.value?.edges ?? []);
const hasGraph = computed(() => nodes.value.length > 0);
const hasTrace = computed(() => trace.value.length > 0);
const visible = computed(() => hasGraph.value || hasTrace.value);

const nameById = computed(() => {
  const map = new Map<string, string>();
  for (const n of nodes.value) map.set(n.id, n.name || n.id);
  return map;
});
function endpointName(id: string): string {
  return nameById.value.get(id) ?? id;
}

function statusKind(node: LineageNode): 'running' | 'done' | 'failed' | 'other' {
  switch (node.status) {
    case 'running':
    case 'done':
    case 'failed':
      return node.status;
    default:
      return 'other';
  }
}

// Memoize markdown by content: the transcript re-renders whenever the task
// updates, and re-parsing every assistant turn on each tick is what pegged the
// browser in the agon verification view. Cache so each turn parses once.
const mdCache = new Map<string, string>();
function renderTurn(text: string): string {
  let html = mdCache.get(text);
  if (html === undefined) {
    if (mdCache.size > 500) mdCache.clear();
    html = renderMarkdown(text);
    mdCache.set(text, html);
  }
  return html;
}
</script>

<template>
  <section v-if="visible" class="lineage">
    <header class="lineage__header">
      <span class="lineage__icon" aria-hidden="true">&#9783;</span>
      <span>Agent Graph</span>
    </header>

    <p v-if="error" class="lineage__note lineage__note--error">Trace unavailable: {{ error }}</p>

    <ul v-if="hasGraph" class="lineage__nodes">
      <li
        v-for="node in nodes"
        :key="node.id"
        class="lineage__node"
        :class="`lineage__node--${statusKind(node)}`"
      >
        <span class="lineage__node-name">{{ node.name || node.id }}</span>
        <span v-if="node.role" class="lineage__node-role">{{ node.role }}</span>
        <span class="lineage__node-status">{{ node.status }}</span>
      </li>
    </ul>

    <ul v-if="edges.length" class="lineage__edges">
      <li v-for="(edge, i) in edges" :key="i" class="lineage__edge">
        <span class="lineage__edge-end">{{ endpointName(edge.from) }}</span>
        <span class="lineage__edge-kind" :class="`lineage__edge-kind--${edge.kind}`">{{ edge.kind }}</span>
        <span class="lineage__edge-end">{{ endpointName(edge.to) }}</span>
      </li>
    </ul>

    <!-- Live per-agent transcript, in run order. -->
    <ol v-if="hasTrace" class="lineage__trace">
      <li
        v-for="row in trace"
        :key="row.id"
        class="lineage__turn"
        :class="`lineage__turn--${row.kind}`"
      >
        <span class="lineage__turn-agent">{{ row.agent }}</span>
        <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
        <div
          v-if="row.kind === 'assistant' && row.text"
          class="lineage__turn-body prose-content"
          v-html="renderTurn(row.text)"
        />
        <span v-else class="lineage__turn-meta">{{ row.result }}</span>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.lineage {
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  padding: 0.85rem;
  margin-bottom: 1.25rem;
}
.lineage__header {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-weight: 600;
  font-size: 0.9rem;
}
.lineage__icon {
  color: var(--accent);
  font-size: 1.05rem;
}
.lineage__note {
  margin: 0.55rem 0 0;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.lineage__note--error {
  color: var(--warn);
}

.lineage__nodes {
  list-style: none;
  margin: 0.7rem 0 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.lineage__node {
  display: inline-flex;
  flex-direction: column;
  gap: 0.15rem;
  min-width: 8rem;
  padding: 0.45rem 0.6rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--border);
  background: var(--bg-sunk);
}
.lineage__node--running {
  border-left-color: var(--accent);
}
.lineage__node--done {
  border-left-color: var(--ok);
}
.lineage__node--failed {
  border-left-color: var(--warn);
}
.lineage__node-name {
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--text);
}
.lineage__node-role {
  font-size: 0.72rem;
  color: var(--text-secondary);
}
.lineage__node-status {
  font-size: 0.68rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
}

.lineage__edges {
  list-style: none;
  margin: 0.7rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.lineage__edge {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.lineage__edge-end {
  color: var(--text);
}
.lineage__edge-kind {
  font-size: 0.66rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  color: var(--text-muted);
  background: var(--bg-hover);
}
.lineage__edge-kind--delegate {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.lineage__edge-kind--deliver {
  color: var(--ok);
  background: color-mix(in srgb, var(--ok) 16%, transparent);
}

.lineage__trace {
  list-style: none;
  margin: 0.8rem 0 0;
  padding: 0.7rem 0 0;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.lineage__turn {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
}
.lineage__turn-agent {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--accent);
}
.lineage__turn-body {
  font-size: 0.82rem;
  color: var(--text);
}
.lineage__turn-meta {
  font-size: 0.76rem;
  color: var(--text-secondary);
}
.lineage__turn--delegate .lineage__turn-agent {
  color: var(--ok);
}
.lineage__turn--tool .lineage__turn-agent {
  color: var(--text-muted);
}
</style>
