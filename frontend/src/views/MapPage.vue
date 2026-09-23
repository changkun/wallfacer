<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useToastStore } from '../stores/toast';
import { useAgentStore } from '../stores/agentSession';
import { api } from '../api/client';
import GraphCanvas from '../components/map/GraphCanvas.vue';
import MapNodePopup from '../components/map/MapNodePopup.vue';
import { stateColor } from '../components/map/nodeColors';
import { ACTION_LABELS, hasPrimaryAction, primaryActions } from '../components/map/actions';
import SpecChatPopup from '../components/plan/SpecChatPopup.vue';
import TaskDetail from '../components/TaskDetail.vue';
import type { Graph, GraphNode, GraphAction, Task } from '../api/types';

// MapPage renders the unified spec+task graph served authoritatively by
// GET /api/graph (internal/graph). The legacy vendored depgraph renderer and
// its window-shim bridge are gone; the graph is fetched data, drawn by the
// hand-rolled GraphCanvas component.

const store = useTaskStore();
const toast = useToastStore();
const agentStore = useAgentStore();
const router = useRouter();
// The shared planning chat, mounted here so generative spec ops (refine,
// break-down, …) run from the graph without rebuilding chat. We just set the
// focused spec and open the same popup Plan uses.
const chatPopupRef = ref<InstanceType<typeof SpecChatPopup> | null>(null);

const graph = ref<Graph>({ nodes: [], edges: [], critical_path: [], blocked: [] });
const loadError = ref(false);
const showArchived = ref(false);
const mapSearch = ref('');
const selectedId = ref<string | null>(null);
const canvas = ref<InstanceType<typeof GraphCanvas> | null>(null);

// State filter. done + cancelled are noise on a pipeline overview, so they are
// hidden by default; the legend rows (right panel) act as the toggles.
const hiddenStatuses = ref<Set<string>>(new Set(['done', 'cancelled']));
// All statuses present in the current graph, for the legend/filter.
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
// Generative ops (refine, break-down, validate-by-agent, …) reuse the planning
// chat: focus its spec and open the shared popup, scoped to this node.
function discussSpec(specPath: string) {
  agentStore.focusSpec(specPath);
  chatPopupRef.value?.open();
}
function openInBoard(taskId: string) {
  if (store.tasks.some((t) => t.id === taskId)) detailTaskId.value = taskId;
}

// --- inline actions: drive the pipeline from the graph ---
// The server's available_actions is the source of truth; after any action we
// refetch the graph so the affordances re-sync (a failed action never leaves a
// stale button claiming something is possible).
const actionBusy = ref(false);

// runAction fires one node action against the real API and re-syncs the graph.
// 'start' promotes a task; every other verb is a spec transition. The server's
// available_actions stays the source of truth — a failure re-fetches so a stale
// button can't linger.
async function runAction(node: GraphNode, action: GraphAction) {
  if (actionBusy.value) return;
  actionBusy.value = true;
  try {
    if (action === 'start') {
      await api('PATCH', `/api/tasks/${node.ref}`, { status: 'in_progress' });
      toast.push('Task started', { kind: 'success' });
    } else {
      const resp = await api<{ dispatched?: { task_id: string }[] }>('POST', '/api/specs/transition', {
        action,
        paths: [node.ref],
        run: false,
      });
      const taskId = action === 'dispatch' ? resp.dispatched?.[0]?.task_id : undefined;
      if (taskId) {
        toast.pushWithAction('Spec dispatched to the board', 'View on Board →', () => {
          router.push({ path: '/', query: { task: taskId } });
        }, { kind: 'success' });
      } else {
        toast.push(`${ACTION_LABELS[action]} done`, { kind: 'success' });
      }
    }
  } catch (e) {
    toast.push(`${ACTION_LABELS[action]} failed: ` + (e instanceof Error ? e.message : String(e)), { kind: 'error' });
  } finally {
    actionBusy.value = false;
    await loadGraph();
  }
}

