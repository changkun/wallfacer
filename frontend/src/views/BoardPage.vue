<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import draggable from 'vuedraggable';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import TaskCard from '../components/TaskCard.vue';
import TaskComposer from '../components/TaskComposer.vue';
import TaskDetail from '../components/TaskDetail.vue';
import SearchBar from '../components/SearchBar.vue';
import type { Task } from '../api/types';

const store = useTaskStore();
const selectedTask = ref<Task | null>(null);

const doneCost = computed(() =>
  store.done.reduce((sum, t) => sum + (t.usage?.cost_usd || 0), 0),
);

async function archiveAllDone() {
  await api('POST', '/api/tasks/archive-done');
}

onMounted(async () => {
  if (!store.tasks.length) await store.fetchTasks();
  if (!store.config) await store.fetchConfig();
});

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

function statusColor(status: string): string {
  const colors: Record<string, string> = {
    backlog: 'var(--col-backlog)',
    in_progress: 'var(--col-progress)', committing: 'var(--col-progress)',
    waiting: 'var(--col-waiting)', failed: 'var(--col-waiting)',
    done: 'var(--col-done)', cancelled: 'var(--col-done)',
  };
  return colors[status] || 'var(--ink-3)';
}
</script>

<template>
  <div class="board">
    <SearchBar />

    <div class="board-columns">
      <section class="column">
        <div class="column-header">
          <span class="column-dot" :style="{ background: statusColor('backlog') }" />
          <span class="column-label">Backlog</span>
          <span class="column-count">{{ store.backlog.length }}</span>
        </div>
        <div class="column-body">
          <TaskComposer />
          <draggable :list="store.backlog" group="board" item-key="id" class="drag-zone" :animation="150" @change="onBacklogChange">
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
          <draggable :list="store.inProgress" group="board" item-key="id" class="drag-zone" :animation="150" :sort="false" @change="onInProgressAdd">
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
          <span class="column-cost">${{ doneCost.toFixed(2) }}</span>
          <button v-if="store.done.length > 0" class="archive-all-btn" @click="archiveAllDone">Archive all</button>
          <span class="column-count">{{ store.done.length }}</span>
        </div>
        <div class="column-body">
          <TaskCard v-for="t in store.done" :key="t.id" :task="t" @click="selectTask(t)" />
          <div v-if="store.done.length === 0" class="column-empty">None</div>
        </div>
      </section>
    </div>

    <div v-if="store.loading" class="board-loading">Loading tasks...</div>
    <TaskDetail v-if="selectedTask" :task="selectedTask" @close="selectedTask = null" />
  </div>
</template>

<style scoped>
.board {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}
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
.column-cost {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--ink-4);
}
.archive-all-btn {
  font-size: 10px;
  color: var(--ink-4);
  background: none;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm, 4px);
  padding: 1px 6px;
  cursor: pointer;
  text-transform: none;
  letter-spacing: normal;
  font-weight: 400;
}
.archive-all-btn:hover {
  color: var(--ink-2);
  border-color: var(--ink-4);
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
.drag-zone { min-height: 40px; }
.board-loading {
  position: fixed;
  bottom: 40px;
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
