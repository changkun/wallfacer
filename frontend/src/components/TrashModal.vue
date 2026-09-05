<script setup lang="ts">
import { ref, watch } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useToastStore } from '../stores/toast';
import type { Task } from '../api/types';
import { statusPill } from '../lib/statusPill';

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
  return 'pill ' + statusPill(task.status || 'backlog');
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
    <div
      v-if="modelValue"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      @click.self="close"
    >
      <div class="dialog dialog--wide trash-modal" role="dialog" aria-label="Trash">
        <div class="dialog-head">
          <div class="dialog-head__main">
            <h2 class="dialog-title">Trash</h2>
            <p class="dialog-sub">Deleted tasks are recoverable for {{ TRASH_BIN_RETENTION_DAYS }} days.</p>
          </div>
          <button type="button" class="icon-btn" aria-label="Close" @click="close">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
          </button>
        </div>

        <div class="dialog-body">
          <div v-if="trashError" class="trash-bin-banner" role="alert">
            <span>{{ trashError }}</span>
            <button type="button" class="trash-bin-banner__dismiss" aria-label="Dismiss error" @click="dismissError">&times;</button>
          </div>

          <div v-if="trashLoading" class="trash-bin-loading">
            <span class="spinner" aria-hidden="true"></span>
            <span>Loading deleted tasks…</span>
          </div>
          <div v-else-if="deletedTasks.length === 0" class="trash-modal__empty">
            <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
            </svg>
            <span>Trash is empty</span>
          </div>
          <div v-else class="card compact">
            <div class="rows trash-modal__list" role="list">
              <div
                v-for="task in deletedTasks"
                :key="task.id"
                class="row trash-modal__row"
                role="listitem"
              >
                <div class="row-main">
                  <div class="row-title">{{ trashTitle(task) }}</div>
                  <div class="row-meta trash-modal__meta">
                    <span :class="statusBadgeClass(task)">{{ statusLabel(task) }}</span>
                    <span>{{ deletedAgo(task) }}</span>
                    <span class="trash-modal__retention">{{ remainingDays(task) }}</span>
                  </div>
                </div>
                <div class="row-end">
                  <button
                    type="button"
                    class="btn sm ghost trash-modal__restore"
                    :disabled="!!restoring[task.id]"
                    @click="restoreTask(task.id)"
                  >{{ restoring[task.id] ? 'Restoring…' : 'Restore' }}</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* The trash is the wide dialog: a card of rows, one per deleted task, with
   the state pill, the deletion age, the days left in warn, and a ghost Restore. */
.trash-modal__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 36px 0 28px;
  color: var(--ink-3);
  font-size: var(--fs-md);
}
.trash-modal__meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 2px;
}
.trash-modal__retention {
  color: var(--warn);
}
.trash-modal__restore:disabled {
  cursor: progress;
}
</style>
