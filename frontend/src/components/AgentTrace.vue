<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api/client';
import { renderMarkdown } from '../lib/markdown';
import type { TaskTrace, TraceNode } from '../api/types';

// AgentTrace renders an agentic-flow run's agent graph plus a live, per-agent
// transcript. The graph nodes (with status colour) and handoff edges come from
// the persisted trace; the transcript is built from the run's events
// (forwarded onto the task timeline as the run proceeds), so it appears live
// while the run is in flight, not just after it completes. refreshKey (the task's
// updated_at) re-pulls both whenever the task changes, riding the existing live
// task-update path — no dedicated stream needed.
const props = defineProps<{ taskId: string; refreshKey?: string }>();

interface EventRow {
  id: number;
  agent: string;
  kind: string; // "assistant" | "delegate" | "tool"
  text: string;
  result: string;
}

const trace = ref<TaskTrace | null>(null);
const events = ref<EventRow[]>([]);
const error = ref('');

interface RawEvent {
  id: number;
  event_type: string;
  data?: Record<string, unknown>;
}

async function fetchAll() {
  // Trace: 404 (not persisted yet, mid-run) is the normal live case.
  try {
    trace.value = await api<TaskTrace>('GET', `/api/tasks/${props.taskId}/trace`);
  } catch {
    trace.value = null;
  }
  try {
    const data = await api<RawEvent[] | { events?: RawEvent[] }>(
      'GET',
      `/api/tasks/${props.taskId}/events`,
    );
    const raw = Array.isArray(data) ? data : (data?.events ?? []);
    events.value = raw
      .filter((e) => e.data?.source === 'agentgraph')
      .map((e) => ({
        id: e.id,
        agent: String(e.data?.agent ?? ''),
        kind: String(e.data?.kind ?? ''),
        text: String(e.data?.text ?? ''),
        result: String(e.data?.result ?? ''),
      }));
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load events';
  }
}

watch(() => [props.taskId, props.refreshKey], fetchAll, { immediate: true });

// Persisted nodes when the run has finished; otherwise synthesize provisional
// "running" nodes from the agents seen in the events so the graph shows live.
const nodes = computed<TraceNode[]>(() => {
  if (trace.value?.nodes?.length) return trace.value.nodes;
  const seen = new Map<string, TraceNode>();
  for (const r of events.value) {
    if (r.agent && !seen.has(r.agent)) {
      seen.set(r.agent, { id: r.agent, name: r.agent, role: '', status: 'running', grants: [], sandbox: '' });
    }
  }
  return [...seen.values()];
});
const edges = computed(() => trace.value?.edges ?? []);
const hasGraph = computed(() => nodes.value.length > 0);
const hasEvents = computed(() => events.value.length > 0);
const visible = computed(() => hasGraph.value || hasEvents.value);

const nameById = computed(() => {
  const map = new Map<string, string>();
  for (const n of nodes.value) map.set(n.id, n.name || n.id);
  return map;
});
function endpointName(id: string): string {
  return nameById.value.get(id) ?? id;
}

function statusKind(node: TraceNode): 'running' | 'done' | 'failed' | 'other' {
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
// browser in the review verification view. Cache so each turn parses once.
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
  <section v-if="visible" class="trace">
    <header class="trace__header">
      <svg
        class="trace__logo"
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.7"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <circle cx="6" cy="7" r="2.2" />
        <circle cx="17" cy="6" r="2.2" />
        <circle cx="18" cy="17" r="2.2" />
        <circle cx="7" cy="18" r="2.2" />
        <path d="M8.1 7.4c2.3 1.3 4.8 1.1 6.9-.5M16.5 8.1c1.4 2 1.8 4.3 1.5 6.7M15.9 17.4c-2.1.9-4.4 1.1-6.7.6M6.8 15.8c-.7-2.2-.8-4.4-.2-6.6M9 9.1l6 6" />
      </svg>
      <span>Agent Graph</span>
      <span class="trace__powered">powered by <span class="topos-brand">Topos</span></span>
    </header>

    <p v-if="error" class="trace__note trace__note--error">Events unavailable: {{ error }}</p>

    <ul v-if="hasGraph" class="trace__nodes">
      <li
        v-for="node in nodes"
        :key="node.id"
        class="trace__node"
        :class="`trace__node--${statusKind(node)}`"
      >
        <span class="trace__node-name">{{ node.name || node.id }}</span>
        <span v-if="node.role" class="trace__node-role">{{ node.role }}</span>
        <span class="trace__node-status">{{ node.status }}</span>
      </li>
    </ul>

    <ul v-if="edges.length" class="trace__edges">
      <li v-for="(edge, i) in edges" :key="i" class="trace__edge">
        <span class="trace__edge-end">{{ endpointName(edge.from) }}</span>
        <span class="trace__edge-kind" :class="`trace__edge-kind--${edge.kind}`">{{ edge.kind }}</span>
        <span class="trace__edge-end">{{ endpointName(edge.to) }}</span>
      </li>
    </ul>

    <!-- Live per-agent transcript, in run order. -->
    <ol v-if="hasEvents" class="trace__events">
      <li
        v-for="row in events"
        :key="row.id"
        class="trace__turn"
        :class="`trace__turn--${row.kind}`"
      >
        <span class="trace__turn-agent">{{ row.agent }}</span>
        <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
        <div
          v-if="row.kind === 'assistant' && row.text"
          class="trace__turn-body prose-content"
          v-html="renderTurn(row.text)"
        />
        <span v-else class="trace__turn-meta">{{ row.result }}</span>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.trace {
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  padding: 0.85rem;
  margin-bottom: 1.25rem;
}
.trace__header {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-weight: 600;
  font-size: 0.9rem;
}
.trace__logo {
  color: var(--accent);
  flex: none;
}
.trace__powered {
  font-weight: 400;
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin-left: 0.15rem;
}
.trace__note {
  margin: 0.55rem 0 0;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.trace__note--error {
  color: var(--warn);
}

.trace__nodes {
  list-style: none;
  margin: 0.7rem 0 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.trace__node {
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
.trace__node--running {
  border-left-color: var(--accent);
}
.trace__node--done {
  border-left-color: var(--ok);
}
.trace__node--failed {
  border-left-color: var(--warn);
}
.trace__node-name {
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--text);
}
.trace__node-role {
  font-size: 0.72rem;
  color: var(--text-secondary);
}
.trace__node-status {
  font-size: 0.68rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
}

.trace__edges {
  list-style: none;
  margin: 0.7rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.trace__edge {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.trace__edge-end {
  color: var(--text);
}
.trace__edge-kind {
  font-size: 0.66rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  color: var(--text-muted);
  background: var(--bg-hover);
}
.trace__edge-kind--delegate {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.trace__edge-kind--deliver {
  color: var(--ok);
  background: color-mix(in srgb, var(--ok) 16%, transparent);
}

.trace__events {
  list-style: none;
  margin: 0.8rem 0 0;
  padding: 0.7rem 0 0;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.trace__turn {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
}
.trace__turn-agent {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--accent);
}
.trace__turn-body {
  font-size: 0.82rem;
  color: var(--text);
}
.trace__turn-meta {
  font-size: 0.76rem;
  color: var(--text-secondary);
}
.trace__turn--delegate .trace__turn-agent {
  color: var(--ok);
}
.trace__turn--tool .trace__turn-agent {
  color: var(--text-muted);
}
</style>