// Nodes the operator can act on right now — surfaced as an inspector list so
// "what's actionable" is legible without hunting the canvas.
const readyNodes = computed(() => graph.value.nodes.filter((n) => hasPrimaryAction(n.available_actions)));
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
  <div class="depgraph-mode-container mission">
    <div class="mission__inner">
      <header class="mission__head">
        <div class="mission__titles">
          <h1 class="mission__title">Mission Control</h1>
          <p class="mission__lede">
            Specs and tasks by dependency. Click a node to inspect and act,
            double-click a spec to open it, drag to reposition. Hold
            <kbd class="key">Space</kbd> and drag to pan;
            <kbd class="key">Ctrl</kbd> or <kbd class="key">&#8984;</kbd> + scroll to zoom.
          </p>
        </div>
        <div class="mission__actions">
          <div class="field-wrap mission__search">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"></circle><line x1="20" y1="20" x2="16.5" y2="16.5"></line></svg>
            <input
              v-model="mapSearch"
              type="search"
              class="field"
              placeholder="Filter nodes…"
              aria-label="Filter graph nodes"
            />
          </div>
          <label class="mission__option" title="Include archived specs and tasks">
            <input type="checkbox" v-model="showArchived" />
            Show archived
          </label>
          <button type="button" class="btn sm ghost" title="Clear filter and reset view" @click="onResetClick">
            Reset layout
          </button>
        </div>
      </header>
      <div class="mission__body">
        <div class="mission__mount">
          <div v-if="loadError" class="mission__empty">
            <p>Couldn't load the graph. <button type="button" class="btn sm ghost" @click="loadGraph">Retry</button></p>
          </div>
          <div v-else-if="!graph.nodes.length" class="mission__empty">
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
        <aside class="depgraph-inspector mission__inspector" aria-label="Graph inspector">
          <section class="card compact">
            <div class="card-head"><span class="eyebrow">Selection</span></div>
            <div v-if="selectedNode" class="card-pad mc-sel">
              <div class="mc-sel__pills">
                <span class="pill pill-neutral"><span class="pill-dot mc-dot" :style="{ background: stateColor(selectedNode.status) }"></span>{{ selectedNode.status.replace('_', ' ') }}</span>
                <span class="pill pill-neutral">{{ selectedNode.kind }}</span>
              </div>
              <p class="mc-sel__title">{{ selectedNode.label }}</p>
              <div class="mc-actions">
                <button
                  v-for="act in (selectedNode.available_actions || [])"
                  :key="act"
                  type="button"
                  class="btn sm mc-btn mc-btn--primary"
                  :disabled="actionBusy"
                  @click="runAction(selectedNode, act)"
                >
                  {{ ACTION_LABELS[act] }}
                </button>
                <button v-if="selectedNode.kind === 'spec'" type="button" class="btn sm ghost mc-btn" @click="discussSpec(selectedNode.ref)">
                  Refine / discuss
                </button>
                <button v-if="selectedNode.kind === 'spec'" type="button" class="btn sm ghost mc-btn" @click="openInPlan(selectedNode.ref)">
                  Open in Plan
                </button>
                <button
                  v-if="selectedNode.kind === 'task' && selectedTask"
                  type="button"
                  class="btn sm ghost mc-btn"
                  @click="openInBoard(selectedNode.ref)"
                >
                  Open in Board
                </button>
              </div>
            </div>
            <p v-else class="card-pad mission__muted">Click a node to inspect it.</p>
          </section>
          <section v-if="readyNodes.length" class="card compact">
            <div class="card-head"><span class="eyebrow">Ready to act</span><span class="count mc-count">{{ readyNodes.length }}</span></div>
            <ul class="rows mc-list">
              <li v-for="n in readyNodes" :key="n.id">
                <button type="button" class="row clickable mc-row" @click="onSelect(n.id)">
                  <span class="mc-dot" :style="{ background: stateColor(n.status) }"></span>
                  <span class="mc-row__label">{{ n.label }}</span>
                  <span class="mc-row__verbs">
                    <span v-for="a in primaryActions(n.available_actions)" :key="a" class="pill pill-brand mc-chip">{{ ACTION_LABELS[a] }}</span>
                  </span>
                </button>
              </li>
            </ul>
          </section>
          <section class="card compact">
            <div class="card-head"><span class="eyebrow">Critical path</span></div>
            <ol v-if="criticalNodes.length" class="rows mc-list mc-crit">
              <li v-for="n in criticalNodes" :key="n.id">
                <button type="button" class="row clickable mc-row" @click="onSelect(n.id)">
                  <span class="mc-dot" :style="{ background: stateColor(n.status) }"></span>
                  <span class="mc-row__label">{{ n.label }}</span>
                </button>
              </li>
            </ol>
            <p v-else class="card-pad mission__muted">
              No dependency chain yet. Add depends-on links between specs or tasks.
            </p>
          </section>
          <section v-if="allStatuses.length" class="card compact">
            <div class="card-head"><span class="eyebrow">Legend</span><span class="mission__muted map-legend__hint">Click a state to hide it</span></div>
            <ul class="map-legend">
              <li v-for="s in allStatuses" :key="s">
                <button
                  type="button"
                  class="map-legend__item"
                  :class="{ 'map-legend__item--off': hiddenStatuses.has(s) }"
                  :aria-pressed="!hiddenStatuses.has(s)"
                  @click="toggleStatus(s)"
                >
                  <span class="mc-dot map-legend__dot" :style="{ background: stateColor(s) }"></span>
                  <span class="map-legend__label">{{ s.replace('_', ' ') }}</span>
                </button>
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
      @discuss="popup && discussSpec(popup.path)"
      @close="popup = null"
    />
    <SpecChatPopup ref="chatPopupRef" />
  </div>
