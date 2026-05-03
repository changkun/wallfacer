<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';
import type { Task } from '../../api/types';

const store = useTaskStore();

const deletedTasks = ref<Task[]>([]);
const trashLoading = ref(false);
const trashError = ref('');
const restoring = ref<Record<string, boolean>>({});

onMounted(() => {
  void loadDeletedTasks();
});

async function loadDeletedTasks() {
  trashLoading.value = true;
  trashError.value = '';
  try {
    deletedTasks.value = await api<Task[]>('GET', '/api/tasks/deleted');
  } catch (e) {
    console.error('load deleted tasks:', e);
    trashError.value = e instanceof Error ? e.message : 'Failed to load';
  } finally {
    trashLoading.value = false;
  }
}

async function restoreTask(id: string) {
  restoring.value = { ...restoring.value, [id]: true };
  try {
    await api('POST', `/api/tasks/${id}/restore`);
    deletedTasks.value = deletedTasks.value.filter((t) => t.id !== id);
    await store.fetchTasks();
  } catch (e) {
    console.error('restore task:', e);
    trashError.value = e instanceof Error ? e.message : 'Failed to restore';
  } finally {
    const next = { ...restoring.value };
    delete next[id];
    restoring.value = next;
  }
}

function dismissError() {
  trashError.value = '';
}

function formatDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="trash">
    <div
      style="
        margin-bottom: 8px;
        font-size: 11px;
        font-weight: 600;
        color: var(--text-muted);
        text-transform: uppercase;
        letter-spacing: 0.5px;
      "
    >
      Deleted Tasks
    </div>
    <div
      style="
        font-size: 11px;
        color: var(--text-muted);
        line-height: 1.4;
        margin-bottom: 12px;
      "
    >
      Soft-deleted tasks remain recoverable for 7 days.
    </div>
    <div v-if="trashError" class="trash-bin-banner" role="alert">
      <span>{{ trashError }}</span>
      <button
        type="button"
        class="trash-bin-banner__dismiss"
        aria-label="Dismiss trash error"
        @click="dismissError"
      >&times;</button>
    </div>
    <div v-if="trashLoading" class="trash-bin-loading">
      <span class="spinner" aria-hidden="true"></span>
      <span>Loading deleted tasks...</span>
    </div>
    <div v-else-if="deletedTasks.length === 0" class="trash-bin-empty">Trash is empty</div>
    <div v-else class="trash-bin-list" role="list">
      <div
        v-for="task in deletedTasks"
        :key="task.id"
        class="trash-bin-row"
        role="listitem"
      >
        <div class="trash-bin-row__main">
          <div class="trash-bin-row__title">
            {{ task.title || task.prompt || task.id }}
          </div>
          <div class="trash-bin-row__meta">
            {{ task.status }} &middot; updated {{ formatDate(task.updated_at) }}
          </div>
        </div>
        <button
          type="button"
          class="trash-bin-row__restore"
          :disabled="!!restoring[task.id]"
          @click="restoreTask(task.id)"
        >{{ restoring[task.id] ? 'Restoring...' : 'Restore' }}</button>
      </div>
    </div>
  </div>
</template>
