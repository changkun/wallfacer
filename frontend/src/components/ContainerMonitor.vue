<script setup lang="ts">
import { ref, watch, onUnmounted, computed } from 'vue';
import { api } from '../api/client';

interface ContainerItem {
  id?: string;
  task_id?: string;
  task_title?: string;
  name?: string;
  image?: string;
  state?: string;
  status?: string;
  created_at?: number;
}

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const containers = ref<ContainerItem[]>([]);
const loading = ref(false);
const error = ref('');
const lastUpdated = ref('');

let timer: ReturnType<typeof setInterval> | null = null;

async function fetchContainers(quiet = false) {
  if (!quiet) loading.value = true;
  try {
    // Endpoint returns a JSON array of ContainerInfo directly.
    const res = await api<ContainerItem[] | null>('GET', '/api/containers');
    containers.value = Array.isArray(res) ? res : [];
    error.value = '';
    lastUpdated.value = `Last refreshed: ${new Date().toLocaleTimeString()}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to fetch containers';
  } finally {
    loading.value = false;
  }
}

function refresh() {
  lastUpdated.value = 'Refreshing...';
  fetchContainers(true);
}

function startPolling() {
  fetchContainers(false);
  timer = setInterval(() => fetchContainers(true), 5000);
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
  if (e.target === e.currentTarget) close();
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

function shortId(id: string | undefined): string {
  return id ? id.slice(0, 12) : '—';
}

function shortTaskId(id: string | undefined): string {
  return id ? id.slice(0, 8) : '';
}

function stateColor(state: string | undefined): string {
  switch ((state || '').toLowerCase()) {
    case 'running': return '#45b87a';
    case 'exited': return '#9c9890';
    case 'paused': return '#d4a030';
    case 'created': return '#6da0dc';
    case 'dead': return '#d46868';
    default: return '#9c9890';
  }
}

function relativeTime(seconds: number | undefined): string {
  if (!seconds || seconds <= 0) return '—';
  const diff = Date.now() - seconds * 1000;
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
}

const isEmpty = computed(() => !loading.value && !error.value && containers.value.length === 0);
const hasContent = computed(() => !loading.value && !error.value && containers.value.length > 0);
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      @click="onBackdrop"
    >
      <div
        class="modal-card"
        :style="{ maxWidth: '860px', width: '100%', maxHeight: '85vh', display: 'flex', flexDirection: 'column' }"
      >
        <div :style="{ display: 'flex', flexDirection: 'column', flex: '1', minHeight: '0' }" class="p-6">
          <div :style="{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }">
            <h3 :style="{ fontSize: '16px', fontWeight: 600, margin: '0' }">Sandbox Containers</h3>
            <div :style="{ display: 'flex', alignItems: 'center', gap: '8px' }">
              <button
                type="button"
                class="btn-icon"
                :style="{ fontSize: '12px', padding: '4px 10px' }"
                @click="refresh"
              >
                Refresh
              </button>
              <button
                type="button"
                :style="{ background: 'none', border: 'none', cursor: 'pointer', fontSize: '20px', color: 'var(--text-muted)', lineHeight: '1' }"
                @click="close"
              >
                &times;
              </button>
            </div>
          </div>

          <div :style="{ flex: '1', minHeight: '0', overflowY: 'auto' }">
            <div
              v-if="loading && containers.length === 0"
              :style="{ display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '32px', color: 'var(--text-muted)', fontSize: '13px' }"
            >
              Loading...
            </div>

            <div
              v-else-if="error"
              :style="{ padding: '12px', background: '#f5d5d5', borderRadius: '6px', fontSize: '12px', color: '#8c2020', fontFamily: 'monospace', whiteSpace: 'pre-wrap' }"
            >
              {{ error }}
            </div>

            <div
              v-else-if="isEmpty"
              :style="{ textAlign: 'center', padding: '32px', color: 'var(--text-muted)', fontSize: '13px' }"
            >
              No wallfacer containers found.
            </div>

            <div v-else-if="hasContent" :style="{ overflowX: 'auto' }">
              <table :style="{ width: '100%', borderCollapse: 'collapse', fontSize: '12px' }">
                <thead>
                  <tr :style="{ borderBottom: '1px solid var(--border)' }">
                    <th class="cm-th">Container ID</th>
                    <th class="cm-th">Task</th>
                    <th class="cm-th">Name</th>
                    <th class="cm-th">State</th>
                    <th class="cm-th">Status</th>
                    <th class="cm-th">Created</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(c, i) in containers" :key="(c.id || c.name || '') + i" class="cm-row">
                    <td :style="{ padding: '8px 10px', fontFamily: 'monospace', color: 'var(--text-secondary)', whiteSpace: 'nowrap' }">
                      {{ shortId(c.id) }}
                    </td>
                    <td :style="{ padding: '8px 10px', maxWidth: '260px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }">
                      <span v-if="c.task_title" :style="{ color: 'var(--text-primary)' }">{{ c.task_title }}</span>
                      <span v-else-if="c.task_id" :style="{ fontFamily: 'monospace', color: 'var(--text-muted)' }">
                        {{ shortTaskId(c.task_id) }}
                      </span>
                      <span v-else :style="{ color: 'var(--text-muted)' }">&mdash;</span>
                    </td>
                    <td
                      :style="{ padding: '8px 10px', fontFamily: 'monospace', color: 'var(--text-secondary)', whiteSpace: 'nowrap', maxWidth: '220px', overflow: 'hidden', textOverflow: 'ellipsis' }"
                      :title="c.name || ''"
                    >
                      {{ c.name || '—' }}
                    </td>
                    <td :style="{ padding: '8px 10px', whiteSpace: 'nowrap' }">
                      <span :style="{ display: 'inline-flex', alignItems: 'center', gap: '5px' }">
                        <span :style="{ width: '7px', height: '7px', borderRadius: '50%', background: stateColor(c.state), flexShrink: '0' }" />
                        {{ c.state || '—' }}
                      </span>
                    </td>
                    <td :style="{ padding: '8px 10px', color: 'var(--text-secondary)', whiteSpace: 'nowrap' }">
                      {{ c.status || '—' }}
                    </td>
                    <td :style="{ padding: '8px 10px', color: 'var(--text-secondary)', whiteSpace: 'nowrap' }">
                      {{ relativeTime(c.created_at) }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div
            :style="{ marginTop: '12px', paddingTop: '10px', borderTop: '1px solid var(--border)', fontSize: '11px', color: 'var(--text-muted)' }"
          >
            <span>{{ lastUpdated }}</span>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cm-th {
  text-align: left;
  padding: 6px 10px;
  font-weight: 600;
  color: var(--text-muted);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  white-space: nowrap;
}
.cm-row {
  border-bottom: 1px solid var(--border);
}
.cm-row:last-child {
  border-bottom: none;
}
.cm-row:hover {
  background: var(--bg-hover, transparent);
}
</style>
