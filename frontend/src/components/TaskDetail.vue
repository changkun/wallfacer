<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue';
import { api } from '../api/client';
import { useLogStream } from '../composables/useLogStream';
import type { Task } from '../api/types';

const props = defineProps<{ task: Task }>();
const emit = defineEmits<{ close: [] }>();

const activeTab = ref<'info' | 'logs'>('info');
const feedback = ref('');
const submittingFeedback = ref(false);
const logContainer = ref<HTMLElement | null>(null);

const streamTaskId = computed(() =>
  (props.task.status === 'in_progress' || props.task.status === 'committing') ? props.task.id : null,
);
const { lines, streaming } = useLogStream(streamTaskId);

watch(lines, async () => {
  await nextTick();
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight;
  }
}, { deep: true });

watch(() => props.task.status, (s) => {
  if (s === 'in_progress' || s === 'committing') activeTab.value = 'logs';
});

const costDisplay = computed(() => {
  const usd = props.task.usage.cost_usd;
  if (usd === 0) return 'no cost';
  if (usd < 0.01) return '<$0.01';
  return '$' + usd.toFixed(2);
});

const totalTokens = computed(() => {
  const u = props.task.usage;
  return (u.input_tokens + u.output_tokens).toLocaleString();
});

function timeStr(iso: string): string {
  return new Date(iso).toLocaleString();
}

async function startTask() {
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'in_progress' });
}
async function cancelTask() {
  await api('POST', `/api/tasks/${props.task.id}/cancel`);
}
async function retryTask() {
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'backlog' });
}
async function completeTask() {
  await api('POST', `/api/tasks/${props.task.id}/done`);
}
async function archiveTask() {
  await api('POST', `/api/tasks/${props.task.id}/archive`);
}
async function deleteTask() {
  await api('DELETE', `/api/tasks/${props.task.id}`);
  emit('close');
}
async function submitFeedback() {
  const text = feedback.value.trim();
  if (!text || submittingFeedback.value) return;
  submittingFeedback.value = true;
  try {
    await api('POST', `/api/tasks/${props.task.id}/feedback`, { feedback: text });
    feedback.value = '';
  } catch (e) {
    console.error('feedback:', e);
  } finally {
    submittingFeedback.value = false;
  }
}

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('detail-backdrop')) emit('close');
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close');
}

onMounted(() => {
  document.addEventListener('keydown', onKeydown);
  if (props.task.status === 'in_progress' || props.task.status === 'committing') {
    activeTab.value = 'logs';
  }
});
onUnmounted(() => document.removeEventListener('keydown', onKeydown));
</script>

<template>
  <div class="detail-backdrop" @click="onBackdrop">
    <aside class="detail-panel">
      <header class="detail-header">
        <h2 class="detail-title">{{ props.task.title || 'Untitled' }}</h2>
        <button class="detail-close" @click="emit('close')">&times;</button>
      </header>

      <div class="detail-meta">
        <span class="detail-id">{{ props.task.id.slice(0, 8) }}</span>
        <span class="detail-status" :class="'s-' + props.task.status">{{ props.task.status }}</span>
        <span v-if="props.task.sandbox" class="detail-sandbox">{{ props.task.sandbox }}</span>
        <span class="detail-cost">{{ costDisplay }}</span>
      </div>

      <div class="detail-tabs">
        <button :class="{ active: activeTab === 'info' }" @click="activeTab = 'info'">Info</button>
        <button :class="{ active: activeTab === 'logs' }" @click="activeTab = 'logs'">
          Logs
          <span v-if="streaming" class="pulse" />
        </button>
      </div>

      <div class="detail-body" v-if="activeTab === 'info'">
        <section class="detail-section">
          <h3>Prompt</h3>
          <pre class="detail-pre">{{ props.task.prompt }}</pre>
        </section>

        <section v-if="props.task.result" class="detail-section">
          <h3>Result</h3>
          <pre class="detail-pre">{{ props.task.result }}</pre>
        </section>

        <section v-if="props.task.status === 'waiting'" class="detail-section">
          <h3>Feedback</h3>
          <form class="feedback-form" @submit.prevent="submitFeedback">
            <textarea v-model="feedback" class="feedback-input" placeholder="Send feedback to the agent..." rows="3" />
            <button type="submit" class="feedback-btn" :disabled="!feedback.trim() || submittingFeedback">
              {{ submittingFeedback ? 'Sending...' : 'Send Feedback' }}
            </button>
          </form>
        </section>

        <section class="detail-section">
          <h3>Usage</h3>
          <div class="detail-kv">
            <span>Cost</span><span>{{ costDisplay }}</span>
            <span>Tokens</span><span>{{ totalTokens }}</span>
            <span>Turns</span><span>{{ props.task.turns }}</span>
          </div>
        </section>

        <section class="detail-section">
          <h3>Timeline</h3>
          <div class="detail-kv">
            <span>Created</span><span>{{ timeStr(props.task.created_at) }}</span>
            <span>Updated</span><span>{{ timeStr(props.task.updated_at) }}</span>
          </div>
        </section>
      </div>

      <div class="detail-body detail-logs" v-else-if="activeTab === 'logs'">
        <div ref="logContainer" class="log-scroll">
          <div v-if="!streaming && lines.length === 0" class="log-empty">
            {{ props.task.status === 'in_progress' ? 'Connecting...' : 'No logs (task not running)' }}
          </div>
          <pre v-for="(line, i) in lines" :key="i" class="log-line">{{ line }}</pre>
        </div>
      </div>

      <section class="detail-actions">
        <button v-if="props.task.status === 'backlog'" class="act-btn act-primary" @click="startTask">Start</button>
        <button v-if="props.task.status === 'in_progress'" class="act-btn act-danger" @click="cancelTask">Cancel</button>
        <button v-if="props.task.status === 'waiting'" class="act-btn act-primary" @click="completeTask">Mark Done</button>
        <button v-if="props.task.status === 'waiting'" class="act-btn act-danger" @click="cancelTask">Cancel</button>
        <button v-if="props.task.status === 'failed' || props.task.status === 'cancelled'" class="act-btn" @click="retryTask">Retry</button>
        <button v-if="props.task.status === 'done' || props.task.status === 'cancelled'" class="act-btn" @click="archiveTask">Archive</button>
        <button class="act-btn act-ghost" @click="deleteTask">Delete</button>
      </section>
    </aside>
  </div>
