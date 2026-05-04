<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';
import type { Task } from '../../api/types';

const TRASH_BIN_RETENTION_DAYS = 7;

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
    const next = { ...restoring.value };
    delete next[id];
    restoring.value = next;
    return;
  }
  const next = { ...restoring.value };
  delete next[id];
  restoring.value = next;
}

function dismissError() {
  trashError.value = '';
}

function trashTitle(task: Task): string {
  if (task.title) return task.title;
  if (task.prompt) {
    return task.prompt.length > 60
      ? task.prompt.slice(0, 60) + '…'
      : task.prompt;
  }
  return task.id || 'Untitled task';
}

function statusLabel(task: Task): string {
  const s = task.status || 'backlog';
  return s.replace(/_/g, ' ');
}

function statusBadgeClass(task: Task): string {
  return 'badge badge-' + (task.status || 'backlog');
}

function deletedAgo(task: Task): string {
  const updatedAt = task.updated_at ? Date.parse(task.updated_at) : NaN;
  if (!Number.isFinite(updatedAt)) return 'unknown';
  const seconds = Math.floor((Date.now() - updatedAt) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) {
    const minutes = Math.floor(seconds / 60);
    return minutes === 1 ? '1 minute ago' : minutes + ' minutes ago';
  }
  if (seconds < 86400) {
    const hours = Math.floor(seconds / 3600);
    return hours === 1 ? '1 hour ago' : hours + ' hours ago';
  }
  const days = Math.floor(seconds / 86400);
  return days === 1 ? '1 day ago' : days + ' days ago';
}

function remainingDays(task: Task): string {
  const updatedAt = task.updated_at ? Date.parse(task.updated_at) : NaN;
  let days = 0;
  if (Number.isFinite(updatedAt)) {
    const elapsed = Math.floor((Date.now() - updatedAt) / 86400000);
    days = Math.max(0, TRASH_BIN_RETENTION_DAYS - elapsed);
  }
  return days === 1 ? '1 day remaining' : days + ' days remaining';
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
        :data-task-id="task.id"
      >
        <div class="trash-bin-row__main">
          <div class="trash-bin-row__title">{{ trashTitle(task) }}</div>
          <div class="trash-bin-row__meta">
            <span :class="statusBadgeClass(task)">{{ statusLabel(task) }}</span>
            <span>{{ deletedAgo(task) }}</span>
            <span class="trash-bin-row__retention">{{ remainingDays(task) }}</span>
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
