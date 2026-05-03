<script setup lang="ts">
import { computed } from 'vue';
import { api } from '../api/client';
import type { Task } from '../api/types';

const props = defineProps<{ task: Task }>();
const emit = defineEmits<{ close: [] }>();

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

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('detail-backdrop')) {
    emit('close');
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close');
}
</script>

<template>
  <div class="detail-backdrop" @click="onBackdrop" @keydown="onKeydown" tabindex="-1" ref="backdrop">
    <aside class="detail-panel">
      <header class="detail-header">
        <h2 class="detail-title">{{ props.task.title || 'Untitled' }}</h2>
        <button class="detail-close" @click="emit('close')">&times;</button>
      </header>

      <div class="detail-meta">
        <span class="detail-id">{{ props.task.id.slice(0, 8) }}</span>
        <span class="detail-status" :class="'s-' + props.task.status">{{ props.task.status }}</span>
        <span v-if="props.task.sandbox" class="detail-sandbox">{{ props.task.sandbox }}</span>
      </div>

      <section class="detail-section">
        <h3>Prompt</h3>
        <pre class="detail-prompt">{{ props.task.prompt }}</pre>
      </section>

      <section v-if="props.task.result" class="detail-section">
        <h3>Result</h3>
        <pre class="detail-result">{{ props.task.result }}</pre>
      </section>

      <section class="detail-section">
        <h3>Usage</h3>
        <div class="detail-usage">
          <span>{{ costDisplay }}</span>
          <span>{{ totalTokens }} tokens</span>
          <span>{{ props.task.turns }} turns</span>
        </div>
      </section>

      <section class="detail-section">
        <h3>Timeline</h3>
        <div class="detail-timeline">
          <span>Created: {{ timeStr(props.task.created_at) }}</span>
          <span>Updated: {{ timeStr(props.task.updated_at) }}</span>
        </div>
      </section>

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
  width: 480px;
  max-width: 90vw;
  height: 100%;
  background: var(--bg);
  border-left: 1px solid var(--rule);
  overflow-y: auto;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.detail-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
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
  letter-spacing: 0.03em;
}
.s-backlog { color: var(--col-backlog); background: var(--bg-hover); }
.s-in_progress, .s-committing { color: var(--col-progress); background: rgba(58, 109, 179, 0.1); }
.s-waiting { color: var(--col-waiting); background: rgba(165, 106, 18, 0.1); }
.s-failed { color: var(--err); background: rgba(163, 45, 45, 0.1); }
.s-done { color: var(--col-done); background: rgba(63, 122, 74, 0.1); }
.s-cancelled { color: var(--ink-3); background: var(--bg-hover); }
.detail-sandbox { color: var(--ink-3); }

.detail-section h3 {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--ink-3);
  margin: 0 0 6px 0;
}
.detail-prompt, .detail-result {
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
.detail-usage, .detail-timeline {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 12px;
  color: var(--ink-2);
  font-family: var(--font-mono);
}

.detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: auto;
  padding-top: 12px;
  border-top: 1px solid var(--rule);
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
