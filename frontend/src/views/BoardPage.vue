<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import draggable from 'vuedraggable';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';
import TaskCard from '../components/TaskCard.vue';
import TaskComposer from '../components/TaskComposer.vue';
import TaskDetail from '../components/TaskDetail.vue';
import SearchBar from '../components/SearchBar.vue';
import ExplorerPanel from '../components/ExplorerPanel.vue';
import { sortBacklog, loadBacklogSortMode, saveBacklogSortMode, type BacklogSortMode } from '../lib/backlogSort';
import type { Task } from '../api/types';

// Empty-board composer dismissal, remembered for the tab session (survives SPA
// route remounts but clears on tab close). Mirrors ui/js/board-composer.js's
// _dismissedForSession flag.
const COMPOSER_DISMISS_KEY = 'wallfacer-empty-composer-dismissed';
function readComposerDismissed(): boolean {
  try { return typeof sessionStorage !== 'undefined' && sessionStorage.getItem(COMPOSER_DISMISS_KEY) === '1'; }
  catch { return false; }
}
const composerDismissed = ref(readComposerDismissed());
function dismissComposer() {
  composerDismissed.value = true;
  try { sessionStorage.setItem(COMPOSER_DISMISS_KEY, '1'); } catch { /* ignore */ }
}

const store = useTaskStore();
const ui = useUiStore();
const route = useRoute();
const router = useRouter();
const selectedTask = ref<Task | null>(null);
// Optional tab to open the detail modal on, carried via ?tab= (command-palette
// tab-switch jumps). Read once when the task opens.
const initialTab = computed(() => (typeof route.query.tab === 'string' ? route.query.tab : ''));

// Open the detail modal for ?task=<id> so the command palette (and deep links)
// can surface a task. Resolves the id against the loaded task list.
function syncSelectedFromQuery() {
  const id = typeof route.query.task === 'string' ? route.query.task : '';
  if (!id) { selectedTask.value = null; return; }
  if (selectedTask.value?.id === id) return;
  const t = store.tasks.find((x) => x.id === id);
  if (t) selectedTask.value = t;
}

function closeDetail() {
  selectedTask.value = null;
  if (route.query.task || route.query.tab) {
    const q = { ...route.query };
    delete q.task; delete q.tab;
    void router.replace({ path: '/', query: q });
  }
}

const backlogSortMode = ref<BacklogSortMode>(loadBacklogSortMode());
const displayedBacklog = computed(() => sortBacklog(store.backlog, backlogSortMode.value));
function toggleBacklogSort() {
  backlogSortMode.value = backlogSortMode.value === 'impact' ? 'manual' : 'impact';
  saveBacklogSortMode(backlogSortMode.value);
}

const doneCost = computed(() =>
  store.done.reduce((sum, t) => sum + (t.usage?.cost_usd || 0), 0),
);

const doneInputTokens = computed(() =>
  store.done.reduce((sum, t) => sum + (t.usage?.input_tokens || 0), 0),
);

const doneOutputTokens = computed(() =>
  store.done.reduce((sum, t) => sum + (t.usage?.output_tokens || 0), 0),
);

const doneStats = computed(() => {
  const i = doneInputTokens.value;
  const o = doneOutputTokens.value;
  const c = doneCost.value;
  if (!i && !o && !c) return '';
  return `${i.toLocaleString()} in / ${o.toLocaleString()} out / $${c.toFixed(2)}`;
});

// In-Progress "max N" cap. The live /api/config omits max_parallel, so the
// authoritative value is /api/env's max_parallel_tasks, with an optional
// per-group override from config.workspace_groups[].max_parallel. Mirrors the
// old ui/js render.js + workspace.js read path.
const envMaxParallel = ref<number>(0);
const maxParallel = computed<string | number>(() => {
  const cfg = store.config;
  const activeKey = cfg?.active_groups?.[0]?.key;
  const group = cfg?.workspace_groups?.find((g) => g.key === activeKey);
  // A per-group override of 0 is a deliberate "unlimited" setting.
  if (group?.max_parallel === 0) return '∞';
  if (group?.max_parallel != null) return group.max_parallel;
  if (envMaxParallel.value > 0) return envMaxParallel.value;
  return cfg?.max_parallel ?? 5;
});

// Empty-state composer: when the whole board is empty (no tasks across
// every column, archived or not), show a centred prompt + auto-expanded
// composer instead of the four columns. Mirrors ui/js/board-composer.js's
// #board-empty-composer slot.
const hasWorkspace = computed(() => (store.config?.workspaces?.length ?? 0) > 0);
const isEmptyBoard = computed(() =>
  hasWorkspace.value && store.tasks.length === 0 && !store.loading,
);
const needsWorkspace = computed(() => store.config != null && !hasWorkspace.value);

async function archiveAllDone() {
  await api('POST', '/api/tasks/archive-done');
}