</template>

<style scoped>
/* Inspector cards: the selection, the ready and critical lists, the legend.
   Rows are the primitive at a tighter padding; the state dot carries the
   color so the list and the canvas never disagree. */
.mc-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex: 0 0 auto;
  display: inline-block;
}
.pill .mc-dot {
  width: 6px;
  height: 6px;
}
.mc-sel {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.mc-sel__pills {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.mc-sel__title {
  margin: 0;
  font-size: var(--fs-md);
  font-weight: 600;
  color: var(--ink);
  line-height: 1.35;
}
.mc-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.mc-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.mc-crit {
  counter-reset: mc-crit;
}
.mc-row {
  width: 100%;
  padding: 8px 14px;
  gap: 8px;
  border-left: none;
  border-right: none;
  border-bottom: none;
  background: none;
  text-align: left;
  font: inherit;
  font-size: var(--fs-base);
  color: var(--ink-2);
}
.mc-list > li:first-child .mc-row {
  border-top: none;
}
.mc-row:hover {
  color: var(--ink);
}
.mc-crit .mc-row::before {
  counter-increment: mc-crit;
  content: counter(mc-crit);
  flex: 0 0 auto;
  width: 14px;
  font: 500 var(--fs-10) / 1 var(--font-mono);
  color: var(--ink-4);
  font-variant-numeric: tabular-nums;
}
.mc-row__label {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mc-row__verbs {
  display: flex;
  gap: 4px;
  flex: 0 0 auto;
}
.map-legend {
  list-style: none;
  margin: 0;
  padding: 6px 8px 8px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px 6px;
}
.map-legend__item {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  min-height: 28px;
  padding: 0 8px;
  border: none;
  background: none;
  border-radius: var(--r-sm);
  font: inherit;
  font-size: var(--fs-10);
  color: var(--ink-2);
  cursor: pointer;
  text-align: left;
  transition: background var(--dur-hover);
}
.map-legend__item:hover {
  background: var(--bg-sunk);
}
/* A filtered-out state reads as muted and struck through. */
.map-legend__item--off {
  opacity: 0.45;
}
.map-legend__item--off .map-legend__label {
  text-decoration: line-through;
}
.map-legend__item--off .map-legend__dot {
  outline: 1px solid var(--rule-2);
  background: transparent !important;
}
.map-legend__label {
  text-transform: capitalize;
}
.map-legend__hint {
  font-size: var(--fs-9);
}
</style>
