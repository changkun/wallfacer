<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import draggable from 'vuedraggable';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';
import TaskCard from '../components/TaskCard.vue';
import TaskComposer from '../components/TaskComposer.vue';
import TaskDetail from '../components/TaskDetail.vue';
import SearchBar from '../components/SearchBar.vue';
import { sortBacklog, loadBacklogSortMode, saveBacklogSortMode, type BacklogSortMode } from '../lib/backlogSort';
import type { Task } from '../api/types';

const store = useTaskStore();
const ui = useUiStore();
const selectedTask = ref<Task | null>(null);

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

const maxParallel = computed(() => store.config?.max_parallel ?? 5);

// Empty-state composer: when the whole board is empty (no tasks across
// every column, archived or not), show a centred prompt + auto-expanded
// composer instead of the four columns. Mirrors ui/js/board-composer.js's
// #board-empty-composer slot.
const isEmptyBoard = computed(() =>
  store.tasks.length === 0 && !store.loading,
);

async function archiveAllDone() {
  await api('POST', '/api/tasks/archive-done');
}

onMounted(async () => {
  if (!store.tasks.length) await store.fetchTasks({ includeArchived: ui.showArchived });
  if (!store.config) await store.fetchConfig();
});

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
    for (let i = 0; i < ids.length; i++) {
      api('PATCH', `/api/tasks/${ids[i]}`, { position: i });
    }
  }
}

async function onInProgressAdd(evt: { added?: { element: Task } }) {
  if (evt.added) {
    await api('PATCH', `/api/tasks/${evt.added.element.id}`, { status: 'in_progress' });
  }
}

</script>

<template>
  <header class="app-header">
    <div class="app-header__spacer"></div>
    <div class="app-header__actions">
      <SearchBar />
      <div class="app-header__button-row">
        <router-link to="/explorer" class="settings-btn" title="Open Explorer">
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
        </router-link>
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

  <main v-if="isEmptyBoard" class="board-empty">
    <div class="board-empty__inner">
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
        <span class="col-icon-btn" aria-hidden="true" title="Sandbox Monitor">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="4 17 10 11 4 5" />
            <line x1="12" y1="19" x2="20" y2="19" />
          </svg>
        </span>
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

  <TaskDetail v-if="selectedTask" :task="selectedTask" @close="selectedTask = null" />
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
}
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