// ── Mobile column navigation ──────────────────────────────────────
// On narrow screens the board is a horizontal snap-scroll; this pill bar jumps
// between columns and tracks the visible one via IntersectionObserver. Mirrors
// ui/js/utils.js initMobileColNav.
const MOBILE_COLS = ['Backlog', 'In Progress', 'Waiting', 'Done'];
const activeCol = ref(0);
let colObserver: IntersectionObserver | null = null;

function boardColumns(): HTMLElement[] {
  const board = document.getElementById('board');
  return board ? Array.from(board.querySelectorAll<HTMLElement>('.col')) : [];
}
function scrollToColumn(i: number) {
  boardColumns()[i]?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'start' });
}
function setupColObserver() {
  colObserver?.disconnect();
  const board = document.getElementById('board');
  const cols = boardColumns();
  if (!board || cols.length === 0) return;
  colObserver = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue;
        const idx = cols.indexOf(e.target as HTMLElement);
        if (idx >= 0) activeCol.value = idx;
      }
    },
    { root: board, threshold: 0.5 },
  );
  cols.forEach((c) => colObserver!.observe(c));
}

onMounted(async () => {
  if (!store.tasks.length) await store.fetchTasks({ includeArchived: ui.showArchived });
  if (!store.config) await store.fetchConfig();
  try {
    const env = await api<{ max_parallel_tasks?: number }>('GET', '/api/env');
    if (typeof env.max_parallel_tasks === 'number') envMaxParallel.value = env.max_parallel_tasks;
  } catch { /* env optional; fall back to config/default */ }
  syncSelectedFromQuery();
  await nextTick();
  setupColObserver();
});
onUnmounted(() => colObserver?.disconnect());
// Re-attach the observer when the board grid (re)appears, e.g. after the empty
// state clears once the first task lands.
watch(() => isEmptyBoard.value || needsWorkspace.value, () => { void nextTick(setupColObserver); });

// Open/replace the detail modal whenever ?task= changes (command palette,
// deep links) or once the task list arrives.
watch(() => route.query.task, syncSelectedFromQuery);
watch(() => store.tasks.length, syncSelectedFromQuery);

// Toggling "Show archived" needs a server round-trip — the server only
// returns archived rows when include_archived=true (see internal/handler/
// tasks.go). Without this re-fetch the in-memory list stays archive-less
// and the toggle does nothing.
watch(
  () => ui.showArchived,
  async (enabled) => {
    await store.fetchTasks({ includeArchived: enabled });
  },
);

function selectTask(t: Task) {
  selectedTask.value = t;
}

async function onBacklogChange(evt: { moved?: { element: Task } }) {
  if (evt.moved) {
    const ids = store.backlog.map(t => t.id);
    // Optimistic: write the new positions into the local store before
    // the SSE delta arrives so the order doesn't flicker back.
    ids.forEach((id, i) => store.patchTaskLocal(id, { position: i }));
    for (let i = 0; i < ids.length; i++) {
      api('PATCH', `/api/tasks/${ids[i]}`, { position: i });
    }
  }
}

async function onInProgressAdd(evt: { added?: { element: Task } }) {
  if (evt.added) {
    // Optimistic: flip the status locally so the card stays in
    // In Progress while the PATCH is in flight. Server SSE will
    // re-confirm; a failed PATCH rolls it back via the next fetch.
    store.patchTaskLocal(evt.added.element.id, { status: 'in_progress' });
    try {
      await api('PATCH', `/api/tasks/${evt.added.element.id}`, { status: 'in_progress' });
    } catch {
      store.patchTaskLocal(evt.added.element.id, { status: 'backlog' });
    }
  }
}

</script>

