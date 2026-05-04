<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import TaskDetail from '../components/TaskDetail.vue';
import type { Task } from '../api/types';

// MapPage hosts the legacy depgraph + unified-graph renderers verbatim from
// ui/js/. We render the same DOM that ui/partials/depgraph-mode.html
// provides, install a handful of `window` shims the legacy modules expect,
// then call window.renderDependencyGraph() reactively.
//
// The legacy modules are imported via Vite's `fs.allow: ['..']` so the old
// UI and the new UI share a single source of truth. When ui/ is removed in
// Phase 5 of the migration spec, `git mv` the two files into
// frontend/src/lib/depgraph/ and update the imports below.

interface SpecMeta { status: string; dispatched_task_id: string | null }
interface SpecNode { path: string; spec: SpecMeta; children: string[]; is_leaf: boolean; depth: number }
interface SpecTreeResponse { nodes: SpecNode[] }

declare global {
  interface Window {
    specModeState?: { tree: SpecNode[]; index: unknown };
    depGraphEnabled?: boolean;
    openTaskModal?: (id: string) => void;
    focusSpec?: (path: string) => void;
    switchMode?: (mode: string, opts?: { persist?: boolean }) => void;
    scheduleRender?: () => void;
    renderDependencyGraph?: (tasks: Task[]) => void;
    hideDependencyGraph?: () => void;
    setMapShowArchived?: (v: boolean) => void;
    setMapSearch?: (q: string) => void;
    resetMapLayout?: () => void;
    _resetMapCentering?: () => void;
  }
}

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
  window.resetMapLayout?.();
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
  await import('../../../ui/js/unified-graph.js');
  await import('../../../ui/js/depgraph.js');

  ready.value = true;
  await nextTick();
  rerender();
});

onBeforeUnmount(() => {
  window.hideDependencyGraph?.();
  window._resetMapCentering?.();
  // Mark Map mode inactive so any lingering pan/zoom listeners no-op.
  window.depGraphEnabled = false;
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
