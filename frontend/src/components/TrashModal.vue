<script setup lang="ts">
import { ref, watch } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useToastStore } from '../stores/toast';
import type { Task } from '../api/types';

// Board-scoped trash: soft-deleted tasks are a property of the board, so this
// lives on the board (a popup) rather than in global Settings.
const TRASH_BIN_RETENTION_DAYS = 7;

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

const store = useTaskStore();
const toast = useToastStore();

const deletedTasks = ref<Task[]>([]);
const trashLoading = ref(false);
const trashError = ref('');
const restoring = ref<Record<string, boolean>>({});

function close() {
  emit('update:modelValue', false);
}

// Reload each time the modal opens so the list is always fresh.
watch(
  () => props.modelValue,
  (open) => {
    if (open) void loadDeletedTasks();
  },
  { immediate: true },
);

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
    await api('PATCH', `/api/tasks/${id}`, { deleted: false });
    deletedTasks.value = deletedTasks.value.filter((t) => t.id !== id);
    await store.fetchTasks();
    toast.push('Task restored to the board', { kind: 'success' });
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

function trashTitle(task: Task): string {
  if (task.title) return task.title;
  if (task.prompt) {
    return task.prompt.length > 60 ? task.prompt.slice(0, 60) + '…' : task.prompt;
  }
  return task.id || 'Untitled task';
}

function statusLabel(task: Task): string {
  return (task.status || 'backlog').replace(/_/g, ' ');
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
  return days === 1 ? '1 day left' : days + ' days left';
}
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="modal-overlay" @click.self="close">
      <div class="trash-modal" role="dialog" aria-label="Trash">
        <header class="trash-modal__head">
          <div class="trash-modal__heading">
            <h2 class="trash-modal__title">Trash</h2>
            <p class="trash-modal__sub">Deleted tasks are recoverable for {{ TRASH_BIN_RETENTION_DAYS }} days.</p>
          </div>
          <button type="button" class="trash-modal__close" aria-label="Close" @click="close">&times;</button>
        </header>

        <div v-if="trashError" class="trash-bin-banner" role="alert">
          <span>{{ trashError }}</span>
          <button type="button" class="trash-bin-banner__dismiss" aria-label="Dismiss error" @click="dismissError">&times;</button>
        </div>

        <div class="trash-modal__body">
          <div v-if="trashLoading" class="trash-bin-loading">
            <span class="spinner" aria-hidden="true"></span>
            <span>Loading deleted tasks…</span>
          </div>
          <div v-else-if="deletedTasks.length === 0" class="trash-modal__empty">
            <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
            </svg>
            <span>Trash is empty</span>
          </div>
          <div v-else class="trash-modal__list" role="list">
            <div
              v-for="task in deletedTasks"
              :key="task.id"
              class="trash-modal__row"
              role="listitem"
            >
              <div class="trash-modal__main">
                <div class="trash-modal__row-title">{{ trashTitle(task) }}</div>
                <div class="trash-modal__meta">
                  <span :class="statusBadgeClass(task)">{{ statusLabel(task) }}</span>
                  <span>{{ deletedAgo(task) }}</span>
                  <span class="trash-modal__retention">{{ remainingDays(task) }}</span>
                </div>
              </div>
              <button
                type="button"
                class="trash-modal__restore"
                :disabled="!!restoring[task.id]"
                @click="restoreTask(task.id)"
              >{{ restoring[task.id] ? 'Restoring…' : 'Restore' }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.trash-modal {
  width: min(560px, calc(100vw - 32px));
  max-height: min(70vh, 640px);
  display: flex;
  flex-direction: column;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-xl);
  box-shadow: var(--sh-pop);
  overflow: hidden;
}
.trash-modal__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--sp-4);
  padding: var(--sp-5) var(--sp-5) var(--sp-4);
  border-bottom: 1px solid var(--rule);
}
.trash-modal__title {
  margin: 0;
  font-size: var(--fs-xl);
  font-weight: 600;
  color: var(--ink);
}
.trash-modal__sub {
  margin: 2px 0 0;
  font-size: var(--fs-base);
  color: var(--ink-3);
}
.trash-modal__close {
  background: none;
  border: none;
  color: var(--ink-3);
  font-size: 22px;
  line-height: 1;
  cursor: pointer;
  padding: 0 4px;
}
.trash-modal__close:hover {
  color: var(--ink);
}
.trash-modal__body {
  overflow-y: auto;
  padding: var(--sp-3) var(--sp-4) var(--sp-5);
}
.trash-modal__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-3);
  padding: var(--sp-7) 0;
  color: var(--ink-3);
  font-size: var(--fs-md);
}
.trash-modal__list {
  display: flex;
  flex-direction: column;
}
.trash-modal__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-4);
  padding: var(--sp-3);
  border-radius: var(--r-md);
  border-bottom: 1px solid var(--rule);
}
.trash-modal__row:last-child {
  border-bottom: none;
}
.trash-modal__row:hover {
  background: var(--bg-hover);
}
.trash-modal__main {
  min-width: 0;
}
.trash-modal__row-title {
  font-size: var(--fs-md);
  color: var(--ink);
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.trash-modal__meta {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  margin-top: 3px;
  font-size: var(--fs-10);
  color: var(--ink-3);
}
.trash-modal__retention {
  color: var(--warn);
}
.trash-modal__restore {
  flex-shrink: 0;
  font-size: var(--fs-base);
  padding: 5px var(--sp-4);
  background: var(--bg-input);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink);
  cursor: pointer;
}
.trash-modal__restore:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}
.trash-modal__restore:disabled {
  opacity: 0.6;
  cursor: progress;
}
</style>
