<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import TaskDetail from '../components/TaskDetail.vue';
import type { Task } from '../api/types';

// MapPage hosts the depgraph + unified-graph renderers, vendored verbatim
// into frontend/src/vendor/depgraph/. We render the same DOM the legacy
// ui/partials/depgraph-mode.html provided, install a handful of `window`
// shims the renderers expect, then call window.renderDependencyGraph()
// reactively. The vendored copies are self-contained (no ui/ imports), so
// frontend/ no longer depends on ui/ for the Map view.

interface SpecMeta { status: string; dispatched_task_id: string | null }
interface SpecNode { path: string; spec: SpecMeta; children: string[]; is_leaf: boolean; depth: number }
interface SpecTreeResponse { nodes: SpecNode[] }

// The window shims these legacy renderers expect are declared ambiently in
// src/env.d.ts so test files can reference them without importing this SFC.

const store = useTaskStore();
const router = useRouter();

const selectedTask = ref<Task | null>(null);
const specTree = ref<SpecNode[]>([]);
const ready = ref(false);

// Stable shared state object — the legacy renderer reads this once during
// each call to renderDependencyGraph. We mutate `tree` in-place so the
// reference held by depgraph.js stays current as the spec tree refreshes.
const sharedSpecState = { tree: [] as SpecNode[], index: null as unknown };

// Track the previous values of the window properties we replace so we can
// restore them on unmount, avoiding leakage across route changes.
const previous: Record<string, unknown> = {};
function setShim<K extends keyof Window>(key: K, value: Window[K]) {
  previous[key as string] = window[key];
  (window as unknown as Record<string, unknown>)[key as string] = value as unknown;
}
function restoreShims() {
  for (const k of Object.keys(previous)) {
    (window as unknown as Record<string, unknown>)[k] = previous[k];
  }
}

function currentTasks(): Task[] {
  return store.tasks;
}

function rerender() {
  if (!ready.value) return;
  if (typeof window.renderDependencyGraph !== 'function') return;
  window.renderDependencyGraph(currentTasks());
}

async function loadSpecTree() {
  try {
    const resp = await api<SpecTreeResponse>('GET', '/api/specs/tree');
    specTree.value = resp.nodes ?? [];
    sharedSpecState.tree.length = 0;
    sharedSpecState.tree.push(...specTree.value);
  } catch (e) {
    console.error('MapPage: spec tree load failed', e);
  }
}

function onShowArchivedChange(e: Event) {
  const target = e.target as HTMLInputElement;
  window.setMapShowArchived?.(target.checked);
}

function onResetClick() {
  mapSearch.value = '';
  window.resetMapLayout?.();
}

// Map-mode search: filters graph nodes by label substring via the depgraph
// lib's setMapSearch hook. This is the spec/depgraph leg of the legacy
// mode-aware filter routing.
const mapSearch = ref('');
function onMapSearchInput() {
  window.setMapSearch?.(mapSearch.value);
}

onMounted(async () => {
  // Install window shims BEFORE importing the legacy IIFE modules so that
  // any free-identifier lookups (typeof specModeState, typeof scheduleRender)
  // resolve through the global object to our shims.
  setShim('specModeState', sharedSpecState);
  setShim('depGraphEnabled', true);
  setShim('openTaskModal', (id: string) => {
    const t = store.tasks.find(x => x.id === id);
    if (t) selectedTask.value = t;
  });
  setShim('focusSpec', (path: string) => {
    void router.push({ path: '/plan', query: { spec: path } });
  });
  setShim('switchMode', () => { /* no-op in Vue: routing handles this */ });
  setShim('scheduleRender', () => {
    // Defer to the next animation frame so coalesced state changes (search
    // input, focus toggle, expand-spec) batch into a single render.
    requestAnimationFrame(() => rerender());
  });

  // Ensure tasks and spec tree are loaded.
  if (!store.tasks.length) await store.fetchTasks();
  await loadSpecTree();

  // Dynamic side-effect import; the IIFEs attach to window at execution.
  await import('../vendor/depgraph/unified-graph.js');
  await import('../vendor/depgraph/depgraph.js');

  ready.value = true;
  await nextTick();
  rerender();
});

onBeforeUnmount(() => {
  window.hideDependencyGraph?.();
  window._resetMapCentering?.();
  // restoreShims() sets depGraphEnabled back to its prior value
  // (undefined in normal operation), which falsy-checks the same as
  // false in the legacy pan/zoom guards.
  restoreShims();
});

// Re-render whenever the task list or spec tree changes. The legacy
// renderer fingerprints the input and short-circuits when nothing has
// actually changed, so calling on every store mutation is cheap.
watch(() => store.tasks, () => rerender(), { deep: false });
watch(specTree, () => rerender(), { deep: false });
</script>

