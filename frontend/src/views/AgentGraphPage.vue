<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api } from '../api/client';
import type { Agent, Flow } from '../api/types';
import AgentGraphCanvas from '../components/AgentGraphCanvas.vue';

// AgentGraphPage is the read-only scaffold of the unified agent-graph surface
// (spec: unified-agent-graph-ui.md, M6.1). Left is the agent palette (the
// merged registry, searchable); centre renders a selected flow as an
// agent-graph. It is deliberately store-free and router-free: cards do not
// navigate and nothing is persisted, which keeps the surface self-contained
// and the component test trivial. Editing arrives in M6.2.

const agents = ref<Agent[]>([]);
const flows = ref<Flow[]>([]);
const loading = ref(true);
const search = ref('');
const selectedSlug = ref<string | null>(null);

const filteredAgents = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return agents.value.slice();
  return agents.value.filter(
    (a) =>
      (a.slug || '').toLowerCase().includes(q) ||
      (a.title || '').toLowerCase().includes(q) ||
      (a.description || '').toLowerCase().includes(q),
  );
});

const selectedFlow = computed<Flow | null>(() => {
  if (!selectedSlug.value) return null;
  return flows.value.find((f) => f.slug === selectedSlug.value) || null;
});

const flowOptions = computed(() => flows.value);

async function loadAgents() {
  try {
    const rows = await api<Agent[]>('GET', '/api/agents');
    agents.value = Array.isArray(rows) ? rows : [];
  } catch (e) {
    console.error('agents:', e);
  }
}

async function loadFlows() {
  try {
    const rows = await api<Flow[]>('GET', '/api/flows');
    flows.value = Array.isArray(rows) ? rows : [];
    if (!selectedSlug.value && flows.value.length) {
      selectedSlug.value = flows.value[0].slug;
    }
  } catch (e) {
    console.error('flows:', e);
  }
}

onMounted(async () => {
  loading.value = true;
  try {
    await Promise.all([loadAgents(), loadFlows()]);
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="ag-mode-container">
    <div class="ag-mode__inner">
      <header class="ag-mode__header">
        <div class="ag-mode__header-row">
          <div>
            <h2 class="ag-mode__title">Agent Graph</h2>
            <p class="ag-mode__subtitle">
              One surface for agents and flows. The palette on the left lists the
              agent registry; the canvas renders a flow as an agent-graph, with a
              node per step and edges for order. This view is read-only.
            </p>
          </div>
          <div class="ag-mode__header-actions">
            <label class="ag-mode__flow-pick">
              <span class="ag-mode__flow-pick-label">Flow</span>
              <select v-model="selectedSlug" class="ag-mode__flow-select" aria-label="Flow">
                <option v-if="!flowOptions.length" :value="null">No flows</option>
                <option v-for="f in flowOptions" :key="f.slug" :value="f.slug">
                  {{ f.name || f.slug }}
                </option>
              </select>
            </label>
          </div>
        </div>
      </header>

      <div class="ag-mode__split">
        <aside class="ag-mode__rail">
          <div class="ag-mode__search">
            <input
              v-model="search"
              type="search"
              placeholder="Search agents..."
              aria-label="Search agents"
              autocomplete="off"
            />
          </div>
          <div class="ag-mode__rail-list">
            <p v-if="loading" class="ag-mode__empty">Loading agents...</p>
            <template v-else>
              <p v-if="filteredAgents.length === 0" class="ag-mode__empty">
                {{ search ? 'No matches.' : 'No agents registered.' }}
              </p>
              <div
                v-for="a in filteredAgents"
                :key="a.slug"
                class="ag-card"
              >
                <div class="ag-card__head">
                  <span class="ag-card__name">{{ a.title || a.slug }}</span>
                  <span v-if="a.harness" class="ag-card__role">{{ a.harness }}</span>
                  <span v-else-if="a.builtin" class="ag-card__role">built-in</span>
                </div>
                <p v-if="a.description" class="ag-card__desc">{{ a.description }}</p>
                <code class="ag-card__slug">{{ a.slug }}</code>
              </div>
            </template>
          </div>
        </aside>

        <section class="ag-mode__detail">
          <div v-if="loading" class="ag-mode__empty-detail">
            <p>Loading flow...</p>
          </div>
          <div v-else-if="!selectedFlow" class="ag-mode__empty-detail">
            <p>Pick a flow above to render its agent-graph.</p>
          </div>
          <template v-else>
            <div class="ag-detail__head">
              <h3 class="ag-detail__title">{{ selectedFlow.name || selectedFlow.slug }}</h3>
              <span
                class="ag-detail__badge"
                :class="{ 'ag-detail__badge--user': !selectedFlow.builtin }"
              >{{ selectedFlow.builtin ? 'built-in' : 'user' }}</span>
              <code class="ag-detail__slug">{{ selectedFlow.slug }}</code>
            </div>
            <p v-if="selectedFlow.description" class="ag-detail__desc">
              {{ selectedFlow.description }}
            </p>
            <div class="ag-detail__canvas">
              <AgentGraphCanvas :flow="selectedFlow" />
            </div>
          </template>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ag-mode-container {
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.ag-mode__inner {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 1.1rem 1.25rem;
  gap: 1rem;
}
.ag-mode__header-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.ag-mode__title {
  margin: 0;
  font-size: 1.25rem;
}
.ag-mode__subtitle {
  margin: 0.3rem 0 0;
  max-width: 46rem;
  font-size: 0.84rem;
  color: var(--text-secondary);
}
.ag-mode__flow-pick {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.ag-mode__flow-select {
  font: inherit;
  padding: 0.35rem 0.5rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--text);
}
.ag-mode__split {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 1rem;
}
.ag-mode__rail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  overflow: hidden;
}
.ag-mode__search {
  padding: 0.6rem;
  border-bottom: 1px solid var(--border);
}
.ag-mode__search input {
  width: 100%;
  font: inherit;
  padding: 0.4rem 0.55rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
}
.ag-mode__rail-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0.6rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.ag-mode__empty {
  margin: 0.5rem 0;
  font-size: 0.8rem;
  color: var(--text-secondary);
}
.ag-card {
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--bg-sunk);
  padding: 0.55rem 0.65rem;
}
.ag-card__head {
  display: flex;
  align-items: baseline;
  gap: 0.45rem;
}
.ag-card__name {
  font-size: 0.84rem;
  font-weight: 600;
  color: var(--text);
}
.ag-card__role {
  font-size: 0.66rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
}
.ag-card__desc {
  margin: 0.25rem 0 0;
  font-size: 0.76rem;
  color: var(--text-secondary);
}
.ag-card__slug {
  display: inline-block;
  margin-top: 0.3rem;
  font-size: 0.7rem;
  color: var(--text-muted);
}
.ag-mode__detail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  padding: 0.9rem 1rem;
  overflow: hidden;
}
.ag-mode__empty-detail {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-secondary);
  font-size: 0.85rem;
}
.ag-detail__head {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
}
.ag-detail__title {
  margin: 0;
  font-size: 1.02rem;
}
.ag-detail__badge {
  font-size: 0.64rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  color: var(--text-muted);
  background: var(--bg-hover);
}
.ag-detail__badge--user {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.ag-detail__slug {
  font-size: 0.72rem;
  color: var(--text-muted);
}
.ag-detail__desc {
  margin: 0.45rem 0 0;
  font-size: 0.8rem;
  color: var(--text-secondary);
}
.ag-detail__canvas {
  flex: 1;
  min-height: 0;
  margin-top: 0.75rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-sunk);
  overflow: auto;
}
</style>
