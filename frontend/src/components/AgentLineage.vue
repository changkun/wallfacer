<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { api } from '../api/client';
import type { TaskLineage, LineageNode } from '../api/types';

// AgentLineage renders the agent-graph lineage of an agentic-flow run: each
// agent as a labeled box with a status colour, and each handoff as a labeled
// edge (delegate / deliver / next). It fetches its own data so it stays
// self-contained and can be dropped beside a task result without the parent
// wiring a request. Rendering is plain interpolation (no markdown, no v-html).
const props = defineProps<{ taskId: string }>();

const lineage = ref<TaskLineage | null>(null);
const loading = ref(true);
const error = ref('');

onMounted(async () => {
  try {
    lineage.value = await api<TaskLineage>('GET', `/api/tasks/${props.taskId}/lineage`);
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load lineage';
  } finally {
    loading.value = false;
  }
});

const nodes = computed(() => lineage.value?.nodes ?? []);
const edges = computed(() => lineage.value?.edges ?? []);
const hasGraph = computed(() => nodes.value.length > 0);

// nameById resolves an edge endpoint id to the agent's display name, falling
// back to the raw id when a referenced node is missing from the node set.
const nameById = computed(() => {
  const map = new Map<string, string>();
  for (const n of nodes.value) map.set(n.id, n.name || n.id);
  return map;
});
function endpointName(id: string): string {
  return nameById.value.get(id) ?? id;
}

// statusKind normalises a node status into one of the styled buckets; an
// unknown status renders with the neutral fallback.
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
</script>

<template>
  <section class="lineage">
    <header class="lineage__header">
      <span class="lineage__icon" aria-hidden="true">&#9783;</span>
      <span>Agent Graph</span>
    </header>

    <p v-if="loading" class="lineage__note">Loading lineage.</p>
    <p v-else-if="error" class="lineage__note lineage__note--error">Lineage unavailable: {{ error }}</p>
    <p v-else-if="!hasGraph" class="lineage__note">No lineage recorded for this run.</p>

    <template v-else>
      <ul class="lineage__nodes">
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
    </template>
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
</style>
