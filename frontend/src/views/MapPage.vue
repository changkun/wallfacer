<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import GraphCanvas from '../components/map/GraphCanvas.vue';
import TaskDetail from '../components/TaskDetail.vue';
import type { Graph, GraphNode, Task } from '../api/types';

// MapPage renders the unified spec+task graph served authoritatively by
// GET /api/graph (internal/graph). The legacy vendored depgraph renderer and
// its window-shim bridge are gone; the graph is fetched data, drawn by the
// hand-rolled GraphCanvas component.

const store = useTaskStore();
const router = useRouter();

const graph = ref<Graph>({ nodes: [], edges: [], critical_path: [], blocked: [] });
const loadError = ref(false);
const showArchived = ref(false);
const mapSearch = ref('');
const selectedId = ref<string | null>(null);
const canvas = ref<InstanceType<typeof GraphCanvas> | null>(null);

async function loadGraph() {
  try {
    const q = showArchived.value ? '?archived=1' : '';
    graph.value = await api<Graph>('GET', '/api/graph' + q);
    loadError.value = false;
  } catch (e) {
    console.error('MapPage: graph load failed', e);
    loadError.value = true;
  }
}

onMounted(loadGraph);

// Filter by label substring; keep only edges whose endpoints both survive.
const filteredGraph = computed<Graph>(() => {
  const q = mapSearch.value.trim().toLowerCase();
  if (!q) return graph.value;
  const nodes = graph.value.nodes.filter((n) => n.label.toLowerCase().includes(q));
  const keep = new Set(nodes.map((n) => n.id));
  const edges = graph.value.edges.filter((e) => keep.has(e.from) && keep.has(e.to));
  const critical_path = graph.value.critical_path.filter((id) => keep.has(id));
  const blocked = graph.value.blocked.filter((id) => keep.has(id));
  return { nodes, edges, critical_path, blocked };
});

const selectedNode = computed<GraphNode | null>(
  () => graph.value.nodes.find((n) => n.id === selectedId.value) ?? null,
);
// A selected task node resolves to a live Task for the TaskDetail overlay.
const selectedTask = computed<Task | null>(() => {
  const n = selectedNode.value;
  if (!n || n.kind !== 'task') return null;
  return store.tasks.find((t) => t.id === n.ref) ?? null;
});
const detailTaskId = ref<string | null>(null);
const detailTask = computed<Task | null>(() =>
  detailTaskId.value ? store.tasks.find((t) => t.id === detailTaskId.value) ?? null : null,
);

function onSelect(id: string) {
  selectedId.value = id;
}
function openInPlan(path: string) {
  void router.push({ path: '/plan', query: { spec: path } });
}
function openInBoard(taskId: string) {
  if (store.tasks.some((t) => t.id === taskId)) detailTaskId.value = taskId;
}
function onResetClick() {
  mapSearch.value = '';
  canvas.value?.resetView();
}

const criticalNodes = computed(() =>
  graph.value.critical_path
    .map((id) => graph.value.nodes.find((n) => n.id === id))
    .filter((n): n is GraphNode => !!n),
);

// Re-sync the graph when the drawn task set changes (SSE deltas mutate tasks in
// place), when the workspace switches, or when the archived toggle flips.
watch(
  () => store.tasks.map((t) => `${t.id}:${t.status}:${t.archived ? 1 : 0}:${(t.depends_on || []).join('|')}`).join(','),
  () => void loadGraph(),
);
watch(() => (store.config?.workspaces ?? []).join('\n'), () => void loadGraph());
watch(showArchived, () => void loadGraph());
</script>

<template>
  <div class="depgraph-mode-container">
    <div class="depgraph-mode__inner">
      <header class="depgraph-mode__header">
        <div class="depgraph-mode__titles">
          <h2 class="depgraph-mode__title">Map</h2>
          <p class="depgraph-mode__subtitle">
            The whole pipeline — specs and tasks by dependency. Click a node to
            inspect and act; drag to reposition. Hold <kbd>Space</kbd> and drag
            to pan; <kbd>Ctrl</kbd>/<kbd>&#8984;</kbd>+scroll to zoom.
          </p>
        </div>
        <div class="depgraph-mode__actions">
          <input
            v-model="mapSearch"
            type="search"
            class="depgraph-mode__search"
            placeholder="Filter nodes…"
            aria-label="Filter graph nodes"
          />
          <label class="depgraph-mode__option" title="Include archived specs and tasks">
            <input type="checkbox" v-model="showArchived" />
            Show archived
          </label>
          <button type="button" class="depgraph-mode__reset-btn" title="Clear filter and reset view" @click="onResetClick">
            Reset layout
          </button>
        </div>
      </header>
      <div class="depgraph-mode__body">
        <div class="depgraph-mode__mount">
          <div v-if="loadError" class="depgraph-mode__empty">
            <p>Couldn't load the graph. <button type="button" @click="loadGraph">Retry</button></p>
          </div>
          <div v-else-if="!graph.nodes.length" class="depgraph-mode__empty">
            <p>
              No specs or tasks yet. Create a spec in Plan or add a task on the
              Board to see the pipeline graph.
            </p>
          </div>
          <GraphCanvas v-else ref="canvas" :graph="filteredGraph" :selected-id="selectedId" @select="onSelect" />
        </div>
        <aside class="depgraph-inspector" aria-label="Graph inspector">
          <section class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Selection</h3>
            <div v-if="selectedNode" class="depgraph-inspector__selection">
              <p><strong>{{ selectedNode.label }}</strong></p>
              <p class="depgraph-inspector__muted">{{ selectedNode.kind }} · {{ selectedNode.status }}</p>
              <div class="depgraph-inspector__actions">
                <button v-if="selectedNode.kind === 'spec'" type="button" @click="openInPlan(selectedNode.ref)">
                  Open in Plan
                </button>
                <button
                  v-if="selectedNode.kind === 'task' && selectedTask"
                  type="button"
                  @click="openInBoard(selectedNode.ref)"
                >
                  Open in Board
                </button>
              </div>
            </div>
            <p v-else class="depgraph-inspector__muted">Click a node to inspect it.</p>
          </section>
          <section class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Critical path</h3>
            <ol v-if="criticalNodes.length" class="depgraph-inspector__critical">
              <li v-for="n in criticalNodes" :key="n.id">{{ n.label }}</li>
            </ol>
            <p v-else class="depgraph-inspector__muted">
              No dependency chain yet — add depends-on links between specs or tasks.
            </p>
          </section>
        </aside>
      </div>
    </div>

    <TaskDetail v-if="detailTask" :task="detailTask" @close="detailTaskId = null" />
  </div>
</template>