<template>
  <div id="depgraph-mode-container" class="depgraph-mode-container">
    <div class="depgraph-mode__inner">
      <header class="depgraph-mode__header">
        <div class="depgraph-mode__titles">
          <h2 class="depgraph-mode__title">Map</h2>
          <p class="depgraph-mode__subtitle">
            Specs and tasks by dependency. Click a node to focus;
            <kbd>Shift</kbd>+click to open. Drag to pin, double-click to un-pin.
            Hold <kbd>Space</kbd> and drag to pan;
            <kbd>Ctrl</kbd>/<kbd>&#8984;</kbd>+scroll to zoom.
          </p>
        </div>
        <div class="depgraph-mode__actions">
          <input
            v-model="mapSearch"
            type="search"
            class="depgraph-mode__search"
            placeholder="Filter nodes…"
            aria-label="Filter graph nodes"
            @input="onMapSearchInput"
          />
          <label class="depgraph-mode__option" title="Include archived specs and tasks">
            <input type="checkbox" id="depgraph-show-archived" @change="onShowArchivedChange" />
            Show archived
          </label>
          <button
            type="button"
            id="depgraph-reset-btn"
            class="depgraph-mode__reset-btn"
            title="Clear pinned positions, focus, zoom and search"
            @click="onResetClick"
          >
            Reset layout
          </button>
        </div>
      </header>
      <div class="depgraph-mode__body">
        <div id="depgraph-mount" class="depgraph-mode__mount">
          <div class="depgraph-mode__empty">
            <p>
              No dependency edges yet. Add depends-on links between tasks to see
              the graph &mdash; drag a card from Backlog onto another, or set
              dependencies via the task modal.
            </p>
          </div>
        </div>
        <aside id="depgraph-inspector" class="depgraph-inspector" aria-label="Graph inspector">
          <section class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Legend</h3>
            <div class="depgraph-inspector__legend">
              <div class="depgraph-inspector__legend-group">
                <div class="depgraph-inspector__legend-subheading">Task</div>
                <ul>
                  <li><span class="depgraph-inspector__swatch" data-task-status="in_progress"></span>In progress</li>
                  <li><span class="depgraph-inspector__swatch" data-task-status="waiting"></span>Waiting</li>
                  <li><span class="depgraph-inspector__swatch" data-task-status="done"></span>Done</li>
                  <li><span class="depgraph-inspector__swatch" data-task-status="backlog"></span>Backlog</li>
                  <li><span class="depgraph-inspector__swatch" data-task-status="failed"></span>Failed</li>
                </ul>
              </div>
              <div class="depgraph-inspector__legend-group">
                <div class="depgraph-inspector__legend-subheading">Spec</div>
                <ul>
                  <li><span class="depgraph-inspector__swatch depgraph-inspector__swatch--spec" data-spec-status="drafted"></span>Drafted</li>
                  <li><span class="depgraph-inspector__swatch depgraph-inspector__swatch--spec" data-spec-status="validated"></span>Validated</li>
                  <li><span class="depgraph-inspector__swatch depgraph-inspector__swatch--spec" data-spec-status="complete"></span>Complete</li>
                  <li><span class="depgraph-inspector__swatch depgraph-inspector__swatch--spec" data-spec-status="stale"></span>Stale</li>
                </ul>
              </div>
              <div class="depgraph-inspector__legend-group">
                <div class="depgraph-inspector__legend-subheading">Edges</div>
                <ul>
                  <li><span class="depgraph-inspector__edge depgraph-inspector__edge--containment"></span>Contains</li>
                  <li><span class="depgraph-inspector__edge depgraph-inspector__edge--dispatch"></span>Dispatch</li>
                  <li><span class="depgraph-inspector__edge depgraph-inspector__edge--spec-dep"></span>Spec dep</li>
                  <li><span class="depgraph-inspector__edge depgraph-inspector__edge--task-dep"></span>Task dep</li>
                </ul>
              </div>
            </div>
          </section>
          <section class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Selection</h3>
            <div id="depgraph-inspector-selection" class="depgraph-inspector__selection">
              <p class="depgraph-inspector__muted">
                Click a node to focus its neighbourhood. Shift+click opens the
                task or spec.
              </p>
            </div>
          </section>
          <section class="depgraph-inspector__section">
            <h3 class="depgraph-inspector__heading">Critical path</h3>
            <div id="depgraph-inspector-critical" class="depgraph-inspector__critical">
              <p class="depgraph-inspector__muted">
                Longest dependency chain appears here once the graph has edges.
              </p>
            </div>
          </section>
        </aside>
      </div>
    </div>

    <TaskDetail v-if="selectedTask" :task="selectedTask" @close="selectedTask = null" />
  </div>
</template>
