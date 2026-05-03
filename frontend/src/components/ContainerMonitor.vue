<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue';
import { api } from '../api/client';

interface ContainerItem {
  task_id: string;
  name: string;
  state: string;
}

interface ContainersResponse {
  count: number;
  items: ContainerItem[];
}

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const containers = ref<ContainerItem[]>([]);
const count = ref(0);
const loading = ref(false);
const error = ref('');

let timer: ReturnType<typeof setInterval> | null = null;

async function fetchContainers() {
  loading.value = true;
  error.value = '';
  try {
    const res = await api<ContainersResponse>('GET', '/api/containers');
    containers.value = res.items ?? [];
    count.value = res.count ?? 0;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to fetch containers';
  } finally {
    loading.value = false;
  }
}

function startPolling() {
  fetchContainers();
  timer = setInterval(fetchContainers, 5000);
}

function stopPolling() {
  if (timer !== null) {
    clearInterval(timer);
    timer = null;
  }
}

function close() {
  emit('update:modelValue', false);
}

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('cm-backdrop')) close();
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close();
}

watch(() => props.modelValue, (open) => {
  if (open) {
    startPolling();
    document.addEventListener('keydown', onKey);
  } else {
    stopPolling();
    document.removeEventListener('keydown', onKey);
  }
}, { immediate: true });

onUnmounted(() => {
  stopPolling();
  document.removeEventListener('keydown', onKey);
});

function shortId(id: string): string {
  return id.slice(0, 8);
}
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="cm-backdrop" @click="onBackdrop">
      <div class="cm-modal">
        <header class="cm-header">
          <h2>Containers ({{ count }})</h2>
          <button class="cm-close" @click="close">&times;</button>
        </header>

        <div class="cm-body">
          <div v-if="loading && containers.length === 0" class="cm-empty">
            Loading...
          </div>
          <div v-else-if="error" class="cm-empty cm-error">
            {{ error }}
          </div>
          <div v-else-if="containers.length === 0" class="cm-empty">
            No running containers.
          </div>
          <ul v-else class="cm-list">
            <li v-for="c in containers" :key="c.task_id" class="cm-item">
              <span class="cm-name">{{ c.name }}</span>
              <span class="cm-task-id">{{ shortId(c.task_id) }}</span>
              <span class="cm-state" :class="c.state === 'running' ? 'cm-state-ok' : 'cm-state-warn'">
                <span class="cm-dot" />
                {{ c.state }}
              </span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cm-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
}

.cm-modal {
  width: 480px;
  max-width: 90vw;
  max-height: 70vh;
  background: var(--bg);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg, 10px);
  box-shadow: var(--sh-pop, 0 12px 40px rgba(0, 0, 0, 0.18));
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-family: var(--font-sans);
}

.cm-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--rule);
}

.cm-header h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
}

.cm-close {
  background: none;
  border: none;
  font-size: 20px;
  color: var(--ink-3);
  cursor: pointer;
  line-height: 1;
}

.cm-close:hover {
  color: var(--ink);
}

.cm-body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 0;
}

.cm-empty {
  padding: 24px 20px;
  text-align: center;
  font-size: 13px;
  color: var(--ink-3);
}

.cm-error {
  color: var(--warn);
}

.cm-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.cm-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 20px;
  border-bottom: 1px solid var(--rule);
  background: var(--bg-card);
}

.cm-item:last-child {
  border-bottom: none;
}

.cm-item:hover {
  background: var(--bg-hover);
}

.cm-name {
  flex: 1;
  font-size: 13px;
  font-family: var(--font-mono);
  color: var(--ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cm-task-id {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--ink-4);
  flex-shrink: 0;
}

.cm-state {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 500;
  flex-shrink: 0;
}

.cm-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  display: inline-block;
}

.cm-state-ok {
  color: var(--ok);
}

.cm-state-ok .cm-dot {
  background: var(--ok);
}

.cm-state-warn {
  color: var(--warn);
}

.cm-state-warn .cm-dot {
  background: var(--warn);
}
</style>
