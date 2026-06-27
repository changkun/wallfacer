<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useToastStore } from '../stores/toast';
import { api } from '../api/client';
import GraphCanvas from '../components/map/GraphCanvas.vue';
import MapNodePopup from '../components/map/MapNodePopup.vue';
import { stateColor } from '../components/map/nodeColors';
import TaskDetail from '../components/TaskDetail.vue';
import type { Graph, GraphNode, Task } from '../api/types';

// MapPage renders the unified spec+task graph served authoritatively by
// GET /api/graph (internal/graph). The legacy vendored depgraph renderer and
// its window-shim bridge are gone; the graph is fetched data, drawn by the
// hand-rolled GraphCanvas component.

const store = useTaskStore();
const toast = useToastStore();
const router = useRouter();

const graph = ref<Graph>({ nodes: [], edges: [], critical_path: [], blocked: [] });
const loadError = ref(false);
const showArchived = ref(false);
const mapSearch = ref('');
const selectedId = ref<string | null>(null);
const canvas = ref<InstanceType<typeof GraphCanvas> | null>(null);

// Status filter (top-right). done + cancelled are noise on a pipeline overview,
// so they are hidden by default; the dropdown lets the user toggle any state.
const hiddenStatuses = ref<Set<string>>(new Set(['done', 'cancelled']));
const statusMenuOpen = ref(false);
// All statuses present in the current graph, for the filter menu.
const allStatuses = computed(() =>
  [...new Set(graph.value.nodes.map((n) => n.status))].sort(),
);
function toggleStatus(s: string) {
  const next = new Set(hiddenStatuses.value);
  if (next.has(s)) next.delete(s);
  else next.add(s);
  hiddenStatuses.value = next;
}

// Spec popup opened by double-clicking a spec node.
const popup = ref<{ path: string; title: string } | null>(null);

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

onMounted(async () => {
  // Ensure the task store is populated even when /map is the first route hit:
  // openInBoard / selectedTask / detailTask all read store.tasks, so a cold
  // load would otherwise offer no Board deep-jump for task nodes.
  if (!store.tasks.length) await store.fetchTasks();
  if (!store.config) await store.fetchConfig(); // workspaces, for the spec popup
  await loadGraph();
});