</template>

<style scoped>
.detail-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.3);
  display: flex;
  justify-content: flex-end;
  z-index: 100;
}
.detail-panel {
  width: 520px;
  max-width: 90vw;
  height: 100%;
  background: var(--bg);
  border-left: 1px solid var(--rule);
  display: flex;
  flex-direction: column;
}

.detail-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px 0;
}
.detail-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--ink);
  margin: 0;
  line-height: 1.3;
}
.detail-close {
  background: none;
  border: none;
  font-size: 20px;
  color: var(--ink-3);
  cursor: pointer;
  padding: 0 4px;
  line-height: 1;
}
.detail-close:hover { color: var(--ink); }

.detail-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 20px;
  font-size: 11px;
  font-family: var(--font-mono);
}
.detail-id { color: var(--ink-4); }
.detail-status {
  padding: 2px 6px;
  border-radius: 3px;
  text-transform: uppercase;
  font-size: 10px;
  font-weight: 600;
}
.s-backlog { color: var(--col-backlog); background: var(--bg-hover); }
.s-in_progress, .s-committing { color: var(--col-progress); background: rgba(58, 109, 179, 0.1); }
.s-waiting { color: var(--col-waiting); background: rgba(165, 106, 18, 0.1); }
.s-failed { color: var(--err); background: rgba(163, 45, 45, 0.1); }
.s-done { color: var(--col-done); background: rgba(63, 122, 74, 0.1); }
.s-cancelled { color: var(--ink-3); background: var(--bg-hover); }
.detail-sandbox { color: var(--ink-3); }
.detail-cost { margin-left: auto; color: var(--ink-3); }

.detail-tabs {
  display: flex;
  gap: 0;
  padding: 0 20px;
  border-bottom: 1px solid var(--rule);
}
.detail-tabs button {
  padding: 6px 14px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--ink-3);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
}
.detail-tabs button.active {
  color: var(--ink);
  border-bottom-color: var(--accent);
}
.detail-tabs button:hover { color: var(--ink); }

.pulse {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  animation: pulse-anim 1.5s infinite;
}
@keyframes pulse-anim {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.detail-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.detail-section h3 {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--ink-3);
  margin: 0 0 6px 0;
}
.detail-pre {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink);
  background: var(--bg-sunk);
  padding: 10px;
  border-radius: var(--r-sm);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 200px;
  overflow-y: auto;
  margin: 0;
  line-height: 1.5;
}
.detail-kv {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 2px 12px;
  font-size: 12px;
  font-family: var(--font-mono);
}
.detail-kv span:nth-child(odd) { color: var(--ink-3); }
.detail-kv span:nth-child(even) { color: var(--ink-2); }

.feedback-form {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.feedback-input {
  width: 100%;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg-sunk);
  color: var(--ink);
  font-family: var(--font-sans);
  font-size: 12px;
  padding: 8px;
  resize: vertical;
  outline: none;
}
.feedback-input:focus { border-color: var(--accent); }
.feedback-btn {
  align-self: flex-end;
  padding: 4px 12px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--r-sm);
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}
.feedback-btn:hover { background: var(--accent-2); }
.feedback-btn:disabled { opacity: 0.4; cursor: default; }

.detail-logs {
  padding: 0;
}
.log-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 8px 12px;
  background: var(--bg-sunk);
  font-family: var(--font-mono);
  font-size: 11px;
}
.log-empty {
  padding: 20px;
  text-align: center;
  color: var(--ink-4);
  font-family: var(--font-sans);
  font-size: 12px;
}
.log-line {
  margin: 0;
  padding: 0;
  line-height: 1.6;
  color: var(--ink-2);
  white-space: pre-wrap;
  word-break: break-all;
}

.detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 12px 20px;
  border-top: 1px solid var(--rule);
  flex-shrink: 0;
}
.act-btn {
  padding: 5px 14px;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg-card);
  color: var(--ink);
  font-size: 12px;
  cursor: pointer;
}
.act-btn:hover { background: var(--bg-hover); }
.act-primary { background: var(--accent); color: #fff; border-color: var(--accent); }
.act-primary:hover { background: var(--accent-2); }
.act-danger { background: var(--err); color: #fff; border-color: var(--err); }
.act-danger:hover { opacity: 0.9; }
.act-ghost { border-color: transparent; color: var(--ink-3); }
.act-ghost:hover { color: var(--err); }
</style>
