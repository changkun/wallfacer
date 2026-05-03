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

</script>

<template>
  <header class="app-header">
    <div class="app-header__spacer"></div>
    <div class="app-header__actions">
      <SearchBar />
    </div>
  </header>

  <main class="board-grid" id="board">
    <div class="col col-backlog">
      <div class="col-hd">
        <span class="col-dot" aria-hidden="true" />
        <span class="col-name">Backlog</span>
        <span class="col-count">{{ store.backlog.length }}</span>
      </div>
      <div class="column col-bg">
        <TaskComposer />
        <draggable :list="store.backlog" group="board" item-key="id" class="col-list" :animation="150" @change="onBacklogChange">
          <template #item="{ element }">
            <TaskCard :task="element" @click="selectTask(element)" />
          </template>
        </draggable>
      </div>
    </div>

    <div class="col col-progress">
      <div class="col-hd">
        <span class="col-dot" aria-hidden="true" />
        <span class="col-name">In Progress</span>
        <span class="col-count">{{ store.inProgress.length }}</span>
      </div>
      <div class="column col-bg">
        <draggable :list="store.inProgress" group="board" item-key="id" class="col-list" :animation="150" :sort="false" @change="onInProgressAdd">
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
        <span class="col-stats">${{ doneCost.toFixed(2) }}</span>
        <button v-if="store.done.length > 0" class="col-btn" @click="archiveAllDone">Archive all</button>
      </div>
      <div class="column col-bg">
        <TaskCard v-for="t in store.done" :key="t.id" :task="t" @click="selectTask(t)" />
      </div>
    </div>
  </main>

  <TaskDetail v-if="selectedTask" :task="selectedTask" @close="selectedTask = null" />
</template>