// Apply label-substring search and the status filter; keep only edges whose
// endpoints both survive. Pruning critical_path/blocked to surviving nodes
// keeps the inspector consistent with what's drawn.
const filteredGraph = computed<Graph>(() => {
  const q = mapSearch.value.trim().toLowerCase();
  const hidden = hiddenStatuses.value;
  const nodes = graph.value.nodes.filter(
    (n) => !hidden.has(n.status) && (!q || n.label.toLowerCase().includes(q)),
  );
  if (nodes.length === graph.value.nodes.length) return graph.value;
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
// Double-click: open the spec in a popup (task nodes open their Board detail).
function onOpen(id: string) {
  const n = graph.value.nodes.find((x) => x.id === id);
  if (!n) return;
  if (n.kind === 'spec') popup.value = { path: n.ref, title: n.label };
  else openInBoard(n.ref);
}
function openInPlan(path: string) {
  void router.push({ path: '/plan', query: { spec: path } });
}
function openInBoard(taskId: string) {
  if (store.tasks.some((t) => t.id === taskId)) detailTaskId.value = taskId;
}

// --- inline actions: drive the pipeline from the graph ---
// The server's available_actions is the source of truth; after any action we
// refetch the graph so the affordances re-sync (a failed action never leaves a
// stale button claiming something is possible).
const actionBusy = ref(false);

function hasAction(node: GraphNode | null, action: string): boolean {
  return !!node && (node.available_actions ?? []).includes(action as never);
}

async function dispatchSpec(node: GraphNode) {
  if (actionBusy.value) return;
  actionBusy.value = true;
  try {
    const resp = await api<{ dispatched?: { task_id: string }[] }>('POST', '/api/specs/transition', {
      action: 'dispatch',
      paths: [node.ref],
      run: false,
    });
    const taskId = resp.dispatched?.[0]?.task_id;
    if (taskId) {
      toast.pushWithAction('Spec dispatched to the board', 'View on Board →', () => {
        router.push({ path: '/', query: { task: taskId } });
      }, { kind: 'success' });
    } else {
      toast.push('Spec dispatched', { kind: 'success' });
    }
  } catch (e) {
    toast.push('Dispatch failed: ' + (e instanceof Error ? e.message : String(e)), { kind: 'error' });
  } finally {
    actionBusy.value = false;
    await loadGraph();
  }
}

async function startTask(node: GraphNode) {
  if (actionBusy.value) return;
  actionBusy.value = true;
  try {
    await api('PATCH', `/api/tasks/${node.ref}`, { status: 'in_progress' });
    toast.push('Task started', { kind: 'success' });
  } catch (e) {
    toast.push('Start failed: ' + (e instanceof Error ? e.message : String(e)), { kind: 'error' });
  } finally {
    actionBusy.value = false;
    await loadGraph();
  }
}

// Nodes the operator can act on right now — surfaced as an inspector list so
// "what's actionable" is legible without hunting the canvas.
const readyNodes = computed(() => graph.value.nodes.filter((n) => (n.available_actions?.length ?? 0) > 0));
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
          <div class="map-statefilter" @pointerleave="statusMenuOpen = false">
            <button
              type="button"
              class="depgraph-mode__reset-btn"
              :aria-expanded="statusMenuOpen"
              title="Show or hide nodes by state"
              @click="statusMenuOpen = !statusMenuOpen"
            >
              States{{ hiddenStatuses.size ? ` (${hiddenStatuses.size} hidden)` : '' }} ▾
            </button>
            <div v-if="statusMenuOpen" class="map-statefilter__menu">
              <label v-for="s in allStatuses" :key="s" class="map-statefilter__item">
                <input type="checkbox" :checked="!hiddenStatuses.has(s)" @change="toggleStatus(s)" />
                <span class="map-statefilter__label">{{ s }}</span>
              </label>
            </div>
          </div>
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
          <GraphCanvas
            v-else
            ref="canvas"
            :graph="filteredGraph"
            :selected-id="selectedId"
            @select="onSelect"
            @open="onOpen"
          />
        </div>
        <aside class="depgraph-inspector" aria-label="Graph inspector">
          <section class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Selection</h3>
            <div v-if="selectedNode" class="depgraph-inspector__selection">
              <p><strong>{{ selectedNode.label }}</strong></p>
              <p class="depgraph-inspector__muted">{{ selectedNode.kind }} · {{ selectedNode.status }}</p>
              <div class="depgraph-inspector__actions">
                <button
                  v-if="hasAction(selectedNode, 'dispatch')"
                  type="button"
                  class="depgraph-inspector__action--primary"
                  :disabled="actionBusy"
                  @click="dispatchSpec(selectedNode)"
                >
                  Dispatch
                </button>
                <button
                  v-if="hasAction(selectedNode, 'start')"
                  type="button"
                  class="depgraph-inspector__action--primary"
                  :disabled="actionBusy"
                  @click="startTask(selectedNode)"
                >
                  Start
                </button>
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
          <section v-if="readyNodes.length" class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Ready to act</h3>
            <ul class="depgraph-inspector__ready">
              <li v-for="n in readyNodes" :key="n.id">
                <button type="button" class="depgraph-inspector__ready-item" @click="onSelect(n.id)">
                  {{ n.label }}
                  <span class="depgraph-inspector__ready-tag">{{ (n.available_actions || []).join(', ') }}</span>
                </button>
              </li>
            </ul>
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
          <section v-if="allStatuses.length" class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Legend</h3>
            <ul class="map-legend">
              <li v-for="s in allStatuses" :key="s" class="map-legend__item">
                <span class="map-legend__dot" :style="{ background: stateColor(s) }"></span>
                <span class="map-legend__label">{{ s }}</span>
              </li>
            </ul>
          </section>
        </aside>
      </div>
    </div>

    <TaskDetail v-if="detailTask" :task="detailTask" @close="detailTaskId = null" />
    <MapNodePopup
      v-if="popup"
      :path="popup.path"
      :title="popup.title"
      :workspaces="store.config?.workspaces ?? []"
      @close="popup = null"
    />
  </div>
</template>

<style scoped>
.map-statefilter {
  position: relative;
}
.map-statefilter__menu {
  position: absolute;
  right: 0;
  top: calc(100% + 4px);
  z-index: 20;
  min-width: 160px;
  padding: 6px;
  background: var(--bg-card, #fff);
  border: 1px solid var(--rule-2, #c7c0af);
  border-radius: var(--r-md, 6px);
  box-shadow: var(--sh-3, 0 8px 24px rgba(0, 0, 0, 0.16));
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.map-statefilter__item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 8px;
  border-radius: var(--r-sm, 4px);
  font-size: var(--fs-md, 13px);
  color: var(--ink-2, #4c4842);
  cursor: pointer;
}
.map-statefilter__item:hover {
  background: var(--bg-hover, rgba(31, 29, 26, 0.045));
}
.map-statefilter__label {
  text-transform: capitalize;
}

.map-legend {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 4px 10px;
}
.map-legend__item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--fs-10, 11px);
  color: var(--ink-2, #4c4842);
}
.map-legend__dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex: 0 0 auto;
}
.map-legend__label {
  text-transform: capitalize;
}
</style>
