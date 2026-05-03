<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import draggable from 'vuedraggable';
import { useTaskStore } from '../stores/tasks';
import { useBootStore } from '../stores/boot';
import { useSse } from '../composables/useSse';
import { useTheme } from '../composables/useTheme';
import { api } from '../api/client';
import TaskCard from '../components/TaskCard.vue';
import TaskComposer from '../components/TaskComposer.vue';
import TaskDetail from '../components/TaskDetail.vue';
import type { Task } from '../api/types';

const store = useTaskStore();
const boot = useBootStore();
const { theme, cycle } = useTheme();
const selectedTask = ref<Task | null>(null);

onMounted(async () => {
  await Promise.all([store.fetchTasks(), store.fetchConfig()]);
});

const { connected } = useSse({
  url: '/api/tasks/stream',
  listeners: {
    snapshot: (data) => store.setTasks(data as Task[]),
    'task-updated': (data) => {
      const t = data as Task;
      store.updateTask(t);
      if (selectedTask.value?.id === t.id) selectedTask.value = t;
    },
    'task-deleted': (data) => {
      const d = data as { id: string };
      store.removeTask(d.id);
      if (selectedTask.value?.id === d.id) selectedTask.value = null;
    },
  },
});

const totalCost = computed(() => {
  const sum = store.tasks.reduce((s, t) => s + (t.usage?.cost_usd || 0), 0);
  if (sum === 0) return '';
  return '$' + sum.toFixed(2);
});

const themeIcon = computed(() => {
  switch (theme.value) {
    case 'light': return '☀';
    case 'dark': return '☾';
    default: return '◐';
  }
});

function selectTask(t: Task) {
  selectedTask.value = t;
}

async function onBacklogChange(evt: { added?: { element: Task }; moved?: { element: Task; newIndex: number } }) {
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

function statusColor(status: string): string {
  switch (status) {
    case 'backlog': return 'var(--col-backlog)';
    case 'in_progress':
    case 'committing': return 'var(--col-progress)';
    case 'waiting':
    case 'failed': return 'var(--col-waiting)';
    case 'done':
    case 'cancelled': return 'var(--col-done)';
    default: return 'var(--ink-3)';
  }
}
</script>

<template>
  <div class="board">
    <header class="board-header">
      <span class="board-title">Wallfacer</span>
      <span class="board-version">{{ boot.version || 'dev' }}</span>
      <span class="header-spacer" />
      <span v-if="totalCost" class="header-cost">{{ totalCost }}</span>
      <span class="header-dot" :class="connected ? 'dot-ok' : 'dot-off'" :title="connected ? 'Connected' : 'Disconnected'" />
      <button class="header-theme" @click="cycle" :title="'Theme: ' + theme">{{ themeIcon }}</button>
    </header>

    <div class="board-columns">
      <section class="column">
        <div class="column-header">
          <span class="column-dot" :style="{ background: statusColor('backlog') }" />
          <span class="column-label">Backlog</span>
          <span class="column-count">{{ store.backlog.length }}</span>
        </div>
        <div class="column-body">
          <TaskComposer />
          <draggable
            :list="store.backlog"
            group="board"
            item-key="id"
            class="drag-zone"
            :animation="150"
            @change="onBacklogChange"
          >
            <template #item="{ element }">
              <TaskCard :task="element" @click="selectTask(element)" />
            </template>
          </draggable>
        </div>
      </section>

      <section class="column">
        <div class="column-header">
          <span class="column-dot" :style="{ background: statusColor('in_progress') }" />
          <span class="column-label">In Progress</span>
          <span class="column-count">{{ store.inProgress.length }}</span>
        </div>
        <div class="column-body">
          <draggable
            :list="store.inProgress"
            group="board"
            item-key="id"
            class="drag-zone"
            :animation="150"
            :sort="false"
            @change="onInProgressAdd"
          >
            <template #item="{ element }">
              <TaskCard :task="element" @click="selectTask(element)" />
            </template>
          </draggable>
          <div v-if="store.inProgress.length === 0" class="column-empty">Idle</div>
        </div>
      </section>

      <section class="column">
        <div class="column-header">
          <span class="column-dot" :style="{ background: statusColor('waiting') }" />
          <span class="column-label">Waiting</span>
          <span class="column-count">{{ store.waiting.length }}</span>
        </div>
        <div class="column-body">
          <TaskCard v-for="t in store.waiting" :key="t.id" :task="t" @click="selectTask(t)" />
          <div v-if="store.waiting.length === 0" class="column-empty">None</div>
        </div>
      </section>

      <section class="column">
        <div class="column-header">
          <span class="column-dot" :style="{ background: statusColor('done') }" />
          <span class="column-label">Done</span>
          <span class="column-count">{{ store.done.length }}</span>
        </div>
        <div class="column-body">
          <TaskCard v-for="t in store.done" :key="t.id" :task="t" @click="selectTask(t)" />
          <div v-if="store.done.length === 0" class="column-empty">None</div>
        </div>
      </section>
    </div>

    <div v-if="store.loading" class="board-loading">Loading tasks...</div>

    <TaskDetail
      v-if="selectedTask"
      :task="selectedTask"
      @close="selectedTask = null"
    />
  </div>
</template>

<style scoped>
.board {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-sans);
  font-size: 13px;
}

.board-header {
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--h-header);
  padding: 0 var(--sp-5);
  border-bottom: 1px solid var(--rule);
  flex-shrink: 0;
}
.board-title { font-weight: 600; font-size: 14px; }
.board-version {
  color: var(--ink-4);
  font-size: 11px;
  font-family: var(--font-mono);
}
.header-spacer { flex: 1; }
.header-cost {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
}
.header-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.dot-ok { background: var(--ok); }
.dot-off { background: var(--err); }
.header-theme {
  background: none;
  border: none;
  font-size: 14px;
  cursor: pointer;
  padding: 2px 4px;
  color: var(--ink-3);
}
.header-theme:hover { color: var(--ink); }

.board-columns {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 1px;
  flex: 1;
  overflow: hidden;
  background: var(--rule);
}

.column {
  display: flex;
  flex-direction: column;
  background: var(--bg);
  overflow: hidden;
}
.column-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--ink-3);
  border-bottom: 1px solid var(--rule);
  flex-shrink: 0;
}
.column-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.column-count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--ink-4);
}
.column-body {
  flex: 1;
  overflow-y: auto;
  padding: 6px;
}
.column-empty {
  padding: 20px 12px;
  text-align: center;
  color: var(--ink-4);
  font-size: 12px;
}

.drag-zone {
  min-height: 40px;
}

.board-loading {
  position: fixed;
  bottom: 16px;
  left: 50%;
  transform: translateX(-50%);
  padding: 6px 16px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  color: var(--ink-3);
  font-size: 12px;
  box-shadow: var(--sh-2);
}
</style>