<template>
  <header class="app-header">
    <div class="app-header__spacer"></div>
    <div class="app-header__actions">
      <SearchBar />
      <div class="app-header__button-row">
        <button
          type="button"
          class="settings-btn"
          :class="{ 'settings-btn--active': ui.showExplorer }"
          :title="ui.showExplorer ? 'Close Explorer' : 'Open Explorer'"
          :aria-pressed="ui.showExplorer"
          @click="ui.toggleExplorer()"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
            <line x1="9" y1="9" x2="9" y2="21"></line>
          </svg>
        </button>
        <router-link
          to="/settings?tab=execution"
          class="settings-btn"
          title="Automation"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
          </svg>
        </router-link>
      </div>
    </div>
  </header>

  <div class="board-with-explorer">
    <!-- Left side panel; only renders once a workspace exists (nothing to
         browse before one is picked). The board stays visible beside it. -->
    <ExplorerPanel v-if="ui.showExplorer && hasWorkspace" @close="ui.closeExplorer()" />

  <main v-if="needsWorkspace" class="board-empty">
    <div class="board-empty__inner">
      <h1 class="board-empty__title">Pick a workspace to begin</h1>
      <p class="board-empty__hint">
        Wallfacer scopes every task to a workspace directory. Choose
        one (or more) before creating tasks.
      </p>
      <button
        type="button"
        class="composer__btn composer__btn--primary"
        @click="ui.showWorkspaces = true"
      >Open workspace picker</button>
    </div>
  </main>

  <main v-else-if="isEmptyBoard && !composerDismissed" class="board-empty">
    <div class="board-empty__inner">
      <button type="button" class="board-empty__dismiss" title="Dismiss" aria-label="Dismiss composer" @click="dismissComposer">&times;</button>
      <h1 class="board-empty__title">What do you want to work on?</h1>
      <p class="board-empty__hint">
        Describe a task to kick things off. The board fills in as soon as
        you save the first one.
      </p>
      <TaskComposer auto-expand class="board-empty__composer" />
    </div>
  </main>

  <main v-else class="board-grid" id="board">
    <div class="col col-backlog">
      <div class="col-hd">
        <span class="col-dot" aria-hidden="true" />
        <span class="col-name">Backlog</span>
        <span class="col-count">{{ store.backlog.length }}</span>
        <button
          type="button"
          class="col-btn"
          title="Toggle backlog sort order"
          @click="toggleBacklogSort"
        >Sort: {{ backlogSortMode === 'impact' ? 'Impact' : 'Manual' }}</button>
      </div>
      <div class="column col-bg">
        <TaskComposer />
        <draggable
          :list="displayedBacklog"
          :group="{ name: 'board', pull: true, put: false }"
          item-key="id"
          class="col-list"
          :animation="150"
          :sort="backlogSortMode !== 'impact'"
          ghost-class="card-drag-ghost"
          chosen-class="card-drag-chosen"
          @change="onBacklogChange"
        >
          <template #item="{ element, index }">
            <TaskCard :task="element" :rank="backlogSortMode === 'impact' ? undefined : index + 1" @click="selectTask(element)" />
          </template>
        </draggable>
      </div>
    </div>

    <div class="col col-progress">
      <div class="col-hd">
        <span class="col-dot" aria-hidden="true" />
        <span class="col-name">In Progress</span>
        <span class="col-count">{{ store.inProgress.length }}</span>
        <span class="max-parallel-tag" title="Max parallel tasks for this workspace group">max {{ maxParallel }}</span>
      </div>
      <div class="column col-bg">
        <draggable :list="store.inProgress" :group="{ name: 'board', pull: false, put: true }" item-key="id" class="col-list" :animation="150" :sort="false" @change="onInProgressAdd">
          <template #item="{ element }">
            <TaskCard :task="element" @click="selectTask(element)" />
          </template>
        </draggable>
      </div>
    </div>

    <div class="col col-waiting">
      <div class="col-hd">
        <span class="col-dot" aria-hidden="true" />
        <span class="col-name">Waiting</span>
        <span class="col-count">{{ store.waiting.length }}</span>
      </div>
      <div class="column col-bg">
        <TaskCard v-for="t in store.waiting" :key="t.id" :task="t" @click="selectTask(t)" />
      </div>
    </div>

    <div class="col col-done">
      <div class="col-hd">
        <span class="col-dot" aria-hidden="true" />
        <span class="col-name">Done</span>
        <span class="col-count">{{ store.done.length }}</span>
        <span v-if="doneStats" class="col-stats">{{ doneStats }}</span>
        <button v-if="store.done.length > 0" class="col-btn" @click="archiveAllDone">Archive all</button>
      </div>
      <div class="column col-bg">
        <TaskCard v-for="t in store.done" :key="t.id" :task="t" @click="selectTask(t)" />
      </div>
    </div>
  </main>
  </div>

  <!-- Mobile-only column nav: jump between the snap-scrolled columns. -->
  <nav v-if="!isEmptyBoard && !needsWorkspace" class="board-mobile-nav" aria-label="Board columns">
    <button
      v-for="(label, i) in MOBILE_COLS"
      :key="label"
      type="button"
      class="board-mobile-nav__btn"
      :class="{ active: activeCol === i }"
      @click="scrollToColumn(i)"
    >{{ label }}</button>
  </nav>

  <TaskDetail v-if="selectedTask" :task="selectedTask" :initial-tab="initialTab" @close="closeDetail" />
</template>

<style scoped>
.board-empty {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--sp-5);
}
.board-empty__inner {
  width: min(560px, 100%);
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 12px;
  text-align: center;
  position: relative;
}
.board-empty__dismiss {
  position: absolute;
  top: -4px;
  right: -4px;
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: var(--ink-3);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  border-radius: 6px;
}
.board-empty__dismiss:hover { color: var(--ink); background: var(--bg-hover); }
.board-empty__title {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 400;
  font-size: var(--fs-3xl, 28px);
  color: var(--ink);
  margin: 0;
}
.board-empty__hint {
  margin: 0 0 4px;
  color: var(--ink-3);
  font-size: var(--fs-md);
}
.board-empty__composer {
  text-align: left;
}
</style>
